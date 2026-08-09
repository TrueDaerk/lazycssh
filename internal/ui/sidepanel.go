package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// The sidebar panels are child models, lazygit style: each one owns its own
// cursor and dialog state and renders its own body, and the root model routes
// to whichever panel is selected instead of switching over an enum in every
// code path (issue #228).
//
// The split of responsibilities:
//
//   - A panel owns its *view* state: the list cursor, the dialogs it asks with,
//     the rows it was last handed. That state lives in the child struct, not on
//     [App].
//   - The root keeps the *domain* state - the open sessions, the fleet
//     snapshot, the layout and the focus - and reduces the domain messages.
//     A panel that needs a domain change emits it the way the transport does:
//     as a message the root reduces, never by reaching into the root.
//   - What a panel reads of the domain arrives as a [panelContext] snapshot,
//     pushed by [App.syncPanels] on every Update. A panel therefore renders
//     from its own fields alone, which is the same discipline the fleet
//     snapshot enforces for the grid (issues #135, #136).
//
// The children are value structs inside [App], so the root model keeps its
// value semantics: an Update mutates a local copy and returns it, and no
// pointer escapes into an older model.

// sidePanel is one numbered panel down the left. The root routes key presses
// to the selected panel's Update and draws every panel through View; the panel
// decides what its keys mean and what its body shows.
type sidePanel interface {
	// Update handles one key press routed to the focused panel. Domain
	// changes are emitted as commands; panel-local state changes in place.
	Update(msg tea.KeyPressMsg) tea.Cmd
	// View renders the panel body into the width and height it was dealt.
	// focused reports whether the panel would receive a keystroke right now,
	// which decides how its cursor row is drawn (issue #222).
	View(focused bool, width, height int) string
	// Title is the heading shown in the sidebar.
	Title() string
	// Number is the key that jumps to this panel.
	Number() int
	// MoveCursor nudges the list cursor without committing anything - the
	// mouse wheel browses. A panel without a list ignores it.
	MoveCursor(delta int)
	// SetCursorRow puts the list cursor on a clicked visible body row.
	SetCursorRow(row int)
	// Preview renders the panel's main-area preview of its cursor row
	// (issue #218). ok false means the panel has none and the grid keeps
	// the main area.
	Preview(width, height int) (title, body string, ok bool)
}

// panelContext is the domain snapshot the panels render from. The root pushes
// it into every child on every Update ([App.syncPanels]), so a panel's View
// never reads the root model and cannot disagree with the frame it is drawn
// in. Long-lived collaborators - the group store, the command log, the
// broadcast router, the working set - are handed to the children once at
// construction instead; only what changes per message travels here.
type panelContext struct {
	// theme is the current palette, rebuilt when the terminal reports its
	// background.
	theme Theme
	// focus is the area that receives key presses, which is what the Status
	// panel's "keys go to" line is about.
	focus Area
	// focusedHost is the host whose pane has focus, empty without one.
	focusedHost string
	// sessionName is the run's name, empty for an ad hoc run.
	sessionName string
	// activeName is the foreground open session's name, empty when nothing
	// is open. It prefills the save-as prompt.
	activeName string
	// counts summarises the fleet.
	counts ssh.Counts
	// insecure and logging mirror the run flags the Status panel must show.
	insecure bool
	logging  bool
	// authLines is the Status panel's summary of open auth questions,
	// already styled.
	authLines []string
	// connectErr is the last connect request's resolve error, empty without
	// one.
	connectErr string
	// open are the open sessions in open order; active is the foreground
	// one, -1 when nothing is open. Read-only in the children.
	open   []openSession
	active int
	// hostStates is the fleet snapshot's per-host state, keyed by id.
	hostStates map[string]hostState
	// fleetIDs is every host in the run, the save-as fallback patterns.
	fleetIDs []string
	// runPatterns and runDefaults are what saving the run writes.
	runPatterns []string
	runDefaults sessions.HostOptions
	// bcastMode is the broadcast mode a saved run records.
	bcastMode broadcast.Mode
}

