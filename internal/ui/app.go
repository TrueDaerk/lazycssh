package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// Panel is one of the numbered panels down the left, in the order the number
// keys select them.
type Panel int

const (
	// PanelStatus answers "what happens if I type". It also hosts the new-host
	// prompt: connecting is about the run, and the run's summary is here.
	PanelStatus Panel = iota
	// PanelGroups lists the saved groups: create, delete, open.
	PanelGroups
	// PanelSessions lists the open sessions and switches the foreground.
	PanelSessions
	// PanelCommandLog is the audit trail of what was sent this run.
	PanelCommandLog
	// PanelDiff groups the hosts by the output of the last sent command, so
	// the machines that disagree stand out (issue #46).
	PanelDiff
)

// Panels returns the panels in sidebar order.
func Panels() []Panel {
	return []Panel{PanelStatus, PanelGroups, PanelSessions, PanelCommandLog, PanelDiff}
}

// Title is the heading shown in the sidebar.
func (p Panel) Title() string {
	switch p {
	case PanelStatus:
		return "Status"
	case PanelGroups:
		return "Groups"
	case PanelSessions:
		return "Sessions"
	case PanelCommandLog:
		return "Command log"
	case PanelDiff:
		return "Output diff"
	default:
		return "unknown(" + strconv.Itoa(int(p)) + ")"
	}
}

// Number is the key that jumps to this panel. The Output diff panel skips to
// 6: 5 has meant the broadcast bar since the bar existed, in the docs and in
// muscle memory, and a diff view is not worth renumbering it.
func (p Panel) Number() int {
	if p == PanelDiff {
		return 6
	}
	return int(p) + 1
}

// Config is what the root model needs to exist. Everything is passed in;
// nothing is read from a global.
type Config struct {
	// SessionName is shown in the status bar, empty for an ad hoc run.
	SessionName string
	// Hosts are the host identifiers of the run, in order. It is used when
	// there is no [Config.Fleet], which is how the views are tested without a
	// transport.
	Hosts []string
	// Fleet is the live transport. When set it is the source of truth for the
	// host list, the connection states and the counts.
	Fleet Fleet
	// Targets reports the broadcast scope. Nil means the run has no router
	// yet, and the status panel says so rather than guessing a count.
	Targets Targeter
	// WorkingSet is the current subject of work: what the Status panel reports
	// and what the Groups panel lists and switches between.
	WorkingSet WorkingSets
	// Sessions is the saved-session store the Sessions panel lists and writes.
	Sessions SessionStore
	// RunPatterns are the host arguments as the user typed them, kept so
	// saving the run writes patterns rather than expanded hostnames.
	RunPatterns []string
	// Version is the app version shown next to the name in the status bar.
	// Empty means the bar shows the name alone. internal/ui does not import
	// internal/version itself; the caller passes the string in to keep the
	// layering one-directional.
	Version string
	// RunDefaults are the connection options the run was started with, written
	// into a saved session.
	RunDefaults sessions.HostOptions
	// Sender delivers commands to the active broadcast set. Nil means the run
	// has no transport yet and the command line says so rather than pretending
	// to send.
	Sender CommandLine
	// Panes is where a focused pane's keystrokes go: one host, directly,
	// bypassing the broadcast scope. Nil means the run has no transport yet
	// and typing says so rather than pretending.
	Panes PaneWriter
	// Recorder receives the commands that were sent, for the audit trail.
	Recorder Recorder
	// History is the persistent command-line history: loaded once to seed
	// Up/Down recall, appended to on every send. Nil means the run has no
	// history file and recall starts empty, same as before this existed.
	History HistoryStore
	// CommandLog is the audit trail of what was sent this run. Nil means the
	// panel says so rather than pretending the run sent nothing.
	CommandLog CommandLog
	// Logging reports that session output is being written to disk, which is
	// off by default and must be visible for the whole run while it is on.
	Logging bool
	// Theme selects the palette.
	Theme Options
	// Keys overrides the bindings. The zero value means [DefaultKeyMap].
	Keys *KeyMap
	// Insecure reports that host key verification is off, which the status bar
	// then says for the whole run.
	Insecure bool
	// ConfigAliases are the concrete host aliases of the user's ~/.ssh/config,
	// which the Hosts panel offers as connect candidates. A plain slice keeps
	// this package free of the resolver, and the views testable without a
	// config file.
	ConfigAliases []string
	// Recent is the recent-host list earlier runs wrote, offered by the host
	// picker as its `rec` rows. Nil means the run has no recent file and the
	// picker offers none, same as before this existed (issue #254).
	Recent RecentHosts
	// HostSource supplies the fuzzy host picker's candidates. Nil means the
	// three default sources merged - [Config.ConfigAliases], the saved groups
	// of [Config.Sessions] and [Config.Recent] - and the field exists so a
	// further source can be plugged in without the picker changing (issues
	// #246, #254).
	HostSource HostSource
}

