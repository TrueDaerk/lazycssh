// Package program assembles the layers into a running application: it builds
// the fleet from the resolved plan, wires the broadcast router, the working
// set, the command log and the TUI together, and runs the bubbletea event
// loop.
//
// This is the one place allowed to know every layer. internal/ui stays unable
// to dial and internal/ssh stays unaware of rendering; the wrapper model here
// is where the messages the UI emits (reconnect, close, launch a session) meet
// the transport that can act on them.
package program

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	cryptossh "golang.org/x/crypto/ssh"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/commandlog"
	"github.com/TrueDaerk/lazycssh/internal/history"
	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/recent"
	"github.com/TrueDaerk/lazycssh/internal/sessionlog"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/ui"
	"github.com/TrueDaerk/lazycssh/internal/version"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// Config is what a resolved command line hands the program.
type Config struct {
	// Patterns are the host arguments as typed, unexpanded, in order.
	Patterns []string
	// SessionName is shown in the status bar, empty for an ad hoc run.
	SessionName string
	// Broadcast is the mode the run starts in.
	Broadcast broadcast.Mode
	// WorkingSet is the working set the run starts with, or nil for all hosts.
	WorkingSet workingset.Selector
	// Store is the saved-session store the Sessions panel lists and writes.
	Store *sessions.Store
	// Insecure disables host key verification. It is a per-run choice the
	// status bar reports for the whole run.
	Insecure bool
	// Logs is the opt-in session log for this run, nil when logging is off.
	// When set, every host's output is copied to its file and the status bar
	// says SESSION LOGGING ON for the whole run.
	Logs *sessionlog.Run
	// History is the persistent broadcast command history the command line
	// reads on start and appends to on every send. Nil means the run has no
	// history file: recall starts empty and nothing is persisted, which keeps
	// tests that build a Model without one off the disk.
	History *history.Store
	// Recent is the persistent recent-host list: the host picker offers it as
	// its `rec` rows, and every session that reaches connected is recorded in
	// it. Nil means the run has no recent file, which keeps tests that build
	// a Model without one off the disk (issue #254).
	Recent *recent.Store

	// NewSession overrides the session factory. Nil means the real one, which
	// dials; tests inject fakes here so nothing in this package needs a
	// network.
	NewSession ssh.Factory
	// Resolver overrides host resolution. Nil means the user's ~/.ssh/config;
	// tests pass a zero resolver so nothing here depends on the machine.
	Resolver *hosts.Resolver
}

// Model is the root bubbletea model of a run: the UI plus the transport it is
// not allowed to own. It forwards everything to [ui.App] and acts on the
// messages the UI emits instead of handling.
type Model struct {
	app      ui.App
	mgr      *ssh.Manager
	ws       *workingset.Manager
	router   *broadcast.Router
	targets  loggedTargets
	resolver *hosts.Resolver
	store    *sessions.Store

	// recent is the persistent recent-host list, nil when the run has none;
	// recorded is which session identifiers already went into it, so a host
	// that reconnects three times is written once per run (issue #254).
	recent   *recent.Store
	recorded map[string]bool

	// patterns is how the run was assembled, as the user gave it: the CLI
	// arguments, then everything connected or launched at runtime, deduped in
	// order. Saving the run writes patterns, so they must track every change.
	patterns []string

	// prompter carries host key questions out of dialling sessions and
	// secrets does the same for passwords, passphrases and
	// keyboard-interactive answers; both nil when a test injected its own
	// session factory. pendingKeys and pendingSecrets are the questions the
	// UI is showing right now, by session id - every dialling session may
	// have its own open at once (issue #182).
	prompter       *keyPrompter
	pendingKeys    map[string]*keyQuestion
	secrets        *secretPrompter
	pendingSecrets map[string]*secretQuestion

	// ctx bounds every dial. Cancelled when the program shuts down.
	ctx context.Context

	width, height int

	// paneWidth and paneHeight are the pane content size the remotes were last
	// told about. The size follows more than the terminal - the screen mode
	// and which area has focus both move it (issue #219) - so every UI update
	// is a candidate for a resize, and this is what keeps that from sending a
	// window-change request per keystroke.
	paneWidth, paneHeight int
}

