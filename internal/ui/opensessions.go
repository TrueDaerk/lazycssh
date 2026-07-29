package ui

import (
	tea "charm.land/bubbletea/v2"
)

// An **open session** is a named set of hosts that is on screen together: the
// runtime counterpart of a saved group. Several can be open at once; exactly
// one is in the foreground, and only its panes are drawn and only its hosts
// are broadcast targets (fleet mode excepted). A background session keeps its
// connections - switching is about what is visible, never about tearing
// anything down.

// adHocSessionName is the session an unnamed run's hosts live in: the CLI
// arguments, or the first host connected with n before any group was opened.
const adHocSessionName = "adhoc"

// openSession is one open session.
type openSession struct {
	// Name is the group the session was opened from, or [adHocSessionName].
	Name string
	// Hosts are the session's host identifiers, in the order they joined.
	Hosts []string
}

// GroupOpenMsg asks the program to open a saved group as a session. The UI
// cannot dial, so it is emitted, not handled: the program resolves the group's
// patterns through ~/.ssh/config and connects, then answers with
// [SessionOpenedMsg].
type GroupOpenMsg struct {
	// Name is the group to open.
	Name string
}

// SessionOpenedMsg says a group's hosts are in the fleet and names them. The
// UI foregrounds the session for that group, creating it if it is new.
type SessionOpenedMsg struct {
	// Name is the group that was opened.
	Name string
	// Hosts are the session's host identifiers, in order.
	Hosts []string
	// Patterns are the run's host patterns after the open, kept so saving
	// writes the truth; nil means "unchanged".
	Patterns []string
}

// GridChangedMsg says the set of visible panes changed shape: a session came
// to the foreground, a filter was toggled. The UI emits it so the program can
// resize the remote PTYs to the new pane size.
type GridChangedMsg struct{}

// OpenSessionNames returns the open sessions' names in open order.
func (a App) OpenSessionNames() []string {
	names := make([]string, 0, len(a.open))
	for _, s := range a.open {
		names = append(names, s.Name)
	}
	return names
}

// ActiveSession is the foreground session's name, empty when nothing is open.
func (a App) ActiveSession() string {
	if a.active < 0 || a.active >= len(a.open) {
		return ""
	}
	return a.open[a.active].Name
}

// sessionHosts returns the foreground session's hosts that are still in the
// fleet, in session order. With nothing open it falls back to the whole fleet,
// which is also how the views are tested without sessions.
func (a App) sessionHosts() []string {
	if a.active < 0 || a.active >= len(a.open) {
		return a.fleetIDs()
	}
	fleet := make(map[string]bool)
	for _, id := range a.fleetIDs() {
		fleet[id] = true
	}
	var out []string
	for _, id := range a.open[a.active].Hosts {
		if fleet[id] {
			out = append(out, id)
		}
	}
	return out
}

// openSessionAt upserts a session and brings it to the foreground. New hosts
// are appended to an existing session of the same name; its earlier hosts keep
// their order, so panes do not jump about when a group is opened twice.
func (a App) openSessionAt(name string, hosts []string) App {
	for i := range a.open {
		if a.open[i].Name != name {
			continue
		}
		known := make(map[string]bool, len(a.open[i].Hosts))
		for _, id := range a.open[i].Hosts {
			known[id] = true
		}
		merged := append([]string(nil), a.open[i].Hosts...)
		for _, id := range hosts {
			if !known[id] {
				merged = append(merged, id)
			}
		}
		a.open[i].Hosts = merged
		return a.foregroundSession(i)
	}
	a.open = append(a.open, openSession{Name: name, Hosts: append([]string(nil), hosts...)})
	return a.foregroundSession(len(a.open) - 1)
}

// foregroundSession makes the session at index the visible one. The pane focus
// moves to its first host - the previous focus belonged to panes that are no
// longer on screen - and the broadcast limit follows, so the next keystroke
// cannot reach a pane the user cannot see.
func (a App) foregroundSession(index int) App {
	if index < 0 || index >= len(a.open) {
		return a
	}
	a.active = index
	a.page = 0
	a.paneIndex = 0
	a.fullScreen = false
	return a.syncBroadcastLimit().syncFocusTarget()
}

// syncBroadcastLimit pushes the visible host set into the router. With no
// session open there is nothing to limit; with one open, all and selected mode
// stop at its edge.
func (a App) syncBroadcastLimit() App {
	if a.cfg.Targets == nil {
		return a
	}
	if a.active < 0 || a.active >= len(a.open) {
		a.cfg.Targets.SetLimit(nil)
		return a
	}
	a.cfg.Targets.SetLimit(a.sessionHosts())
	return a
}

// pruneSessions drops hosts that left the fleet from every open session, and
// drops sessions that lost their last host. The foreground session is kept by
// identity, the way pane focus is kept by host.
func (a App) pruneSessions() App {
	fleet := make(map[string]bool)
	for _, id := range a.fleetIDs() {
		fleet[id] = true
	}

	activeName := a.ActiveSession()
	kept := a.open[:0]
	for _, s := range a.open {
		var hosts []string
		for _, id := range s.Hosts {
			if fleet[id] {
				hosts = append(hosts, id)
			}
		}
		if len(hosts) == 0 {
			continue
		}
		s.Hosts = hosts
		kept = append(kept, s)
	}
	a.open = kept

	a.active = -1
	for i, s := range a.open {
		if s.Name == activeName {
			a.active = i
			break
		}
	}
	if a.active < 0 && len(a.open) > 0 {
		a.active = len(a.open) - 1
	}
	a.sessionCursor = clamp(a.sessionCursor, 0, max(0, len(a.open)-1))
	return a.syncBroadcastLimit()
}

// adoptNewHosts assigns fleet hosts that belong to no open session to the
// foreground session - a host connected with n joins what the user is looking
// at. With nothing open the hosts open the ad hoc session, named after the
// run when it has a name.
func (a App) adoptNewHosts() App {
	owned := make(map[string]bool)
	for _, s := range a.open {
		for _, id := range s.Hosts {
			owned[id] = true
		}
	}
	var orphans []string
	for _, id := range a.fleetIDs() {
		if !owned[id] {
			orphans = append(orphans, id)
		}
	}
	if len(orphans) == 0 {
		return a
	}

	if a.active >= 0 && a.active < len(a.open) {
		a.open[a.active].Hosts = append(a.open[a.active].Hosts, orphans...)
		return a.syncBroadcastLimit()
	}
	name := a.cfg.SessionName
	if name == "" {
		name = adHocSessionName
	}
	return a.openSessionAt(name, orphans)
}

// gridChanged tells the program the visible panes changed shape, so the
// remote PTYs can be resized to match what is drawn.
func gridChanged() tea.Cmd {
	return func() tea.Msg { return GridChangedMsg{} }
}