// App is the root bubbletea model: it owns the layout, the focus and the panel
// selection, and it renders the frame every other view draws into.
//
// Model mutation happens only in [App.Update]. Nothing here dials, reads or
// writes a host; the transport reports through messages.
type App struct {
	cfg    Config
	keys   *KeyMap
	theme  *Theme
	layout Layout

	// help is behind a pointer for the same reason the inputs are boxed: it
	// is ~4.6 KiB of styles, immutable except at resize and theme change,
	// and both of those sites replace the pointer rather than write through
	// it (issue #279).
	help *help.Model

	// memo is the one-frame render cache. Nil outside View; View plants one
	// on its value copy of the model, so it can never leak into Update. See
	// framememo.go.
	memo *frameMemo

	// panels are the sidebar's child models; see internal/ui/sidepanel.go.
	panels panelSet

	// The prompts are boxed - held by pointer, mutated copy-on-write - so
	// their ~7.8 KiB each stays off the App value; see boxedinput.go.
	cmdInput    boxedInput
	searchInput boxedInput
	hostInput   boxedInput

	// picker is the fuzzy host picker overlay; see internal/ui/hostpicker.go.
	picker hostPicker

	// open are the open sessions, in open order; active is the foreground
	// one, -1 when nothing is open.
	open   []openSession
	active int

	// keptSlots is the cell count the grid shape is kept from, so a host
	// leaving does not reflow every pane; see internal/ui/retile.go.
	keptSlots int

	// The split: chunks of splitSize panes, splitChunk on screen. 0 is off.
	splitInput boxedInput
	splitSize  int
	splitChunk int

	// The output filter: the pattern typed at `f`, empty while the grid shows
	// everything, and which hosts matched it as of the last evaluation. The
	// match set is a model field so a render never reads a live scrollback;
	// see internal/ui/outputfilter.go (issue #255).
	filterInput  boxedInput
	outputFilter string
	filterMatch  map[string]bool

	// broadcastLine is the broadcast bar's local echo of what was typed
	// since the last enter. The truth is on the hosts; this is the reminder.
	broadcastLine []rune

	// broadcastView routes the bar's keys to commands instead of the hosts.
	// It does not outlive the bar's focus: entering the bar always starts in
	// edit mode.
	broadcastView bool

	// prefixArmed is a typed ctrl+a waiting for its second key. The chord
	// works wherever focus is - in a pane, in the bar and at the app level
	// (issue #273) - and lasts exactly one key press; see prefix.go.
	prefixArmed bool

	// pendingPaste is a multiline paste addressed to more than one host, held
	// until enter releases it or esc drops it; see broadcastbar.go (issue
	// #248). Nil the rest of the time.
	pendingPaste *pendingPaste

	// connectErr is the last connect request's resolve error, shown in the
	// Status panel until the fleet changes.
	connectErr string

	// The fleet snapshot: the host list, per-session state and the counts,
	// re-read from the transport only inside Update (see snapshotFleet).
	// Render helpers read these fields; View never touches the fleet, so a
	// session goroutine flipping a state mid-render cannot shrink the host
	// list under an index (issue #135) or race the renderer (issue #136).
	fleetHosts  []string
	hostStates  map[string]hostState
	fleetCounts ssh.Counts

	// textSel is the mouse text selection inside a pane; see textselect.go.
	textSel textSelection

	// hadHosts records that the run held at least one host at some point.
	// It is what separates a run that emptied - every session logged out or
	// removed, the program's work is done, quit (issue #146) - from a run
	// that started empty and is waiting on the sessions picker.
	hadHosts bool

	// scroll is each pane's scrollback offset in wrapped lines from the
	// bottom; a missing entry is the tail. searchTerm is the one term every
	// pane highlights.
	scroll     map[string]int
	searchTerm string

	// matchAt is each pane's current match as a virtual line index, and
	// searchAnchor is the scroll offset that pane had before the search first
	// moved it, so esc can put the window back (issue #250). Both are empty
	// while no search is live.
	matchAt      map[string]int
	searchAnchor map[string]int

	// search is the incremental match cache behind the live term — a memo
	// shared by every model copy, self-validating against the emulator's
	// history cursor; see internal/ui/searchcache.go (issue #278).
	search *searchCache

	// render is the cross-frame pane render cache — the same shared-pointer
	// shape as search, self-validating against the emulator's sequence
	// counter, so a pane that did not change since the last frame is not
	// re-rendered; see internal/ui/rendercache.go (issue #293).
	render *renderCache

	cmdHistory    []string
	cmdHistoryPos int
	lastDelivery  string

	// The Output diff panel's comparison window: the last sent command and
	// each reached target's scrollback length at the send, which is where its
	// tail starts; see internal/ui/diffpanel.go (issue #46).
	diffCommand string
	diffMarks   map[string]int

	// The per-command exit indicator's window: each host reached by the last
	// command-line send, and its exit-marker sequence at that moment. A
	// marker past the mark is that command's answer; see
	// internal/ui/failures.go (issue #251).
	cmdExitMarks map[string]uint64

	// auth is the open auth questions by session id - every dialling session
	// may have its own at once (issue #182); see internal/ui/authpane.go.
	auth map[string]*paneAuth

	focus     Area
	panel     Panel
	paneIndex int
	page      int
	showHelp  bool

	// showPreview is the focused panel's preview as a popup over the frame.
	// The main area belongs to the grid while there are panes (issue #290), so
	// `p` is how the cursor row's detail is asked for; see preview.go.
	showPreview bool

	// screen is how much of the terminal the focused area gets; see screen.go.
	screen ScreenMode

	// now is the clock the scrollback file export stamps its filename with;
	// time.Now outside tests, a fixed instant inside them (issue #252).
	now func() time.Time
}

// newLineInput builds a text input the way every prompt here uses it: no
// prompt string, and cmd+arrow (super, macOS) jumping to line start and end
// alongside the home/end and ctrl+a/ctrl+e the widget already understands.
func newLineInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Prompt = ""
	ti.KeyMap.LineStart = key.NewBinding(key.WithKeys("home", "ctrl+a", "super+left"))
	ti.KeyMap.LineEnd = key.NewBinding(key.WithKeys("end", "ctrl+e", "super+right"))
	return ti
}

// NewApp builds the root model.
func NewApp(cfg Config) App {
	keys := DefaultKeyMap()
	if cfg.Keys != nil {
		keys = *cfg.Keys
	}

	if cfg.HostSource == nil {
		// The default sources are what the run already knows, in the order
		// that decides a repeated name: the ssh-config aliases the completion
		// hints come from, the saved groups, then the recent hosts.
		cfg.HostSource = MergeHostSources(
			aliasSource(cfg.ConfigAliases),
			groupSource{store: cfg.Sessions},
			recentSource{store: cfg.Recent},
		)
	}

	theme := NewTheme(cfg.Theme)
	h := help.New()
	h.Styles = HelpStyles(&theme)

	command := newLineInput("command")
	search := newLineInput("search")
	host := newLineInput("host, user@host:port, web-{01..04}")
	split := newLineInput("panes per view")
	filter := newLineInput("text in the output")

	a := App{
		cfg:         cfg,
		keys:        &keys,
		theme:       &theme,
		help:        &h,
		cmdInput:    boxInput(command),
		searchInput: boxInput(search),
		hostInput:   boxInput(host),
		splitInput:  boxInput(split),
		filterInput: boxInput(filter),
		scroll:      make(map[string]int),
		search:      &searchCache{},
		render:      &renderCache{},
		focus:       AreaSidebar,
		panel:       PanelStatus,
		now:         time.Now,
		active:      -1,
	}
	a.panels = panelSet{
		status: statusPanel{targets: cfg.Targets, workingSet: cfg.WorkingSet},
		groups: groupsPanel{
			keys:       &keys,
			store:      cfg.Sessions,
			workingSet: cfg.WorkingSet,
			nameInput:  boxInput(newLineInput("group name")),
			hostsInput: boxInput(newLineInput("host patterns, space separated")),
			saveInput:  boxInput(newLineInput("session name")),
		},
		sessions: sessionsPanel{keys: &keys, chosen: -1},
		log:      logPanel{keys: &keys, log: cfg.CommandLog},
		diff:     diffPanel{keys: &keys},
	}
	// A run that starts with hosts starts with a session holding them: the
	// CLI arguments are a workspace like any opened group.
	a = a.snapshotFleet().adoptNewHosts().keepGridSlots().syncPanels()
	// An argumentless start opens with nothing focused: the empty grid says
	// what the options are, and which of them comes first - connect, launch a
	// session, read the help - is the user's call, not the program's. The
	// group directory is read by Init's command, not here: construction must
	// not touch the disk (issue #225).
	return a
}

// resetToStart returns the model to the neutral argumentless start after the
// run emptied (issue #168): no pane focus, no kept grid shape, no filters. The
// sidebar gets the keyboard, because that is where the next action - open a
// group, connect a host - begins. Saved groups and the command log survive; a
// run's *view* state does not outlive its hosts.
func (a App) resetToStart() App {
	a.hadHosts = false
	a.screen = ScreenNormal
	a.paneIndex = 0
	a.page = 0
	a.splitSize = 0
	a.splitChunk = 0
	a.outputFilter = ""
	a.filterMatch = nil
	a.scroll = make(map[string]int)
	if a.focus == AreaGrid {
		a.focus = AreaSidebar
	}
	return a.resetGridSlots().syncBroadcastLimit()
}

// Init asks the terminal for its background colour so the palette can match
// it, and starts the first read of the group directory - disk I/O belongs in a
// Cmd, so not even startup reads it inline.
func (a App) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, a.loadGroupsCmd(), a.loadHistoryCmd())
}

// Focus is the area that receives key presses.
func (a App) Focus() Area { return a.focus }

// Panel is the selected sidebar panel.
func (a App) Panel() Panel { return a.panel }

// Layout is the current geometry.
func (a App) Layout() Layout { return a.layout }

// Theme is the current styles, rebuilt when the terminal reports its
// background.
func (a App) Theme() Theme { return *a.theme }