// Build assembles a run. Nothing is dialled yet; [Model.Init] starts the fleet
// so that connecting happens inside the event loop's lifetime.
func Build(ctx context.Context, cfg Config) (*Model, error) {
	resolver := cfg.Resolver
	if resolver == nil {
		var err error
		resolver, err = hosts.NewResolver()
		if err != nil {
			return nil, fmt.Errorf("resolve hosts: %w", err)
		}
	}

	fleet, err := resolver.ResolveAll(cfg.Patterns)
	if err != nil {
		return nil, err
	}

	factory := cfg.NewSession
	var prompter *keyPrompter
	var secrets *secretPrompter
	if factory == nil {
		// The channels are unbuffered on purpose: a dialling session blocks
		// until the UI has asked its question, so questions arrive one at a
		// time (issues #173, #175).
		prompter = &keyPrompter{questions: make(chan *keyQuestion)}
		secrets = &secretPrompter{questions: make(chan *secretQuestion)}
		factory, err = realFactory(ctx, cfg.Insecure, prompter, secrets, cfg.Logs)
		if err != nil {
			return nil, err
		}
	}

	mgr, err := ssh.NewManager(ssh.ManagerConfig{Hosts: fleet, NewSession: factory})
	if err != nil {
		return nil, err
	}

	ws := workingset.New(mgr.IDs())
	if cfg.WorkingSet != nil {
		if err := ws.Apply(cfg.WorkingSet); err != nil {
			return nil, err
		}
	}

	router, err := broadcast.NewRouter(ws)
	if err != nil {
		return nil, err
	}
	router.Attach(mgr)
	// The UI's mode switches go through targets, which keeps the session log's
	// suppression in lockstep: single mode is where passwords are typed, and
	// its output must never reach the disk.
	targets := loggedTargets{Router: router, logs: cfg.Logs}
	if cfg.Broadcast.Valid() {
		if err := targets.SetMode(cfg.Broadcast); err != nil {
			return nil, err
		}
	}

	logbook := commandlog.New(0)

	uiCfg := ui.Config{
		SessionName: cfg.SessionName,
		Version:     version.Version,
		Fleet:       mgr,
		Targets:     targets,
		WorkingSet:  ws,
		RunPatterns: cfg.Patterns,
		Sender:      router,
		Panes:       mgr,
		Recorder:    logbook,
		CommandLog:  logbook,
		Insecure:    cfg.Insecure,
		Logging:     cfg.Logs != nil,
		// The Hosts panel offers these as connect candidates; the UI still
		// cannot dial, it can only ask.
		ConfigAliases: resolver.Aliases(),
	}
	// A typed nil in the interface field would read as "there is a store";
	// leave it absent instead.
	if cfg.Store != nil {
		uiCfg.Sessions = cfg.Store
	}
	if cfg.History != nil {
		uiCfg.History = cfg.History
	}
	if cfg.Recent != nil {
		uiCfg.Recent = cfg.Recent
	}
	app := ui.NewApp(uiCfg)

	return &Model{
		app:      app,
		mgr:      mgr,
		ws:       ws,
		router:   router,
		targets:  targets,
		resolver: resolver,
		store:    cfg.Store,
		recent:   cfg.Recent,
		recorded: make(map[string]bool),
		prompter: prompter,
		secrets:  secrets,
		patterns: append([]string(nil), cfg.Patterns...),
		ctx:      ctx,
	}, nil
}

