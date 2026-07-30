package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
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
	// Ending marks a session the user asked to end with x: its terminals
	// were sent ctrl+c and ctrl+d, and the session leaves the list as soon
	// as every host is done - closed or failed - rather than waiting for a
	// host that died mid-shutdown.
	Ending bool
	// SawClose remembers that a host of this session logged out cleanly.
	// Closed panes leave the run on their own (issue #146), so by the time
	// only failed hosts remain the logouts are no longer visible in the
	// host states - this flag is what lets such a session still end instead
	// of a dead-on-arrival host keeping it, and the program, alive.
	SawClose bool
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

// sessionHosts returns the foreground session's hosts in session order, with
// a hole ("") where a host left the run: the pane grid keeps its positions
// until an explicit retile (issue #169). With nothing open it falls back to
// the whole fleet, which is also how the views are tested without sessions.
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
		if id != "" && fleet[id] {
			out = append(out, id)
			continue
		}
		out = append(out, "")
	}
	return out
}

// nonHoles returns ids without the "" hole markers: the hosts that actually
// exist, which is what broadcast limits and counts are about.
func nonHoles(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" {
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
			if id != "" {
				known[id] = true
			}
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
	// A session switch is an explicit view change: it tiles for what the new
	// session actually holds - holes compacted, kept shape dropped.
	a.open[index].Hosts = nonHoles(a.open[index].Hosts)
	return a.resetGridSlots().syncBroadcastLimit().syncFocusTarget()
}

// syncBroadcastLimit pushes the visible host set into the router. With no
// session open and no filter there is nothing to limit; otherwise all and
// selected mode stop at the edge of what is on screen.
func (a App) syncBroadcastLimit() App {
	if a.cfg.Targets == nil {
		return a
	}
	if (a.active < 0 || a.active >= len(a.open)) && !a.connectedOnly && a.splitSize <= 0 {
		a.cfg.Targets.SetLimit(nil)
		return a
	}
	// Holes are grid positions, not hosts: a keystroke cannot go to one.
	a.cfg.Targets.SetLimit(nonHoles(a.visibleHosts()))
	return a
}

// pruneSessions turns hosts that left the fleet into holes ("") in every open
// session - the pane positions must survive a departure until an explicit
// retile (issue #169) - and drops sessions that lost their last real host. The
// foreground session is kept by identity, the way pane focus is kept by host.
func (a App) pruneSessions() App {
	fleet := make(map[string]bool)
	for _, id := range a.fleetIDs() {
		fleet[id] = true
	}

	activeName := a.ActiveSession()
	kept := a.open[:0]
	for _, s := range a.open {
		hosts := make([]string, 0, len(s.Hosts))
		real := 0
		for _, id := range s.Hosts {
			if id != "" && fleet[id] {
				hosts = append(hosts, id)
				real++
				continue
			}
			hosts = append(hosts, "")
		}
		if real == 0 {
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

// sessionOver reports whether an open session has run its course: every host
// is done and at least one ended in a deliberate logout - seen live or
// remembered in SawClose after its pane left - or, for a session the user
// asked to end, every host done one way or the other. A session whose hosts
// merely all *failed* is not over: that is an outage to look at and
// reconnect, not a completed shutdown. But a failed host must not keep a
// session - and with it the program - alive after every other host was
// logged out (issue #146).
func (a App) sessionOver(s openSession) bool {
	if len(nonHoles(s.Hosts)) == 0 {
		return true
	}
	sawClose, doneAll := s.SawClose, true
	for _, id := range nonHoles(s.Hosts) {
		st := a.state(id)
		if st == ssh.StateClosed {
			sawClose = true
		}
		if !st.Done() {
			doneAll = false
		}
	}
	return doneAll && (sawClose || s.Ending)
}

// reapSessions ends every session that is over: it leaves the list, and its
// hosts leave the run - unless another open session still contains them, in
// which case they stay untouched. Called on every fleet event, which is how
// "ctrl+d broadcast to the whole session" ends the session without another
// keypress.
func (a App) reapSessions() (App, tea.Cmd) {
	// Record clean logouts before judging the sessions, so a session whose
	// last live host just logged out ends in this same pass even though its
	// closed panes are about to leave the run.
	for i := range a.open {
		for _, id := range a.open[i].Hosts {
			if a.state(id) == ssh.StateClosed {
				a.open[i].SawClose = true
				break
			}
		}
	}

	var ended []openSession
	activeName := a.ActiveSession()
	focused := a.FocusedHost()
	kept := a.open[:0]
	for _, s := range a.open {
		if a.sessionOver(s) {
			ended = append(ended, s)
			continue
		}
		kept = append(kept, s)
	}
	a.open = kept

	var cmds []tea.Cmd
	removed := make(map[string]bool)
	if len(ended) > 0 {
		// The foreground is kept by identity; losing it falls back to what is
		// still open, the way pruning does.
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
		if a.ActiveSession() != activeName {
			// The foreground fell back to a different session; a shape kept
			// from the ended one would be a museum of the wrong view.
			a = a.resetGridSlots()
		}
		a.sessionCursor = clamp(a.sessionCursor, 0, max(0, len(a.open)-1))

		// An ended session's hosts leave the run, deduplicated, unless a
		// surviving session still holds a host that is not itself done.
		surviving := make(map[string]bool)
		for _, s := range a.open {
			for _, id := range s.Hosts {
				surviving[id] = true
			}
		}
		fleet := make(map[string]bool)
		for _, id := range a.fleetIDs() {
			fleet[id] = true
		}
		cmds = append(cmds, gridChanged())
		for _, s := range ended {
			for _, id := range s.Hosts {
				if surviving[id] || removed[id] || !fleet[id] {
					continue
				}
				removed[id] = true
				host := id
				cmds = append(cmds, func() tea.Msg { return RemoveHostMsg{ID: host} })
			}
		}
	}

	// A cleanly logged-out shell has nothing left to show: its pane leaves
	// the run on its own (issue #146), whichever sessions still list the
	// host - the shell is just as gone in all of them. The removal comes
	// back as HostsChangedMsg, which keeps the grid shape: no retile.
	for _, id := range a.fleetIDs() {
		if removed[id] || a.state(id) != ssh.StateClosed {
			continue
		}
		removed[id] = true
		host := id
		cmds = append(cmds, func() tea.Msg { return RemoveHostMsg{ID: host} })
	}

	if len(cmds) == 0 {
		return a, nil
	}
	a = a.syncBroadcastLimit().refocus(focused).followFocus()
	return a, tea.Batch(cmds...)
}