// HelpVisible reports whether the overlay is open.
func (a App) HelpVisible() bool { return a.showHelp }

// PreviewVisible reports whether the focused panel's row preview is floating
// over the frame.
func (a App) PreviewVisible() bool { return a.showPreview }

// Update handles one message. It is the only place the model changes.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := a.update(msg)
	app, ok := next.(App)
	if !ok {
		return next, cmd
	}
	if _, isOutput := msg.(SessionOutputMsg); isOutput &&
		app.outputFilter == "" && app.panel != PanelDiff {
		// A redraw hint changes nothing the resyncs below read: no layout, no
		// visible set, no panel context. Skipping them matters because these
		// hints arrive once per host per batch — a broadcast keystroke into a
		// 100-host fleet is 100 of them, and the O(fleet) resync per hint was
		// most of that keystroke's Update cost (issue #291). Two states keep
		// the full path, because for them new output is exactly what changes
		// the frame's shape: a live output filter re-decides which panes
		// match, and a selected Output diff panel regroups on what the hosts
		// said.
		return app, cmd
	}
	// The broadcast limit is a statement about what is on screen, and almost
	// every message can move that: a resize repages the grid, an arrow key
	// turns a page, a host leaving reflows the run. Resyncing once here is the
	// only way the limit cannot drift out of step with the panes (issue #199).
	// The panels render from a pushed snapshot the same way; syncing it here,
	// after the message settled, is what keeps a child's view from disagreeing
	// with the root state it describes.
	return app.syncLayout().syncBroadcastLimit().syncPanels(), cmd
}

// update is the real message handler; [App.Update] wraps it.
func (a App) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.layout = ComputeScreenLayout(msg.Width, msg.Height, a.screen, a.focus)
		h := *a.help
		h.SetWidth(max(0, a.layout.Width-2))
		a.help = &h
		return a, nil

	case tea.BackgroundColorMsg:
		opts := a.cfg.Theme
		opts.Dark = msg.IsDark()
		a.cfg.Theme = opts
		t := NewTheme(opts)
		a.theme = &t
		h := *a.help
		h.Styles = HelpStyles(a.theme)
		a.help = &h
		return a, nil

	case CommandResendMsg:
		// Resending goes through the same path as typing it: the current
		// broadcast set, the same report, the same audit entry.
		return a.sendCommand(msg.Command)

	case CommandResendMissingMsg:
		return a.resendMissing(msg)

	case DiffSelectMsg:
		// A variant's hosts become the selection, so "these three disagree"
		// turns into a target set B can broadcast to.
		sel, ok := a.selector()
		if !ok {
			a.lastDelivery = "no host selection yet"
			return a, nil
		}
		sel.ClearSelection()
		for _, id := range msg.Hosts {
			a.cfg.Targets.Toggle(id)
		}
		a.lastDelivery = fmt.Sprintf("selected this variant's %d host%s",
			len(msg.Hosts), plural(len(msg.Hosts)))
		return a, nil

	case SessionsChangedMsg:
		// The re-read is disk I/O, so it happens in a Cmd; the rows arrive as
		// a GroupsLoadedMsg (issue #225).
		return a, a.loadGroupsCmd()

	case HistoryLoadedMsg:
		return a.applyHistoryLoaded(msg), nil

	case GroupsLoadedMsg:
		a.panels.groups.applyLoaded(msg)
		return a, nil

	case GroupSavedMsg:
		return a, a.panels.groups.applySaved(msg)

	case GroupRemovedMsg:
		return a, a.panels.groups.applyRemoved(msg)

	case SaveResultMsg:
		cmd, saved := a.panels.groups.applySaveResult(msg)
		if saved {
			// The run now has the saved name: the status bar and the next
			// save's prefill carry it.
			a.cfg.SessionName = msg.Name
		}
		return a, cmd

	case HostKeyQuestionMsg:
		return a.showHostKeyQuestion(msg), nil

	case SecretQuestionMsg:
		return a.showSecretQuestion(msg), nil

	case SessionOpenedMsg:
		if msg.Patterns != nil {
			a.cfg.RunPatterns = msg.Patterns
		}
		a.connectErr = ""
		next := a.snapshotFleet().openSessionAt(msg.Name, msg.Hosts)
		return next, gridChanged()

	case FleetUpdatedMsg:
		// Re-read the fleet into the model; the panels render from that
		// snapshot. With a split on, the visible set follows the host list,
		// so the broadcast limit must follow too, and a session whose last
		// host closed is over.
		a = a.snapshotFleet()
		if a.splitSize > 0 {
			a = a.syncBroadcastLimit()
		}
		next, cmd := a.reapSessions()
		return next.refreshFilter().followFocus(), cmd

	case SessionOutputMsg:
		// Nothing to store: the pane reads the scrollback - which is
		// internally synchronized - when it renders. Redrawing is the whole
		// effect - except under an output filter, where the new output may
		// have changed which panes match (issue #255).
		return a.refreshFilter(), nil

	case HostsChangedMsg:
		focused := a.FocusedHost()
		next := a.withHosts(msg.Hosts).snapshotFleet().pruneSessions().
			adoptNewHosts().keepGridSlots().syncFilterMatches().
			refocus(focused).followFocus()
		// The fleet changed, so whatever a connect complained about is stale.
		next.connectErr = ""
		if msg.Patterns != nil {
			// The program tracks how the run was assembled; saving writes
			// patterns, so they must follow every change.
			next.cfg.RunPatterns = msg.Patterns
		}
		if next.hadHosts && len(next.fleetIDs()) == 0 {
			// The run had hosts and now has none: every session was logged
			// out or removed. The TUI stays open and falls back to the
			// neutral argumentless start (issue #168) - quitting is what q
			// and ctrl+q are for, and the user may well open the next group
			// from here.
			return next.resetToStart(), nil
		}
		return next, nil

	case ConnectErrorMsg:
		a.connectErr = msg.Err
		return a, nil

	case PaneExportedMsg:
		a.lastDelivery = msg.report()
		return a, nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)

	case tea.PasteMsg:
		return a.handlePaste(msg)

	case tea.MouseClickMsg:
		if msg.Button == tea.MouseLeft {
			return a.handleClick(msg.X, msg.Y)
		}
		return a, nil

	case tea.MouseMotionMsg:
		return a.handleMouseMotion(msg), nil

	case tea.MouseReleaseMsg:
		return a.handleMouseRelease(msg)

	case tea.MouseWheelMsg:
		return a.handleWheel(msg), nil
	}

	return a, nil
}

