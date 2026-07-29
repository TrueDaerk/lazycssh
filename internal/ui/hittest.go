package ui

// Mouse hit-testing. Everything here is pure arithmetic over the layout the
// renderer draws from - the same rects, the same height splits - so a click
// can be resolved without a terminal and the tests stay table-driven.

// Region is the part of the frame a point falls into.
type Region int

const (
	// RegionNone is outside every rect.
	RegionNone Region = iota
	// RegionSidebar is the stacked panel column.
	RegionSidebar
	// RegionMain is the pane grid.
	RegionMain
	// RegionBroadcast is the broadcast bar.
	RegionBroadcast
	// RegionStatus is the status bar.
	RegionStatus
)

// contains reports whether a point is inside the rect.
func (r Rect) contains(x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

// regionAt resolves a point to the frame region it falls into.
func (l Layout) regionAt(x, y int) Region {
	switch {
	case l.Sidebar.contains(x, y):
		return RegionSidebar
	case l.Main.contains(x, y):
		return RegionMain
	case l.Broadcast.contains(x, y):
		return RegionBroadcast
	case l.StatusBar.contains(x, y):
		return RegionStatus
	default:
		return RegionNone
	}
}

// sidebarPanelAt resolves a y offset inside the sidebar to the panel box it
// falls into and the row within that panel's body. bodyRow is -1 on the title
// line or a border, because clicking a title selects without moving a cursor.
func sidebarPanelAt(heights []int, y int) (panel, bodyRow int, ok bool) {
	top := 0
	for i, h := range heights {
		if h <= 0 {
			continue
		}
		if y < top+h {
			row := y - top - 1 // the title line is row -1... 0 is the first body row
			if row >= h-2 && h >= 3 {
				row = -1 // the bottom border
			}
			if h < 3 {
				row = -1 // collapsed boxes have no body
			}
			return i, row, true
		}
		top += h
	}
	return 0, -1, false
}

// paneAt resolves an absolute point to the host index of the pane under it.
// Cells are absolute rects on the current page; full screen means the whole
// main area is the focused pane.
func paneAt(g Grid, main Rect, page, count, focused int, fullScreen bool, x, y int) (int, bool) {
	if fullScreen {
		if main.contains(x, y) && focused >= 0 && focused < count {
			return focused, true
		}
		return 0, false
	}
	for slot, cell := range g.Cells {
		if !cell.contains(x, y) {
			continue
		}
		index := page*g.PerPage + slot
		if index >= count {
			return 0, false // the empty slot on the last page
		}
		return index, true
	}
	return 0, false
}

// paneCloseHit reports whether a point inside a pane's cell is on the [x]
// button at the right end of its header line.
func paneCloseHit(cell Rect, x, y int) bool {
	if y != cell.Y+1 {
		return false
	}
	right := cell.X + cell.Width - 2 // inside the border
	return x > right-len(paneCloseButton) && x <= right
}
