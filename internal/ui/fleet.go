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
// closed or reconnected. It carries nothing, because the model reads the live
// state rather than reconstructing it from a stream of events - the transport
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

// hostIDs returns the run's hosts: the fleet's when there is one, the
// configured list otherwise, so views can be tested without a transport.
func (a App) hostIDs() []string {
	if a.cfg.Fleet != nil {
		return a.cfg.Fleet.IDs()
	}
	return a.cfg.Hosts
}

// counts summarises the fleet for the status panel. Without a transport only
// the total is known, and it says so by leaving the rest at zero.
func (a App) counts() ssh.Counts {
	if a.cfg.Fleet != nil {
		return a.cfg.Fleet.Counts()
	}
	return ssh.Counts{Total: len(a.cfg.Hosts)}
}

// state returns a host's connection state, or [ssh.StatePending] when there is
// no transport to ask.
func (a App) state(id string) ssh.State {
	if a.cfg.Fleet == nil {
		return ssh.StatePending
	}
	session, ok := a.cfg.Fleet.Session(id)
	if !ok {
		return ssh.StatePending
	}
	return session.State()
}