// handleKey dispatches a key press. Bindings are matched by area, so a key
// means one thing at a time; see [KeyMap].
func (a App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The bar's modal state does not outlive its focus: whatever mode the bar
	// was left in, coming back to it starts in edit mode. An armed ctrl+a
	// prefix is not bar state - it is live wherever focus is - so it survives
	// here and is cleared by the key that resolves it (issue #273).
	if a.focus != AreaBroadcast {
		a.broadcastView = false
	}

	// ctrl+q quits even while a text input has the keyboard: the chord is
	// never a character there, and an empty start opens straight into the
	// host prompt - which must not be able to trap the user. While typing to
	// a host it stays a keystroke for the host (XON), like every other chord
	// the pane forwards.
	if key.Matches(msg, a.keys.ForceQuit) &&
		(a.cmdInput.Focused() || a.hostInput.Focused() || a.picker.open ||
			a.searchInput.Focused() || a.Saving() || a.splitInput.Focused() ||
			a.filterInput.Focused() ||
			a.GroupDialogOpen() || a.DeleteGroupPending() != "" || a.EndSessionPending() != "") {
		return a, tea.Quit
	}

	// A live mouse selection takes ctrl+c/super+c ahead of every focused-input
	// or overlay dispatch below: whichever handler owns the keyboard would
	// otherwise see the chord first and the selection would never get copied
	// (issue #305). Without a selection, ctrl+c/super+c fall through unchanged
	// to their usual per-context handling further down.
	if a.textSelectionValid() && key.Matches(msg, a.keys.CopySelection) {
		return a.copyTextSelection()
	}

	// The command line has the keyboard while it is open: a command containing
	// a "b" must not switch the broadcast mode, and ctrl+c while editing must
	// not reach forty machines.
	if a.cmdInput.Focused() {
		return a.handleCommandLineKey(msg)
	}

	// The save prompt has the keyboard while it is open, for the same reason
	// the filter does: a session called "x" has to be nameable.
	if a.Saving() {
		return a, a.panels.groups.handleSaveKey(msg)
	}

	// The new-group dialog has the keyboard while it is open, for the same
	// reason the save prompt does: a group called "n" has to be nameable.
	if a.GroupDialogOpen() {
		return a, a.panels.groups.handleDialogKey(msg)
	}

	// So does the delete question: it is answered, never typed past.
	if a.DeleteGroupPending() != "" {
		return a, a.panels.groups.handleDeleteKey(msg)
	}

	// And the end-session question, for the same reason: ctrl+c on N
	// machines is not something a stray keystroke may confirm.
	if a.EndSessionPending() != "" {
		return a.handleSessionEndKey(msg)
	}

	// So does the host picker, for the same reason: every key that is not one
	// of its own is filter text.
	if a.picker.open {
		return a.handleHostPickerKey(msg)
	}

	// The new-host prompt has the keyboard while it is open: a pattern
	// containing "b" must not switch the broadcast mode.
	if a.hostInput.Focused() {
		return a.handleHostInputKey(msg)
	}

	// So does the scrollback search, for the same reason.
	if a.searchInput.Focused() {
		return a.handleSearchKey(msg)
	}

	// And the split prompt: it is one number, typed.
	if a.splitInput.Focused() {
		return a.handleSplitKey(msg)
	}

	// And the output filter's prompt, for the reason the command line has the
	// keyboard: a pattern containing a "b" must not switch the broadcast mode.
	if a.filterInput.Focused() {
		return a.handleFilterKey(msg)
	}

	// While the overlay is open it is the only thing listening: a user reading
	// the help is not also driving the panes. q/esc close the overlay, lazygit
	// convention for a topmost overlay; ctrl+q still force-quits the app.
	if a.showHelp {
		if key.Matches(msg, a.keys.ForceQuit) {
			return a, tea.Quit
		}
		a.showHelp = false
		return a, nil
	}

	// The row preview is the same kind of topmost overlay, and it closes the
	// same way: it is something the user opened to read, not a mode to drive
	// the fleet from (issue #290).
	if a.showPreview {
		if key.Matches(msg, a.keys.ForceQuit) {
			return a, tea.Quit
		}
		a.showPreview = false
		return a, nil
	}

	// A live mouse selection also takes esc here (copy is handled above, ahead
	// of the focused-input dispatch): esc clears the selection and still does
	// whatever it did before (issue #149, #302). Without a selection ctrl+c
	// stays what it always was, a keystroke for the hosts - but super+c has no
	// interrupt meaning to fall back to, so it is swallowed instead of
	// reaching the pane.
	if a.textSelectionValid() {
		if key.Matches(msg, a.keys.PromptCancel) {
			a = a.clearTextSelection()
		}
	} else if key.Matches(msg, a.keys.CopySelection) && msg.Mod.Contains(tea.ModSuper) {
		return a, nil
	}

	// A focused pane is a terminal: everything below this line is app-level
	// command handling, and the user decided commands only exist while no
	// input pane is selected.
	if a.focus == AreaGrid {
		return a.handleTypingKey(msg)
	}

	// So is the broadcast bar - for every target at once. In view mode it
	// routes to the app-level commands below instead; see broadcastbar.go.
	if a.focus == AreaBroadcast {
		return a.handleBroadcastKey(msg)
	}

	return a.handleAppKey(msg)
}

// handleAppKey is the app-level command dispatch: everything that is live once
// no terminal-like input owns the keyboard. The broadcast bar's view mode
// enters here directly, which is what makes its keys commands.
func (a App) handleAppKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// The ctrl+a chord is live here too, so paging works from the app level
	// in terminals that never send ctrl+shift+arrows (issue #273); it is
	// matched first, because a panel's plain letters must not shadow the key
	// that resolves an armed prefix. See prefix.go.
	if a.prefixArmed {
		return a.resolveAppPrefix(msg)
	}
	if key.Matches(msg, a.keys.Prefix) {
		return a.armPrefix(), nil
	}

	// The Groups panel's own letters shadow the global ones while it has
	// focus: n creates a group rather than connecting a host, d asks to
	// delete rather than selecting the hosts that are down.
	if a.focus == AreaSidebar && a.panel == PanelGroups {
		switch {
		case key.Matches(msg, a.keys.GroupNew):
			a.panels.groups.beginNew()
			return a, nil
		case key.Matches(msg, a.keys.GroupDelete):
			a.panels.groups.beginDelete()
			return a, nil
		}
	}

	// The scrollback search is a mode of this scope: `/` opens it, and while a
	// term is live n/N walk the matches - shadowing "connect a new host" for
	// exactly as long as the search lasts (issue #250). A focused pane never
	// gets here, so `/` and `n` still reach the host while typing.
	if next, handled := a.handleSearchModeKey(msg); handled {
		return next, nil
	}

	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit

	case key.Matches(msg, a.keys.Help):
		a.showHelp = true
		return a, nil

	case key.Matches(msg, a.keys.NextTab):
		return a.cycleFocus(+1), nil

	case key.Matches(msg, a.keys.PrevTab):
		return a.cycleFocus(-1), nil

	case key.Matches(msg, a.keys.CommandLine):
		return a.openCommandLine(), nil

	case key.Matches(msg, a.keys.QuickSave):
		// Saving works from anywhere at the app level: the prompt is
		// prefilled and enter confirms, so the common case is three keys.
		// The Groups panel is where the prompt renders, so it opens too.
		a.panel = PanelGroups
		a.focus = AreaSidebar
		a.panels.groups.beginSave()
		return a, nil

	case key.Matches(msg, a.keys.BroadcastAll):
		return a.setBroadcastMode(broadcast.ModeAll), nil
	case key.Matches(msg, a.keys.BroadcastSelected):
		return a.setBroadcastMode(broadcast.ModeSelected), nil
	case key.Matches(msg, a.keys.BroadcastSingle):
		// Single follows the focused pane, because "this host only" means the
		// pane the user is looking at.
		next := a.setBroadcastMode(broadcast.ModeSingle)
		return next.syncFocusTarget(), nil
	case key.Matches(msg, a.keys.BroadcastFleet):
		return a.setBroadcastMode(broadcast.ModeFleet), nil

	case key.Matches(msg, a.keys.Retile):
		return a.retile()

	case key.Matches(msg, a.keys.Split):
		return a.beginSplit(), nil
	case key.Matches(msg, a.keys.Filter):
		return a.beginOutputFilter(), nil
	case key.Matches(msg, a.keys.NextSplit):
		return a.stepView(+1)
	case key.Matches(msg, a.keys.PrevSplit):
		return a.stepView(-1)

	case key.Matches(msg, a.keys.NextFailure):
		// Global, not a grid key: "which host went wrong" is the question
		// whatever has focus.
		return a.jumpToNextFailure().syncFocusTarget(), nil

	case key.Matches(msg, a.keys.ReconnectAll):
		// Global, next to the per-pane alt+r: forty dropped hosts is not
		// forty keystrokes.
		return a.reconnectAllFailed()

	case key.Matches(msg, a.keys.NewHost):
		// Connecting is about the run, so it works from anywhere; the prompt
		// renders in the Status panel, which the keypress selects.
		a.panel = PanelStatus
		a.focus = AreaSidebar
		a.hostInput.Focus()
		return a, nil

	case key.Matches(msg, a.keys.HostPicker):
		// Browsing what could be connected is about the run too, so it opens
		// from anywhere; the picker floats over whatever is on screen.
		return a.openHostPicker(), nil
	}

	// The selection keys are app-level for the same reason the broadcast-mode
	// keys are: the selection is about the run, not about a panel.
	if next, handled := a.handleSelectionKey(msg); handled {
		return next, nil
	}

	if panel, ok := a.panelForKey(msg); ok {
		a.panel = panel
		a.focus = AreaSidebar
		return a, nil
	}
	if key.Matches(msg, a.keys.Panel5) {
		// Selecting the bar is entering it fresh: 5 from view mode is the
		// second way back to edit mode.
		a.focus = AreaBroadcast
		a.broadcastView, a.prefixArmed = false, false
		return a, nil
	}

	// Pane management works from the app level too: the chords are alt/shift
	// combinations no sidebar panel uses, so managing panes never requires
	// entering a pane's terminal.
	if next, cmd, handled := a.handlePaneKey(msg); handled {
		return next, cmd
	}

	// Everything below here is dispatched by focus, so the same key press means
	// one thing at a time. The bindings of the area that does not have focus
	// are not consulted at all.
	if a.focus == AreaSidebar {
		return a.handleSidebarKey(msg)
	}
	return a, nil
}