// stateOf reads a host's connection state from a snapshot, or
// [ssh.StatePending] when the snapshot does not know the host.
func stateOf(states map[string]hostState, id string) ssh.State {
	if st, ok := states[id]; ok {
		return st.state
	}
	return ssh.StatePending
}

// stateErrOf reads the failure text behind a host's failed state from a
// snapshot, empty for a host that is not failed.
func stateErrOf(states map[string]hostState, id string) string {
	st, ok := states[id]
	if !ok || st.state != ssh.StateFailed {
		return ""
	}
	return st.errText
}

// panelSet holds the four children in sidebar order. It lives by value inside
// [App], so copying the root copies the panels with it.
type panelSet struct {
	status   statusPanel
	groups   groupsPanel
	sessions sessionsPanel
	log      logPanel
	diff     diffPanel
}

// byID resolves a panel id to its child model, nil for a panel that has none.
// This is the one place the enum maps to a child; everything else dispatches
// through the interface.
func (ps *panelSet) byID(p Panel) sidePanel {
	switch p {
	case PanelStatus:
		return &ps.status
	case PanelGroups:
		return &ps.groups
	case PanelSessions:
		return &ps.sessions
	case PanelCommandLog:
		return &ps.log
	case PanelDiff:
		return &ps.diff
	default:
		return nil
	}
}

// syncPanels pushes the domain snapshot into every child. It runs on every
// Update, after the message was reduced, so the panels always render the state
// the root just settled on.
func (a App) syncPanels() App {
	ctx := panelContext{
		theme:       a.theme,
		focus:       a.focus,
		focusedHost: a.FocusedHost(),
		sessionName: a.cfg.SessionName,
		activeName:  a.ActiveSession(),
		counts:      a.counts(),
		insecure:    a.cfg.Insecure,
		logging:     a.cfg.Logging,
		authLines:   a.authPanelLines(),
		connectErr:  a.connectErr,
		open:        a.open,
		active:      a.active,
		hostStates:  a.hostStates,
		fleetIDs:    a.fleetIDs(),
		runPatterns: a.cfg.RunPatterns,
		runDefaults: a.cfg.RunDefaults,
		bcastMode:   a.broadcastMode(),
	}
	a.panels.status.ctx = ctx
	a.panels.groups.ctx = ctx
	a.panels.sessions.ctx = ctx
	a.panels.log.ctx = ctx
	a.panels.diff.ctx = ctx
	a.panels.diff.command = a.diffCommand
	a.panels.diff.marked = len(a.diffMarks)
	if a.panel == PanelDiff {
		// Grouping re-reads the marked hosts' scrollback, which is too much
		// work to repeat on every message for a panel nobody is looking at;
		// unselected, the panel shows the groups as of its last selection.
		a.panels.diff.variants = a.computeDiffVariants()
	}
	// A session leaving the list must not strand the cursor past the end;
	// the clamp lives with the cursor's owner.
	a.panels.sessions.clampCursor()
	a.panels.diff.clampCursor()
	return a
}

// activeFlags renders every state that weakens a default, in the warning
// style. It feeds both the status bar and the Status panel, so the two cannot
// drift.
func activeFlags(theme Theme, insecure, logging bool, targets Targeter) []string {
	var flags []string
	if insecure {
		flags = append(flags, theme.StatusInsecure.Render("HOST KEYS UNVERIFIED"))
	}
	if logging {
		flags = append(flags, theme.StatusWarning.Render("SESSION LOGGING ON"))
	}
	if targets != nil && targets.Warning() {
		flags = append(flags, theme.StatusWarning.Render("BROADCASTING TO EVERY HOST"))
	}
	return flags
}

// field renders a labelled value.
func field(theme Theme, label, value string) string {
	return theme.Muted.Render(label+": ") + theme.Base.Render(value)
}
