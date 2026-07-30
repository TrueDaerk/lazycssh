package ui

import (
	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// Fleet is what the panels read about the run. It is the subset of
// [ssh.Manager] the interface needs, declared here so the views can be tested
// against fakes and so nothing in this package can accidentally dial a host.
type Fleet interface {
	// IDs returns every session identifier in host order.
	IDs() []string
	// Session looks one up.
	Session(id string) (ssh.Session, bool)
	// Counts summarises the fleet.
	Counts() ssh.Counts
}

// Targeter is what the interface reads about broadcast scope: which hosts the
// next keystroke reaches, and how to say so. [broadcast.Router] implements it.
type Targeter interface {
	// Mode is the current broadcast mode.
	Mode() broadcast.Mode
	// SetMode changes the broadcast mode.
	SetMode(m broadcast.Mode) error
	// SetFocus records the focused pane, which is what single mode sends to.
	SetFocus(id string)
	// SetLimit restricts all and selected mode to the visible hosts; nil
	// lifts the limit. The UI pushes it, the router enforces it.
	SetLimit(ids []string)
	// Targets are the hosts a keystroke reaches right now.
	Targets() []string
	// Count is how many hosts that is.
	Count() int
	// Describe renders the status bar label.
	Describe() string
	// Warning reports whether the scope weakens the working set's protection.
	Warning() bool
	// IsSelected reports whether a host is toggled.
	IsSelected(id string) bool
	// Toggle flips a host's selection and reports whether it is now selected.
	// Selection is by identifier, so it survives a reconnect and paging: the
	// pane may move, the host does not change its name.
	Toggle(id string) bool
}

// FleetUpdatedMsg says the fleet changed: a session connected, failed, was
// closed or reconnected. It carries nothing, because Update re-reads the whole
// fleet into the model's snapshot when it arrives (see App.snapshotFleet)
// rather than reconstructing state from a stream of events - the transport
// drops event hints when the UI is behind, and a status bar built from dropped
// hints would be wrong exactly when it matters.
type FleetUpdatedMsg struct{}

// SessionOutputMsg says a session appended output. It carries no bytes: the
// pane reads the live scrollback when it renders, so the message only has to
// cause a redraw - and a coalesced or dropped message costs nothing, because
// the next one shows everything that arrived in between.
type SessionOutputMsg struct {
	// ID is the session that produced output.
	ID string
}

// ReconnectHostMsg asks the program to reconnect one host. The UI cannot dial,
// so it is emitted, not handled: the layer that owns the transport acts on it.
type ReconnectHostMsg struct {
	// ID is the session to replace.
	ID string
}

// CloseHostMsg asks the program to close one host's session. Emitted for the
// same reason as [ReconnectHostMsg].
type CloseHostMsg struct {
	// ID is the session to close.
	ID string
}

// RemoveHostMsg asks the program to take one host out of the run entirely:
// close its session and drop its pane. Emitted for the same reason as
// [ReconnectHostMsg].
type RemoveHostMsg struct {
	// ID is the session to remove.
	ID string
}

// HostConnectMsg asks the program to connect to hosts. The UI cannot dial, so
// it is emitted, not handled: it carries the patterns as the user gave them -
// an ssh-config alias picked from the Hosts panel, or free text that may hold
// brace expansion and user@host:port syntax.
type HostConnectMsg struct {
	// Patterns are the host patterns to resolve and connect, in order.
	Patterns []string
}

// ConnectErrorMsg reports that a [HostConnectMsg] could not be resolved. The
// program sends it back so the failure is visible where the user asked, rather
// than being dropped.
type ConnectErrorMsg struct {
	// Err is the resolve error, already rendered.
	Err string
}

// hostState is the model's copy of one session's cross-goroutine state. It is
// written only by [App.snapshotFleet], inside Update, so render helpers read
// model fields instead of racing the session goroutines.
type hostState struct {
	state        ssh.State
	exit         int
	exitReported bool
}

// snapshotFleet re-reads the fleet into the model: the host list, every
// session's state and last exit, and the counts derived from that one pass -
// one consistent view, taken inside Update, which is the only place the model
// changes. Everything below the sessions' own locks stays out of View: the
// render helpers read these fields, never the fleet.
//
// Called on every message that says the fleet changed ([FleetUpdatedMsg],
// [HostsChangedMsg], [SessionOpenedMsg]) and once at construction. The
// snapshot is what preserves the connected-only filter's "live view": a host
// flipping state emits a fleet event, the event refreshes the snapshot, and
// the redraw shows the new list - no keypress involved.
func (a App) snapshotFleet() App {
	if a.cfg.Fleet == nil {
		a.fleetHosts = nil
		a.hostStates = nil
		a.fleetCounts = ssh.Counts{Total: len(a.cfg.Hosts)}
		return a
	}
	ids := a.cfg.Fleet.IDs()
	states := make(map[string]hostState, len(ids))
	var counts ssh.Counts
	for _, id := range ids {
		counts.Total++
		session, ok := a.cfg.Fleet.Session(id)
		if !ok {
			counts.Pending++
			continue
		}
		st := session.State()
		code, reported := session.LastExit()
		states[id] = hostState{state: st, exit: code, exitReported: reported}
		switch st {
		case ssh.StateConnected:
			counts.Connected++
		case ssh.StateFailed:
			counts.Failed++
		case ssh.StateClosed:
			counts.Closed++
		default:
			counts.Pending++
		}
	}
	a.fleetHosts = ids
	a.hostStates = states
	a.fleetCounts = counts
	return a
}

// fleetIDs returns every host in the run: the snapshot's when there is a
// transport, the configured list otherwise, so views can be tested without
// one.
func (a App) fleetIDs() []string {
	if a.cfg.Fleet != nil {
		return a.fleetHosts
	}
	return a.cfg.Hosts
}

// hostIDs returns the hosts whose panes are drawn: the foreground session's -
// or the whole fleet when no session is open - after the visibility filters.
// Everything pane-shaped - the grid, the focus, paging, hit-testing - indexes
// into this list.
//
// The list is a pure function of model fields - the filters read the fleet
// snapshot, never live session state - so every computation inside one frame
// agrees (issue #135, #136). Callers that index into the result should still
// fetch it once and use that one slice.
func (a App) hostIDs() []string {
	return a.visibleHosts()
}

// counts summarises the fleet for the status panel, from the snapshot. Without
// a transport only the total is known, and it says so by leaving the rest at
// zero.
func (a App) counts() ssh.Counts {
	return a.fleetCounts
}

// state returns a host's connection state from the snapshot, or
// [ssh.StatePending] when there is no transport to ask.
func (a App) state(id string) ssh.State {
	if st, ok := a.hostStates[id]; ok {
		return st.state
	}
	return ssh.StatePending
}
