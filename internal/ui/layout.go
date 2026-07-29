package ui

// Layout constants. They are the smallest sizes at which the interface still
// says something true; below them it says so rather than drawing nonsense.
const (
	// SidebarMinWidth is the narrowest a useful panel list can be.
	SidebarMinWidth = 20
	// SidebarMaxWidth stops the sidebar eating a wide terminal.
	SidebarMaxWidth = 34
	// MainMinWidth is the narrowest the pane grid can be and still show
	// output.
	MainMinWidth = 24
	// StatusBarHeight is the height of the bar along the bottom. It is never
	// dropped: the broadcast target count is the one thing the user must
	// always be able to see.
	StatusBarHeight = 1
	// MinWidth and MinHeight are the smallest terminal the interface draws in.
	// Smaller than this it renders a single message instead.
	MinWidth  = 24
	MinHeight = 4
	// CollapsedPanelHeight is a titled sidebar box with no body: the title
	// line, one empty row, the bottom border.
	CollapsedPanelHeight = 3
	// BroadcastBarHeight is the always-visible broadcast input: a titled box
	// with one content line.
	BroadcastBarHeight = 3
)

// SidebarHeights divides the sidebar's height over the stacked panels, lazygit
// style: every unselected panel collapses to a titled box and the selected one
// takes everything that is left.
//
// When even the collapsed boxes do not fit, the unselected panels shrink to a
// bare one-line title, and when that does not fit either they vanish and the
// selected panel gets the whole column. Heights are never negative and always
// sum to at most total, so a hostile resize cannot push a renderer below zero.
func SidebarHeights(total, panels, selected int) []int {
	if panels <= 0 {
		return nil
	}
	heights := make([]int, panels)
	if total <= 0 {
		return heights
	}
	selected = clamp(selected, 0, panels-1)

	collapsed := CollapsedPanelHeight
	switch {
	case total >= CollapsedPanelHeight*panels:
		// Room for every box plus a selected panel at least as tall.
	case total >= (panels-1)+CollapsedPanelHeight:
		collapsed = 1
	default:
		collapsed = 0
	}

	for i := range heights {
		if i != selected {
			heights[i] = collapsed
		}
	}
	heights[selected] = total - collapsed*(panels-1)
	return heights
}

// Rect is a region of the terminal. A zero-width or zero-height rect is not
// drawn.
type Rect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// Empty reports whether the rect has no area.
func (r Rect) Empty() bool { return r.Width <= 0 || r.Height <= 0 }

// Layout is where everything goes at a given terminal size.
//
// It is computed from nothing but the width and the height, so the arithmetic
// can be tested without a terminal - which matters, because a layout that
// underflows to a negative width is how a TUI panics on a resize.
type Layout struct {
	// Width and Height are the terminal size the layout was computed for.
	Width  int
	Height int
	// Sidebar is the numbered panel list on the left. Empty when the terminal
	// is too narrow to hold both it and a usable grid.
	Sidebar Rect
	// Main is the pane grid.
	Main Rect
	// Broadcast is the always-visible broadcast input between the body and
	// the status bar. Empty when the terminal is too short to hold it.
	Broadcast Rect
	// StatusBar is the bar along the bottom.
	StatusBar Rect
	// TooSmall reports that the terminal cannot hold the interface at all. The
	// root model renders a single line saying so instead of drawing.
	TooSmall bool
}

// SidebarVisible reports whether the panel list is drawn at this size.
func (l Layout) SidebarVisible() bool { return !l.Sidebar.Empty() }

// BroadcastVisible reports whether the broadcast bar is drawn at this size.
func (l Layout) BroadcastVisible() bool { return !l.Broadcast.Empty() }

// ComputeLayout divides a terminal into the sidebar, the grid and the status
// bar.
//
// Every returned rect is clamped to be non-negative, at every size, including
// sizes no terminal would report: a resize must never produce a negative width
// that a renderer then tries to fill.
func ComputeLayout(width, height int) Layout {
	l := Layout{Width: width, Height: height}

	if width < MinWidth || height < MinHeight {
		l.TooSmall = true
		return l
	}

	// The status bar is taken off the bottom first. It survives every size
	// above the minimum, because the target count is what tells the user how
	// many machines their next keystroke reaches.
	bodyHeight := height - StatusBarHeight
	l.StatusBar = Rect{X: 0, Y: bodyHeight, Width: width, Height: StatusBarHeight}

	// The broadcast bar sits above it and degrades before the body does: a
	// titled box when there is room, a bare line when it is tight, gone when
	// even the body barely fits.
	barHeight := 0
	switch {
	case height >= MinHeight+BroadcastBarHeight:
		barHeight = BroadcastBarHeight
	case height >= MinHeight+1:
		barHeight = 1
	}
	if barHeight > 0 {
		bodyHeight -= barHeight
		l.Broadcast = Rect{X: 0, Y: bodyHeight, Width: width, Height: barHeight}
	}

	sidebarWidth := 0
	if width >= SidebarMinWidth+MainMinWidth {
		sidebarWidth = width / 4
		if sidebarWidth < SidebarMinWidth {
			sidebarWidth = SidebarMinWidth
		}
		if sidebarWidth > SidebarMaxWidth {
			sidebarWidth = SidebarMaxWidth
		}
		// Never at the cost of the grid: on a narrow terminal the panel list
		// gives up its share rather than squeezing the output to nothing.
		if width-sidebarWidth < MainMinWidth {
			sidebarWidth = width - MainMinWidth
		}
	}

	if sidebarWidth > 0 {
		l.Sidebar = Rect{X: 0, Y: 0, Width: sidebarWidth, Height: bodyHeight}
	}
	l.Main = Rect{X: sidebarWidth, Y: 0, Width: width - sidebarWidth, Height: bodyHeight}

	return l
}