// realFactory builds sessions that dial, with agent and identity-file
// authentication and known_hosts verification. An unknown host key is asked
// about through the key prompter (issue #173); passwords, key passphrases and
// keyboard-interactive answers through the secret prompter (issue #175). The
// transport caches what it asked for - once per user, once per key file - so
// a fleet asks once, not forty times.
func realFactory(ctx context.Context, insecure bool, prompter ssh.HostKeyPrompter, secrets ssh.Prompter, logs *sessionlog.Run) (ssh.Factory, error) {
	creds := &ssh.Credentials{Prompter: secrets}

	callback := func(sessionID string, host hosts.Host) cryptossh.HostKeyCallback {
		return ssh.InsecureIgnoreHostKey()
	}
	if !insecure {
		files, err := ssh.DefaultKnownHostsFiles()
		if err != nil {
			return nil, err
		}
		known, err := ssh.NewKnownHosts(files, prompter)
		if err != nil {
			return nil, err
		}
		callback = func(sessionID string, host hosts.Host) cryptossh.HostKeyCallback {
			return known.Callback(ctx, sessionID, host)
		}
	}

	return func(req ssh.SessionRequest) ssh.Session {
		cfg := ssh.Config{
			Host:            req.Host,
			Auth:            creds.Methods(ctx, req.ID, req.Host),
			HostKeyCallback: callback(req.ID, req.Host),
			Terminal:        req.Terminal,
		}
		if logs != nil {
			// One log per host, keyed by session id: a reconnect appends to
			// the same file rather than starting a second one.
			cfg.Log = logs.HostLog(req.ID)
		}
		return ssh.New(req.ID, cfg, req.Events)
	}, nil
}

// loggedTargets is the router as the UI sees it, with one addition: switching
// the broadcast mode keeps the session log's suppression in lockstep. Single
// mode is how a password prompt is answered, so while it is active no host
// output may reach the disk - not even the echo of what is being typed.
type loggedTargets struct {
	*broadcast.Router
	logs *sessionlog.Run
}

// SetMode switches the mode and pauses or resumes the session log with it.
func (t loggedTargets) SetMode(m broadcast.Mode) error {
	if err := t.Router.SetMode(m); err != nil {
		return err
	}
	if t.logs != nil {
		t.logs.SetSuppressed(m == broadcast.ModeSingle)
	}
	return nil
}

// Manager exposes the fleet for the caller's shutdown path.
func (m *Model) Manager() *ssh.Manager { return m.mgr }

// fleetEventMsg wraps a transport event for the UI. The wrapper type exists so
// Update knows to re-arm the pump after delivering it.
type fleetEventMsg struct {
	inner tea.Msg
}

// pumpClosedMsg says the event channel is gone; the pump is not re-armed.
type pumpClosedMsg struct{}

// hostRemovedMsg says a session has been removed and fully closed; the
// bookkeeping that must follow runs where this message lands.
type hostRemovedMsg struct {
	id string
}

// pump drains one transport event and converts it to the message the UI
// documents. It blocks until an event arrives, which is fine inside a tea.Cmd:
// commands run on their own goroutines.
func (m *Model) pump() tea.Cmd {
	events := m.mgr.Events()
	return func() tea.Msg {
		ev, ok := <-events
		if !ok {
			return pumpClosedMsg{}
		}
		switch ev := ev.(type) {
		case ssh.OutputEvent:
			return fleetEventMsg{inner: ui.SessionOutputMsg{ID: ev.ID}}
		default:
			return fleetEventMsg{inner: ui.FleetUpdatedMsg{}}
		}
	}
}

// Init starts the fleet dialling and arms the event and host-key pumps.
func (m *Model) Init() tea.Cmd {
	m.mgr.Start(m.ctx)
	return tea.Batch(m.app.Init(), m.pump(), m.promptPump(), m.secretPump())
}