// handleHostInputKey feeds the new-host prompt, which owns the keyboard while
// it is open. enter asks the program to connect the typed pattern, tab
// completes the first matching ssh-config alias, esc abandons the prompt.
func (a App) handleHostInputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.PromptComplete):
		if hints := a.aliasHints(); len(hints) > 0 {
			a.hostInput.SetValue(hints[0])
			a.hostInput.CursorEnd()
		}
		return a, nil
	case key.Matches(msg, a.keys.PromptSubmit):
		pattern := strings.TrimSpace(a.hostInput.Value())
		a.hostInput.SetValue("")
		a.hostInput.Blur()
		if pattern == "" {
			return a, nil
		}
		a.connectErr = ""
		return a, func() tea.Msg { return HostConnectMsg{Patterns: []string{pattern}} }
	case key.Matches(msg, a.keys.PromptCancel):
		a.hostInput.SetValue("")
		a.hostInput.Blur()
		return a, nil
	}

	cmd := a.hostInput.Update(msg)
	return a, cmd
}

// handleSidebarKey switches panels on left/right, or dispatches up/down and
// the rest to the panel that owns a list of its own. Up/down never switch the
// panel (issue #212): they move a cursor inside the focused panel. Left/right
// - which mean nothing else while the sidebar has focus - are the explicit way
// to change which panel is focused, stopping at the ends; tab/shift+tab still
// own reaching the broadcast bar.
func (a App) handleSidebarKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.Left):
		return a.movePanel(-1), nil
	case key.Matches(msg, a.keys.Right):
		return a.movePanel(+1), nil
	case key.Matches(msg, a.keys.RowPreview) && hasPreview(a.panel):
		// The detail of the cursor row, floated over the grid rather than
		// taking it (issue #290). A panel without a preview leaves p alone.
		a.showPreview = true
		return a, nil
	}

	cmd := a.panels.byID(a.panel).Update(msg)
	if index, ok := a.panels.sessions.takeChosen(); ok {
		// The Sessions panel asked for a session in the foreground; the
		// switch is the root's move, because the grid and the broadcast
		// scope hang off it.
		return a.foregroundSession(index), tea.Batch(cmd, gridChanged())
	}
	if cmd != nil {
		return a, cmd
	}

	if a.panel == PanelStatus && key.Matches(msg, a.keys.Choose) {
		// Choosing from the Status panel is choosing the run's panes: the
		// grid is where the user wanted to end up.
		a.focus = AreaGrid
	}
	return a, nil
}

// closeOrRemove is what x means on a host: a live session is closed - the
// pane stays, saying so - and a dead one is removed, so the second x takes the
// pane off the screen. Both are emitted, not handled: the UI cannot touch the
// transport.
func (a App) closeOrRemove(id string) tea.Cmd {
	if a.state(id).Done() {
		return func() tea.Msg { return RemoveHostMsg{ID: id} }
	}
	return func() tea.Msg { return CloseHostMsg{ID: id} }
}

// grid is the current tiling of the main area, minus the overflow footer's
// line when it is drawn. It tiles for the kept slot count, which is the host
// count unless a departure left an empty cell behind.
func (a App) grid() Grid { return a.memoGrid() }

// tileGrid computes the grid without the frame memo; grid() is the reader.
func (a App) tileGrid() Grid { return TileGridCapped(a.gridArea(), a.gridSlots(), a.gridPaneCap()) }

// Grid returns the tiling the view is drawing, which is what the tests and the
// paging logic ask about.
func (a App) Grid() Grid { return a.grid() }

// FullScreen reports whether one pane fills the main area.
func (a App) FullScreen() bool { return a.screen == ScreenFull }

// panelForKey maps a number key to the panel it selects.
func (a App) panelForKey(msg tea.KeyPressMsg) (Panel, bool) {
	numbered := []key.Binding{a.keys.Panel1, a.keys.Panel2, a.keys.Panel3, a.keys.Panel4, a.keys.Panel6}
	for i, binding := range numbered {
		if key.Matches(msg, binding) {
			return Panel(i), true
		}
	}
	return 0, false
}

// cycleFocus advances the tab order the way lazygit does: through the sidebar
// panels one by one, then the broadcast bar, then round again. The grid is not
// a tab stop - inside it tab belongs to the host - so panes are entered with
// enter on a host row, an alt+arrow, or their number-free click; ctrl+] leads
// back out. The global area is not a stop: it is what is live everywhere.
func (a App) cycleFocus(step int) App {
	panels := len(Panels())
	stops := panels + 1 // every panel, then the broadcast bar

	pos := panels
	if a.focus == AreaSidebar {
		pos = int(a.panel)
	}
	pos = ((pos+step)%stops + stops) % stops

	if pos == panels {
		a.focus = AreaBroadcast
		return a
	}
	a.focus = AreaSidebar
	a.panel = Panel(pos)
	return a
}

