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
	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/sessionlog"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/ui"
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
	app := ui.NewApp(uiCfg)

	return &Model{
		app:      app,
		mgr:      mgr,
		ws:       ws,
		router:   router,
		targets:  targets,
		resolver: resolver,
		store:    cfg.Store,
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
		return m, tea.Batch(cmd, m.pump())

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

	case ui.CloseHostMsg:
		id := msg.ID
		return m, func() tea.Msg {
			_ = m.mgr.Close(id)
			return fleetEventMsg{inner: ui.FleetUpdatedMsg{}}
		}

	case ui.RemoveHostMsg:
		// The error is not actionable here: an unknown id means the host is
		// already gone, which is what removing asked for.
		_ = m.mgr.Remove(msg.ID)
		m.router.Forget(msg.ID)
		m.dropPattern(msg.ID)
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

// forward hands one message to the UI and keeps the returned model.
func (m *Model) forward(msg tea.Msg) tea.Cmd {
	model, cmd := m.app.Update(msg)
	if app, ok := model.(ui.App); ok {
		m.app = app
	}
	return cmd
}

// openGroup opens a saved group as a session: its hosts are resolved through
// ~/.ssh/config - HostName, Port and IdentityFile apply, the way every connect
// does - and added to the fleet. Hosts already in the run are reused, never
// dialled twice: opening a group a second time foregrounds its session.
//
// The group's broadcast mode and working set are applied, because opening is
// an explicit action about what the next keystroke should mean.
func (m *Model) openGroup(msg ui.GroupOpenMsg) (tea.Model, tea.Cmd) {
	if m.store == nil {
		return m, nil
	}
	sess, err := m.store.Load(msg.Name)
	if err != nil {
		// The Groups panel shows unreadable files on its rows; re-reading
		// the directory is how the error becomes visible.
		return m, m.forward(ui.SessionsChangedMsg{})
	}

	fleet, err := m.resolver.ResolveAll(sess.Patterns())
	if err != nil {
		return m, m.forward(ui.SessionsChangedMsg{})
	}

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

	cmd := m.forward(ui.SessionOpenedMsg{Name: msg.Name, Hosts: ids, Patterns: m.patterns})
	m.resizePTYs()
	return m, cmd
}

// connectHosts adds the hosts the UI asked to connect - an ssh-config alias
// picked in the Hosts panel, or a typed pattern with brace expansion.
//
// Hosts already in the run are skipped by identifier, so pressing enter twice
// on the same candidate cannot mint a duplicate "host-2" session; a resolve
// error goes back to the UI instead of being dropped, because the user just
// typed the thing that failed.
func (m *Model) connectHosts(msg ui.HostConnectMsg) (tea.Model, tea.Cmd) {
	fleet, err := m.resolver.ResolveAll(msg.Patterns)
	if err != nil {
		return m, m.forward(ui.ConnectErrorMsg{Err: err.Error()})
	}

	running := make(map[string]bool)
	for _, id := range m.mgr.IDs() {
		running[id] = true
	}
	added := false
	for _, host := range fleet {
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

	m.addPatterns(msg.Patterns)
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

// resizePTYs tells every remote PTY how big its pane's content is right now.
// The grid is recomputed the way the UI draws it, so what the remote believes
// matches what the user sees.
func (m *Model) resizePTYs() {
	if m.width <= 0 || m.height <= 0 {
		return
	}
	// The UI knows which panes are drawn - the foreground session, not the
	// fleet - so its grid is the shape the remotes must match.
	grid := m.app.Grid()
	if grid.Empty() {
		return
	}
	cell := grid.Cells[0]
	// One line of header and the border on each side never belong to the
	// remote shell.
	width := max(1, cell.Width-2)
	height := max(1, cell.Height-3)
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