// Update routes messages: transport events into the UI, UI requests into the
// transport, everything else straight through.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case fleetEventMsg:
		cmd := m.forward(msg.inner)
		return m, tea.Batch(cmd, m.recordRecent(), m.pump())

	case pumpClosedMsg:
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		cmd := m.forward(msg)
		m.resizePTYs()
		return m, cmd

	case ui.ReconnectHostMsg:
		id := msg.ID
		return m, func() tea.Msg {
			// The error is already on the session as state; the pane shows it.
			_ = m.mgr.Reconnect(m.ctx, id)
			return fleetEventMsg{inner: ui.FleetUpdatedMsg{}}
		}

	case ui.ReconnectAllMsg:
		return m, func() tea.Msg {
			// Same reasoning as ReconnectHostMsg: each redial's error is
			// already on its session as state, which is what the panes show.
			m.mgr.ReconnectAll(m.ctx)
			return fleetEventMsg{inner: ui.FleetUpdatedMsg{}}
		}

	case ui.CloneHostMsg:
		return m.cloneHost(msg.ID)

	case ui.CloseHostMsg:
		id := msg.ID
		return m, func() tea.Msg {
			_ = m.mgr.Close(id)
			return fleetEventMsg{inner: ui.FleetUpdatedMsg{}}
		}

	case ui.RemoveHostMsg:
		// Remove waits for the session's goroutines to end, and a hung
		// connection can drag that out - so it runs in a Cmd, and the
		// bookkeeping follows in hostRemovedMsg once the session is gone.
		// The error is not actionable: an unknown id means the host is
		// already gone, which is what removing asked for.
		id := msg.ID
		return m, func() tea.Msg {
			_ = m.mgr.Remove(id)
			return hostRemovedMsg{id: id}
		}

	case hostRemovedMsg:
		m.router.Forget(msg.id)
		m.dropPattern(msg.id)
		m.ws.SetHosts(m.mgr.IDs())
		m.resizePTYs()
		return m, m.forward(ui.HostsChangedMsg{Hosts: m.mgr.IDs(), Patterns: m.patterns})

	case keyQuestionMsg:
		return m.askHostKey(msg)

	case ui.HostKeyAnswerMsg:
		return m.answerHostKey(msg)

	case secretQuestionMsg:
		return m.askSecret(msg)

	case ui.SecretAnswerMsg:
		return m.answerSecret(msg)

	case ui.GroupOpenMsg:
		return m.openGroup(msg)

	case groupResolvedMsg:
		return m.applyGroup(msg)

	case hostsResolvedMsg:
		return m.applyConnect(msg)

	case ui.GridChangedMsg:
		// The visible panes changed shape - a session switch, a filter - so
		// the remote PTYs must match what is drawn now.
		m.resizePTYs()
		return m, nil

	case ui.HostConnectMsg:
		return m.connectHosts(msg)
	}

	return m, m.forward(msg)
}

// recordRecent writes every session that has reached connected since the last
// call into the recent-host list, so the picker's `rec` rows are what actually
// answered rather than what was asked for (issue #254). A host that failed to
// dial is not a recent host.
//
// It records the resolved [hosts.Host] alias rather than the session
// identifier: a clone or a repeated host on the command line gets a
// disambiguated id (`srv1#2`), and `srv1#2` is not something a later run can
// connect to. The write is file I/O, so it happens in the returned Cmd; the
// bookkeeping that must not race stays here, on the Update goroutine.
func (m *Model) recordRecent() tea.Cmd {
	if m.recent == nil {
		return nil
	}
	var aliases []string
	for _, s := range m.mgr.Sessions() {
		if s.State() != ssh.StateConnected || m.recorded[s.ID()] {
			continue
		}
		m.recorded[s.ID()] = true
		aliases = append(aliases, s.Host().Alias)
	}
	if len(aliases) == 0 {
		return nil
	}

	store := m.recent
	return func() tea.Msg {
		// Oldest first, so the last one connected ends up at the front. A
		// list that cannot be written is not worth interrupting a run over:
		// the picker loses a row, nothing else.
		for _, alias := range aliases {
			_ = store.Add(alias)
		}
		return nil
	}
}

// forward hands one message to the UI and keeps the returned model.
func (m *Model) forward(msg tea.Msg) tea.Cmd {
	model, cmd := m.app.Update(msg)
	if app, ok := model.(ui.App); ok {
		m.app = app
	}
	// The pane size can change without the terminal changing: entering a pane
	// in half screen mode widens the grid (issue #219). Only a size that
	// actually moved is sent, so this costs nothing per keystroke.
	m.resizePTYsIfChanged()
	return cmd
}

// groupResolvedMsg is a saved group read from disk and resolved through
// ~/.ssh/config, ready to join the fleet - or the error that stopped it.
// Reading and resolving are file I/O, so they run in openGroup's Cmd; this
// message is where the result meets the managers, on the Update goroutine.
type groupResolvedMsg struct {
	name  string
	sess  *sessions.Session
	fleet []hosts.Host
	err   error
}