// View renders the whole frame.
func (a App) View() tea.View {
	// The memo goes on View's value copy of the model: every render helper
	// below shares it, and it dies with the copy when the frame is returned.
	a.memo = &frameMemo{}
	if a.layout.TooSmall {
		// Even the apology has to fit: a terminal three columns wide gets three
		// columns of it rather than a line that wraps into the scrollback.
		return tea.NewView(a.theme.Muted.
			MaxWidth(max(0, a.layout.Width)).
			MaxHeight(max(0, a.layout.Height)).
			Render("terminal too small"))
	}
	if a.layout.Width == 0 || a.layout.Height == 0 {
		// No WindowSizeMsg has arrived yet. Rendering nothing is honest; the
		// first resize message is on its way.
		return tea.NewView("")
	}

	// The broadcast bar belongs to the grid's column: it stacks under the
	// panes, beside the sidebar, which keeps its full height.
	body := a.renderMain()
	if a.layout.BroadcastVisible() {
		body = lipgloss.JoinVertical(lipgloss.Left, body, a.renderBroadcastBar())
	}
	if a.layout.SidebarVisible() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, a.renderSidebar(), body)
	}

	bottom := a.renderStatusBar()
	if a.cmdInput.Focused() {
		bottom = a.renderCommandLine()
	}
	if a.searchInput.Focused() {
		bottom = a.renderSearchLine()
	}

	content := lipgloss.JoinVertical(lipgloss.Left, body, bottom)

	// The terminal's caret belongs to whatever the keyboard is aimed at, and
	// to nothing else: a dialog while one is open, the status bar's prompt
	// while one is, the focused pane while the grid has it, and nowhere at all
	// otherwise - an overlay covering the frame takes it away again below
	// (issue #292).
	cursor := a.frameCursor()

	// Dialogs and the help are popups over the frame, not replacements for
	// it: the fleet stays visible underneath, the way lazygit's menus behave.
	// The focused dialog's text cursor is the frame's cursor, so the terminal
	// draws it where the typing lands; see modal.go.
	if m, ok := a.activeModal(); ok {
		// The dialog owns the keyboard from here on, so the pane behind it
		// gives its caret up even when the frame is too small to draw a box.
		cursor = nil
		if box, x, y, c := a.renderModal(m); box != "" {
			content, cursor = composite(content, box, x, y), c
		}
	}
	if overlay, x, y, ok := a.previewOverlay(); ok {
		content, cursor = composite(content, overlay, x, y), nil
	}
	if a.showHelp {
		if overlay := a.renderHelpOverlay(); overlay != "" {
			x := max(0, (a.layout.Width-lipgloss.Width(overlay))/2)
			y := max(0, (a.layout.Height-lipgloss.Height(overlay))/2)
			content, cursor = composite(content, overlay, x, y), nil
		}
	}

	view := tea.NewView(content)
	view.Cursor = cursor
	return view
}

// frameCursor is the terminal cursor of whatever owns the keyboard outside a
// dialog: the status bar's prompt while one is open, otherwise the focused
// pane's own cursor - the host's, in the frame's coordinates, so the caret the
// terminal blinks sits exactly where the remote shell says the next character
// lands.
//
// nil means nothing owns it: no prompt, the grid without the focus, a host
// that is not connected, a pane scrolled back into history, a remote app that
// hid its cursor. bubbletea hides the caret for a frame whose Cursor is nil,
// which is what keeps a stale one from being left behind (issue #292).
func (a App) frameCursor() *tea.Cursor {
	// The two status-bar prompts own the keyboard wherever the focus happens
	// to be - see the guard chain in handleKey - so they own the caret too.
	// The order is View's own: the caret sits in the line that is drawn.
	if a.searchInput.Focused() {
		return a.statusPromptCursor("/", a.searchInput)
	}
	if a.cmdInput.Focused() {
		return a.statusPromptCursor(":", a.cmdInput)
	}

	if a.focus != AreaGrid {
		return nil
	}
	id := a.FocusedHost()
	if id == "" {
		return nil
	}
	cell, ok := a.focusedPaneRect()
	if !ok {
		return nil
	}
	// The pane's border eats a column on each side, and its header the first
	// row inside it, exactly as renderPane draws them.
	x, y, ok := a.paneCursor(id, cell.Width-2, cell.Height-3)
	if !ok {
		return nil
	}
	return tea.NewCursor(cell.X+1+x, cell.Y+2+y)
}

// statusPromptCursor places the caret in one of the status bar's prompts: past
// the bar's padding, the prompt's one-column sigil and the text typed before
// the caret, on the bar's own row. The prompts are drawn by renderCommandLine
// and renderSearchLine, which lay their line out exactly this way.
func (a App) statusPromptCursor(sigil string, in boxedInput) *tea.Cursor {
	r := a.layout.StatusBar
	if r.Empty() {
		return nil
	}
	x := a.theme.StatusBar.GetPaddingLeft() + lipgloss.Width(sigil) +
		typedWidth(in.Value(), in.Position())
	if x >= r.Width {
		// Typed past the edge of the bar: the terminal would put the caret on
		// the next line, which is not where the text is.
		return nil
	}
	return tea.NewCursor(r.X+x, r.Y)
}

// focusedPaneRect is the cell the focused pane is drawn in, in the frame's
// coordinates. ok is false when it is not on screen: no hosts, no room for a
// pane, or a page other than the one showing. It walks the same arithmetic as
// renderMain, so the rect is where the pane really is.
func (a App) focusedPaneRect() (Rect, bool) {
	if len(a.hostIDs()) == 0 {
		return Rect{}, false
	}
	if a.screen == ScreenFull {
		// Full screen is one pane in the whole main area.
		return a.layout.Main, true
	}
	g := a.grid()
	if g.Empty() || g.PerPage <= 0 || g.Page(a.paneIndex) != a.clampedPage(g) {
		return Rect{}, false
	}
	return g.Cell(a.paneIndex)
}

// renderSidebar draws the panel column the way lazygit does: every panel is
// its own titled box, the selected one holds everything the others leave, and
// the unselected ones show as much of their own body as their height allows —
// a preview when the sidebar is roomy, a bare title when it is not.
func (a App) renderSidebar() string {
	r := a.layout.Sidebar
	focused := a.focus == AreaSidebar

	panels := Panels()
	heights := a.sidebarHeights()

	boxes := make([]string, 0, len(panels))
	for i, panel := range panels {
		h := heights[i]
		if h <= 0 {
			continue
		}
		title := fmt.Sprintf("%s [%d]", panel.Title(), panel.Number())
		// Every panel renders its body into whatever height it was dealt;
		// titledBox clips the overflow, so a preview is simply the top of the
		// same content. The selected panel is "focused"-styled only while the
		// sidebar has focus; its expansion alone says "selected" when the grid
		// does. The body's width budget is the box minus border and padding.
		panelFocused := focused && panel == a.panel
		body := a.panelBody(panel, max(1, r.Width-4), max(1, h-2), panelFocused)
		boxes = append(boxes, titledBox(a.theme, panelFocused, r.Width, h, title, body))
	}

	return lipgloss.JoinVertical(lipgloss.Left, boxes...)
}

