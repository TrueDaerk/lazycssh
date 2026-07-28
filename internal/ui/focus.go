package ui

// HostsChangedMsg replaces the host list: a session was merged in, a host was
// closed, a reconnect renamed nothing but the order changed.
//
// The focused host is preserved by identity rather than by position. A list that
// shifts under the cursor must not silently move the user onto a different
// machine - that is the pane they are about to type into.
type HostsChangedMsg struct {
	Hosts []string
}

// FocusedHost is the host whose pane has focus, or the empty string when the
// run has no hosts.
func (a App) FocusedHost() string {
	if a.paneIndex < 0 || a.paneIndex >= len(a.cfg.Hosts) {
		return ""
	}
	return a.cfg.Hosts[a.paneIndex]
}

// PaneIndex is the position of the focused pane in the host list.
func (a App) PaneIndex() int { return a.paneIndex }

// withHosts replaces the host list and keeps the focus where the user left it:
// on the same host if it is still there, otherwise clamped to the nearest
// position that exists.
func (a App) withHosts(hosts []string) App {
	focused := a.FocusedHost()
	a.cfg.Hosts = hosts

	if focused != "" {
		for i, id := range hosts {
			if id == focused {
				a.paneIndex = i
				return a
			}
		}
	}
	a.paneIndex = clamp(a.paneIndex, 0, len(hosts)-1)
	return a
}

// movePane moves the pane focus by delta, stopping at the ends rather than
// wrapping. Wrapping from the last pane to the first is how a user ends up
// typing into the machine at the other end of the fleet.
func (a App) movePane(delta int) App {
	a.paneIndex = clamp(a.paneIndex+delta, 0, len(a.cfg.Hosts)-1)
	return a
}

// movePanel moves the sidebar selection by delta, stopping at the ends.
func (a App) movePanel(delta int) App {
	panels := Panels()
	next := clamp(int(a.panel)+delta, 0, len(panels)-1)
	a.panel = Panel(next)
	return a
}

// clamp keeps v inside [lo, hi]. An empty range - hi below lo, which is what an
// empty host list produces - clamps to lo.
func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