// hostsResolvedMsg is a connect request's patterns resolved into hosts, or
// the error that stopped them. Same split as groupResolvedMsg: resolution
// runs in a Cmd, the fleet mutation happens where this message lands.
type hostsResolvedMsg struct {
	patterns []string
	fleet    []hosts.Host
	err      error
}

// openGroup starts opening a saved group as a session. The store read and the
// ~/.ssh/config resolution touch the filesystem, so they run in the returned
// Cmd (issue #225); applyGroup takes over when the result arrives.
func (m *Model) openGroup(msg ui.GroupOpenMsg) (tea.Model, tea.Cmd) {
	if m.store == nil {
		return m, nil
	}
	store, resolver, name := m.store, m.resolver, msg.Name
	return m, func() tea.Msg {
		sess, err := store.Load(name)
		if err != nil {
			return groupResolvedMsg{name: name, err: err}
		}
		fleet, err := resolver.ResolveAll(sess.Patterns())
		if err != nil {
			return groupResolvedMsg{name: name, err: err}
		}
		return groupResolvedMsg{name: name, sess: sess, fleet: fleet}
	}
}

// applyGroup lands a resolved group in the fleet: its hosts are added - the
// way every connect is; hosts already in the run are reused, never dialled
// twice, so opening a group a second time foregrounds its session.
//
// The group's broadcast mode and working set are applied, because opening is
// an explicit action about what the next keystroke should mean.
func (m *Model) applyGroup(msg groupResolvedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		// The Groups panel shows unreadable files on its rows; re-reading
		// the directory is how the error becomes visible.
		return m, m.forward(ui.SessionsChangedMsg{})
	}
	sess, fleet := msg.sess, msg.fleet

	running := make(map[string]bool)
	for _, id := range m.mgr.IDs() {
		running[id] = true
	}
	ids := make([]string, 0, len(fleet))
	for _, host := range fleet {
		if running[host.Alias] {
			ids = append(ids, host.Alias)
			continue
		}
		ids = append(ids, m.mgr.Add(m.ctx, host))
	}
	m.addPatterns(sess.Patterns())
	m.ws.SetHosts(m.mgr.IDs())

	if mode, err := sess.Mode(); err == nil {
		_ = m.targets.SetMode(mode)
	}
	if sel, err := sess.Selector(); err == nil && sel != nil {
		_ = m.ws.Apply(sel)
	}

	cmd := m.forward(ui.SessionOpenedMsg{Name: msg.name, Hosts: ids, Patterns: m.patterns})
	m.resizePTYs()
	return m, cmd
}

// connectHosts starts connecting the hosts the UI asked for - an ssh-config
// alias picked in the Hosts panel, or a typed pattern with brace expansion.
// Resolution reads ~/.ssh/config, so it runs in the returned Cmd (issue
// #225); applyConnect takes over when the result arrives. A resolve error
// goes back to the UI instead of being dropped, because the user just typed
// the thing that failed.
func (m *Model) connectHosts(msg ui.HostConnectMsg) (tea.Model, tea.Cmd) {
	resolver, patterns := m.resolver, msg.Patterns
	return m, func() tea.Msg {
		fleet, err := resolver.ResolveAll(patterns)
		if err != nil {
			return hostsResolvedMsg{patterns: patterns, err: err}
		}
		return hostsResolvedMsg{patterns: patterns, fleet: fleet}
	}
}

// applyConnect lands resolved connect candidates in the fleet. Hosts already
// in the run are skipped by identifier, so pressing enter twice on the same
// candidate cannot mint a duplicate "host-2" session.
func (m *Model) applyConnect(msg hostsResolvedMsg) (tea.Model, tea.Cmd) {
	if msg.err != nil {
		return m, m.forward(ui.ConnectErrorMsg{Err: msg.err.Error()})
	}

	running := make(map[string]bool)
	for _, id := range m.mgr.IDs() {
		running[id] = true
	}
	added := false
	for _, host := range msg.fleet {
		if running[host.Alias] {
			continue
		}
		running[host.Alias] = true
		m.mgr.Add(m.ctx, host)
		added = true
	}
	if !added {
		return m, nil
	}

	m.addPatterns(msg.patterns)
	m.ws.SetHosts(m.mgr.IDs())
	m.resizePTYs()
	return m, m.forward(ui.HostsChangedMsg{Hosts: m.mgr.IDs(), Patterns: m.patterns})
}