// renderMain draws the pane grid area. The grid itself arrives with its own
// issue; until then the frame reports what it will hold, rather than pretending
// to show output.
func (a App) renderMain() string {
	r := a.layout.Main
	focused := a.focus == AreaGrid

	// A focused list panel previews its cursor row here, lazygit style
	// (issue #218) - but only while there is no pane to draw: panes leave the
	// grid when their session ends, not when focus moves (issue #290).
	if preview, ok := a.mainPreview(); ok {
		return preview
	}

	if len(a.hostIDs()) == 0 {
		// The empty state says what to do next rather than showing an empty
		// frame: this is the argumentless start.
		hint := "no hosts\n\n" +
			"press A to pick hosts from ~/.ssh/config, n to type one,\n" +
			"or pick a session in [3] Sessions.\n\n" +
			"once connected: click or enter a pane and just type —\n" +
			"every key goes to that host; 5 broadcasts to all of them.\n\n" +
			"or start with hosts: lazycssh <host...>"
		return a.frame(a.theme.PaneFrame(focused, false), r, a.theme.Muted.Render(hint))
	}

	// Full screen is one pane in the whole area, which is what reading a stack
	// trace or driving an interactive program needs.
	if a.screen == ScreenFull {
		return a.renderPane(a.paneIndex, r, focused)
	}

	g := a.grid()
	if g.Empty() {
		return a.frame(a.theme.PaneFrame(focused, false), r, a.theme.Muted.Render("no room for a pane"))
	}

	first := a.clampedPage(g) * g.PerPage

	var rows []string
	for row := range g.Rows {
		var cells []string
		for col := range g.Columns {
			slot := row*g.Columns + col
			if slot >= len(g.Cells) {
				break
			}
			host := first + slot
			if host >= len(a.hostIDs()) {
				// An empty slot on the last page keeps the panes the size they
				// were, rather than letting them jump about as hosts come and go.
				cells = append(cells, a.frame(a.theme.Pane, g.Cells[slot], ""))
				continue
			}
			cells = append(cells, a.renderPane(host, g.Cells[slot], focused))
		}
		if len(cells) > 0 {
			rows = append(rows, zipJoinRow(cells))
		}
	}

	if a.overflowFooterVisible() {
		// Hidden panes are announced in the grid itself, not only in the
		// status bar: what is on screen must never read as the whole run.
		// Padded to the grid's width here, since the rows are joined without
		// re-measuring below.
		rows = append(rows, padLine(a.overflowFooter(), r.Width))
	}

	// A plain join, not lipgloss.JoinVertical: every row is already exactly
	// the area's width, and the general join would re-measure every line of
	// every pane per frame — width measurement was a fifth of a frame at
	// fleet scale (issue #291).
	return strings.Join(rows, "\n")
}

// zipJoinRow joins one grid row's pane frames side by side. It is
// lipgloss.JoinHorizontal for the one shape the grid guarantees — every cell
// rendered by [App.frame] to its exact rectangle, so all heights match and no
// line needs padding — without the per-line width measurement the general
// join pays (issue #291). Cells that break the shape fall back to the honest
// join rather than shearing the frame.
func zipJoinRow(cells []string) string {
	if len(cells) == 1 {
		return cells[0]
	}
	split := make([][]string, len(cells))
	size := 0
	for i, cell := range cells {
		split[i] = strings.Split(cell, "\n")
		if len(split[i]) != len(split[0]) {
			return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
		}
		size += len(cell) + 1
	}
	var b strings.Builder
	b.Grow(size)
	for y := range split[0] {
		if y > 0 {
			b.WriteByte('\n')
		}
		for i := range split {
			b.WriteString(split[i][y])
		}
	}
	return b.String()
}

// padLine pads one line with spaces to the given visible width, the way the
// general joins would have; a line already there is returned unchanged.
func padLine(line string, width int) string {
	if pad := width - lipgloss.Width(line); pad > 0 {
		return line + strings.Repeat(" ", pad)
	}
	return line
}

// renderPane draws one host's pane through the cross-frame cache: while the
// emulator's sequence and every model input in [paneKey] are what they were
// when the cached frame was rendered, that frame is the frame — a pane whose
// host said nothing since the last redraw costs a key comparison, not a
// render (issue #293). Anything else falls through to renderPaneFresh.
func (a App) renderPane(host int, cell Rect, gridFocused bool) string {
	ids := a.hostIDs()
	if host < 0 || host >= len(ids) || ids[host] == "" {
		// Beyond the list or a hole a closed host left behind (issue #169):
		// an empty frame holding the position until an explicit retile.
		return a.frame(a.theme.Pane, cell, "")
	}
	id := ids[host]
	focused := gridFocused && host == a.paneIndex
	if a.render == nil {
		return a.renderPaneFresh(host, id, cell, focused)
	}

	// Pin the content snapshot first: the key's sequence must name the
	// snapshot the body below would actually be built from.
	a.paneContent(id)
	e := a.render.entry(id)
	key := a.paneKeyFor(e, host, id, cell, focused)
	if e.framed && e.key == key {
		return e.pane
	}
	s := a.renderPaneFresh(host, id, cell, focused)
	e.key, e.pane, e.framed = key, s, true
	e.renders++
	return s
}

// renderPaneFresh draws one host's pane: a one-line header naming the host,
// then the session's scrollback following its tail.
func (a App) renderPaneFresh(host int, id string, cell Rect, focused bool) string {
	// The border eats two columns and rows, the header the top line of what
	// remains.
	content := a.paneHeader(host, cell.Width-2, focused)
	if body := a.paneBody(id, cell.Width-2, cell.Height-3); body != "" {
		if !a.paneAltScreen(id) {
			// A full-screen app owns the grid; a text selection over a live
			// screen would highlight cells that repaint under it.
			body = a.highlightSelection(id, cell.Width-2, body)
		}
		// Last, so the mark is not overpainted by a highlight: a broadcast
		// target that does not own the frame's caret shows where its own host
		// stands (issue #301).
		body = a.paintRemoteCursor(body, a.remoteCursorMark(id, cell.Width-2, cell.Height-3))
		content = content + "\n" + body
	}

	return a.frame(a.theme.PaneFrame(focused, a.commandFailed(id)), cell, content)
}

