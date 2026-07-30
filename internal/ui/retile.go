package ui

import (
	tea "charm.land/bubbletea/v2"
)

// The grid keeps its shape when hosts leave: the freed cells render as empty
// frames, the way the last page's empty slots always have, and nothing jumps
// about under the user's eyes (or under the remote PTYs). ctrl+r re-tiles on
// request. Growth is immediate - a new pane has to appear somewhere - and an
// explicit view change (session switch, filter, split) re-tiles too, because
// the user asked for a different view, not for a museum of the old one.

// gridSlots is the number of cells the grid is tiled for: the visible hosts,
// or the larger count the shape was kept from.
func (a App) gridSlots() int {
	return max(len(a.hostIDs()), a.keptSlots)
}

// keepGridSlots raises the kept slot count to the current host count, so a
// later departure leaves an empty cell instead of reflowing every pane. It
// never lowers - lowering is what ctrl+r and the explicit view changes are
// for.
func (a App) keepGridSlots() App {
	if n := len(a.hostIDs()); n > a.keptSlots {
		a.keptSlots = n
	}
	return a
}

// resetGridSlots drops the kept shape and tiles for what is actually there.
func (a App) resetGridSlots() App {
	a.keptSlots = len(a.hostIDs())
	return a
}

// retile is ctrl+r: recompute the squarest shape for the current count and
// tell the program to resize the PTYs to the new cells.
func (a App) retile() (App, tea.Cmd) {
	a = a.resetGridSlots().followFocus()
	return a, gridChanged()
}