// cloneHost opens a second, independent session to the same resolved host as
// an existing one (issue #253): the same Addr/User/Port, dialled through
// [ssh.Manager.Add], whose identifier disambiguation already keeps a
// repeated host apart from the one it was cloned from - the new pane gets its
// own input, scrollback, close and reconnect, and the broadcast router picks
// it up the same way it does any other session, because it reads the fleet
// live rather than a list captured at connect time. A session that closed or
// was removed between the keypress and here has nothing to clone.
func (m *Model) cloneHost(id string) (tea.Model, tea.Cmd) {
	s, ok := m.mgr.Session(id)
	if !ok {
		return m, nil
	}

	m.mgr.Add(m.ctx, s.Host())
	m.ws.SetHosts(m.mgr.IDs())
	m.resizePTYs()
	return m, m.forward(ui.HostsChangedMsg{Hosts: m.mgr.IDs(), Patterns: m.patterns})
}

// addPatterns appends patterns the run grew by, deduplicated in order, so a
// later save writes how the run was actually assembled - the drift between a
// run extended at runtime and the session it saves was a bug.
func (m *Model) addPatterns(patterns []string) {
	seen := make(map[string]bool, len(m.patterns))
	for _, p := range m.patterns {
		seen[p] = true
	}
	for _, p := range patterns {
		if !seen[p] {
			seen[p] = true
			m.patterns = append(m.patterns, p)
		}
	}
}

// dropPattern removes a pattern that names the removed host exactly. A glob or
// brace pattern stays: it cannot be narrowed by one host, and keeping it is
// the honest description of how the run was asked for.
func (m *Model) dropPattern(id string) {
	out := m.patterns[:0]
	for _, p := range m.patterns {
		if p != id {
			out = append(out, p)
		}
	}
	m.patterns = out
}

// resizePTYsIfChanged is [Model.resizePTYs] for the messages that only
// sometimes move the geometry - every UI update, in other words. A session
// that just joined the run is sized by the explicit call the change already
// makes, so skipping an unchanged size here cannot leave a new remote at the
// dialling default.
func (m *Model) resizePTYsIfChanged() {
	if w, h := m.paneExtent(); w == m.paneWidth && h == m.paneHeight {
		return
	}
	m.resizePTYs()
}

// paneExtent is the pane content size the remotes should believe, 0x0 when
// there is nothing drawn to take it from.
func (m *Model) paneExtent() (width, height int) {
	if m.width <= 0 || m.height <= 0 {
		return 0, 0
	}
	// The UI knows which panes are drawn - the foreground session, not the
	// fleet - so its grid is the shape the remotes must match.
	grid := m.app.Grid()
	if grid.Empty() {
		return 0, 0
	}
	cell := grid.Cells[0]
	// One line of header and the border on each side never belong to the
	// remote shell.
	return max(1, cell.Width-2), max(1, cell.Height-3)
}

// resizePTYs tells every remote PTY how big its pane's content is right now.
// The grid is recomputed the way the UI draws it, so what the remote believes
// matches what the user sees.
func (m *Model) resizePTYs() {
	width, height := m.paneExtent()
	if width <= 0 || height <= 0 {
		return
	}
	m.paneWidth, m.paneHeight = width, height
	_ = m.mgr.Resize(width, height)
}

// View renders the UI full screen.
func (m *Model) View() tea.View {
	v := m.app.View()
	v.AltScreen = true
	// Cell-motion reports clicks, releases and the wheel - what the UI acts
	// on - without the bare-movement chatter of all-motion.
	v.MouseMode = tea.MouseModeCellMotion
	return v
}