// renderStatusBar draws the bottom line: what is selected, how many hosts are in
// the run, and every flag that weakens a default.
func (a App) renderStatusBar() string {
	var parts []string
	if label := a.authStatusLabel(); label != "" {
		// Open auth prompts are counted here whatever has focus: panes off
		// screen may be waiting too.
		parts = append(parts, a.theme.StatusWarning.Render(label))
	}
	if a.focus == AreaBroadcast {
		if a.broadcastView {
			// View mode sends nothing, so it renders in the calm typing style:
			// the warning is for keys that reach hosts.
			parts = append(parts, a.theme.StatusTyping.Render(
				"BROADCAST VIEW — keys are commands · enter edits · "+a.escapeKey()+" leaves"))
		} else {
			count := 0
			if a.cfg.Targets != nil {
				count = a.cfg.Targets.Count()
			}
			label := fmt.Sprintf("BROADCASTING EDIT → %d host%s — %s leaves", count, plural(count), a.escapeKey())
			if a.prefixArmed {
				// After the prefix a key is a lazycssh command (issue #289);
				// only what none of them claims still reaches the hosts.
				label = fmt.Sprintf("BROADCASTING → %d host%s — %s… %s page · %s/a = literal · %s = view · command keys run, the rest go to the hosts",
					count, plural(count), a.prefixKey(), a.pagingLabel(), a.prefixKey(),
					firstKey(a.keys.PrefixCancel, "esc"))
			}
			parts = append(parts, a.theme.StatusWarning.Render(label))
		}
	}
	if a.focus == AreaGrid {
		// Where keystrokes go is the one thing the user must never have to
		// guess. The literal word carries the meaning, so it survives NoColor.
		target := a.FocusedHost()
		if target == "" {
			target = "no host"
		}
		parts = append(parts, a.theme.StatusTyping.Render(
			"TYPING "+target+" — "+a.escapeKey()+" leaves · alt=app"))
	}
	if label := a.prefixLabel(); label != "" && a.focus != AreaBroadcast {
		// An armed chord swallows the next key press, so it is announced for
		// as long as it lasts: a user must never wonder why a keystroke
		// became a command (issue #273). The bar says it in its own label
		// above, together with the target count.
		parts = append(parts, a.theme.StatusWarning.Render(label))
	}

	appLabel := "lazycssh"
	if a.cfg.Version != "" {
		appLabel += " v" + a.cfg.Version
	}
	parts = append(parts, a.theme.Base.Render(appLabel))
	// The foreground session is what the panes and the broadcast scope are
	// about, so its name is the one the bar carries.
	sessionLabel := a.ActiveSession()
	if sessionLabel == "" {
		sessionLabel = a.cfg.SessionName
	}
	if sessionLabel == adHocSessionName {
		// An unnamed run has no name worth a label; the panel still lists it.
		sessionLabel = ""
	}
	if sessionLabel != "" {
		parts = append(parts, a.theme.Base.Render("@"+sessionLabel))
	}
	if len(a.open) > 1 {
		parts = append(parts, a.theme.Muted.Render(
			fmt.Sprintf("%d sessions", len(a.open))))
	}
	hosts := len(a.hostIDs())
	hostsLabel := fmt.Sprintf("%d host%s", hosts, plural(hosts))
	if hosts == 0 {
		// The bar is the one thing on screen whatever panel has focus, so the
		// empty-fleet nudge belongs here as well as in the grid's hint text.
		hostsLabel += " — press " + a.keys.HostPicker.Keys()[0] + " to add"
	}
	parts = append(parts, a.theme.Muted.Render(hostsLabel))
	if a.cfg.Targets != nil {
		scope := a.theme.Base
		if a.cfg.Targets.Warning() {
			scope = a.theme.StatusWarning
		}
		parts = append(parts, scope.Render(a.cfg.Targets.Describe()))
	}
	if label := a.filterLabel(); label != "" {
		// The filter hides panes, so it renders in the warning style for as
		// long as it is in force: a grid that is showing a part must never
		// read as the whole run (issue #255).
		parts = append(parts, a.theme.StatusWarning.Render(label))
	}
	if label := a.splitLabel(); label != "" {
		// The split narrows what a keystroke reaches, so it renders in the
		// warning style for as long as it is in force.
		parts = append(parts, a.theme.StatusWarning.Render(label))
	}
	if label := a.screenLabel(); label != "" {
		// A non-normal screen mode is why panes are missing from the grid, so
		// it says so rather than letting a capped page read as the whole run.
		parts = append(parts, a.theme.StatusWarning.Render(label))
	}
	if label := a.windowLabel(); label != "" {
		parts = append(parts, a.theme.Muted.Render(label))
	}
	if a.lastDelivery != "" {
		parts = append(parts, a.theme.Muted.Render(a.lastDelivery))
	}
	if summary := a.failureSummary(); summary != "" {
		parts = append(parts, summary)
	}
	if label := a.scrollLabel(); label != "" {
		// A pane that is not following its tail says so: fresh output landing
		// behind a frozen window must not look like a quiet host.
		parts = append(parts, a.theme.StatusWarning.Render(label))
	}
	if label := a.searchLabel(); label != "" {
		parts = append(parts, a.theme.Muted.Render(label))
	}

	// The flags that weaken a default live on the status bar as well as in the
	// Status panel, because the bar is the one thing that is on screen whatever
	// the user has scrolled to.
	parts = append(parts, a.activeFlags()...)

	line := strings.Join(parts, " ")
	if short := a.help.ShortHelpView(a.keys.For(a.focus).ShortHelp()); short != "" {
		// Key hints sit flush right, lazygit style, when there is room; when
		// there is not they trail the info and the bar clips as before.
		gap := a.layout.Width - 2 - lipgloss.Width(line) - lipgloss.Width(short)
		if gap > 1 {
			line += strings.Repeat(" ", gap) + short
		} else {
			line += "  " + short
		}
	}
	return a.theme.StatusBar.
		Width(a.layout.Width).
		MaxHeight(StatusBarHeight).
		Render(line)
}

// renderHelpOverlay draws the keybindings popup, generated from the keymap.
// Every column is one area, headed by its name, the focused area first -
// lazygit's keybindings menu, built from the same source the keys are.
func (a App) renderHelpOverlay() string {
	ctx, ok := a.keys.For(a.focus).(contextHelp)
	if !ok {
		return ""
	}

	// A narrower copy of the help model, so the popup's content plus its
	// border and padding always fits inside the frame without wrapping; the
	// help bubble drops whole columns rather than garbling them.
	h := *a.help
	h.SetWidth(max(0, a.layout.Width-6))

	content := renderHelpColumns(h, ctx.FullHelp(), ctx.Titles(), a.theme)
	content += "\n\n" + a.theme.Muted.Render("any key closes this")

	w := min(a.layout.Width-2, lipgloss.Width(content)+4)
	hgt := min(a.layout.Height-1, lipgloss.Height(content)+2)
	return titledBox(a.theme, true, w, hgt, "Keybindings — "+a.focus.String(), content)
}

// renderHelpColumns lays out FullHelp's groups the way help.Model.FullHelpView
// does - one column per group, narrowest-first drop when they overflow h's
// width - but heads each column with the area name from Titles(), so the
// overlay says what a column is for rather than leaving it to the box title
// to name the one area that happens to be focused.
func renderHelpColumns(h help.Model, groups [][]key.Binding, titles []string, t *Theme) string {
	sep := h.Styles.FullSeparator.Inline(true).Render(h.FullSeparator)
	heading := t.Muted.Bold(true)

	var cols []string
	var totalWidth int
	for i, group := range groups {
		col := h.FullHelpView([][]key.Binding{group})
		if col == "" {
			continue
		}
		title := ""
		if i < len(titles) {
			title = titles[i]
		}
		block := lipgloss.JoinVertical(lipgloss.Left, heading.Render(title), col)

		width := lipgloss.Width(block)
		if totalWidth > 0 {
			width += lipgloss.Width(sep)
		}
		if h.Width() > 0 && totalWidth+width > h.Width() && totalWidth > 0 {
			break
		}

		if totalWidth > 0 {
			cols = append(cols, sep)
		}
		cols = append(cols, block)
		totalWidth += width
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, cols...)
}

// frame draws content inside a bordered box sized to a rect. lipgloss v2
// counts the border into Width and Height, so the rect's size is the block's
// size; it is clamped at zero so a tiny terminal cannot ask lipgloss for a
// negative width.
func (a App) frame(style lipgloss.Style, r Rect, content string) string {
	return style.
		Width(max(0, r.Width)).
		Height(max(0, r.Height)).
		MaxWidth(max(0, r.Width)).
		MaxHeight(max(0, r.Height)).
		Render(content)
}

// plural is the "s" that makes a count read as English.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// setBroadcastMode switches the broadcast scope. A run with no router yet has
// no scope to switch, and says nothing rather than pretending.
func (a App) setBroadcastMode(m broadcast.Mode) App {
	if a.cfg.Targets == nil {
		return a
	}
	// An invalid mode cannot come from a binding, and a router that refuses one
	// is a bug in this file rather than something the user can act on.
	_ = a.cfg.Targets.SetMode(m)
	return a
}

// syncFocusTarget tells the router which pane has focus, which is what single
// mode sends to.
func (a App) syncFocusTarget() App {
	if a.cfg.Targets == nil {
		return a
	}
	a.cfg.Targets.SetFocus(a.FocusedHost())
	return a
}

// BroadcastMode is the scope the next keystroke goes to.
func (a App) BroadcastMode() broadcast.Mode { return a.broadcastMode() }
