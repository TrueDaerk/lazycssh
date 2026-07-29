package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// The connected-only filter: ctrl+a narrows the grid to the hosts that can
// take input right now, and the broadcast limit follows, so a keystroke never
// reaches a pane the user cannot see. The filter is a view, not a removal - a
// host that reconnects reappears without a keypress, because the visible list
// is computed from live state on every render.

// ConnectedOnly reports whether the connected-only filter is on.
func (a App) ConnectedOnly() bool { return a.connectedOnly }

// visibleHosts is the session's hosts after the visibility filters: what the
// grid draws and what all/selected broadcast may reach.
func (a App) visibleHosts() []string {
	ids := a.sessionHosts()
	if !a.connectedOnly {
		return ids
	}
	var out []string
	for _, id := range ids {
		if a.state(id) == ssh.StateConnected {
			out = append(out, id)
		}
	}
	return out
}

// toggleConnectedOnly flips the filter. The pane focus is kept by host
// identity where possible - the pane may move, the host does not change its
// name - and the PTYs are asked to match the new grid shape.
func (a App) toggleConnectedOnly() (App, tea.Cmd) {
	focused := a.FocusedHost()
	a.connectedOnly = !a.connectedOnly
	a.page = 0
	a = a.refocus(focused).followFocus().syncBroadcastLimit()
	return a, gridChanged()
}
