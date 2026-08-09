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
	// CollapsedPanelHeight is the smallest titled sidebar box that still has a
	// body row: the title line, one content row, the bottom border.
	CollapsedPanelHeight = 3
	// PreviewPanelMaxHeight caps how tall an unselected panel grows on a roomy
	// sidebar. Previews orient, they do not compete with the selected panel.
	PreviewPanelMaxHeight = 8
	// BroadcastBarHeight is the always-visible broadcast input: a titled box
	// with one content line.
	BroadcastBarHeight = 3
)

// SidebarHeights divides the sidebar's height over the stacked panels, lazygit
// style: every unselected panel collapses to a titled box and the selected one
// takes everything that is left.
//
// When there is height to spare beyond the collapsed boxes, the unselected
// panels grow into previews — half of the surplus is split between them, capped
// at [PreviewPanelMaxHeight] — so Status, Groups and Sessions stay readable
// while something else has the keyboard. The selected panel always keeps at
// least the other half.
//
// When even the collapsed boxes do not fit, the unselected panels shrink to a
// bare one-line title, and when that does not fit either they vanish and the
// selected panel gets the whole column. Heights are never negative and always
// sum to at most total, so a hostile resize cannot push a renderer below zero.
func SidebarHeights(total, panels, selected int) []int {
	return SidebarHeightsMode(total, panels, selected, false)
}

// SidebarHeightsMode is [SidebarHeights] with the half-mode variant: when
// expanded is true the unselected panels give up their preview rows and
// collapse to bare titled boxes, so the selected panel takes every row the
// column can spare. That is what half mode means on the sidebar - the panel
// under the keyboard gets the height, the others still say they are there.
func SidebarHeightsMode(total, panels, selected int, expanded bool) []int {
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
		// Room for every box plus a selected panel at least as tall. Any
		// surplus beyond that buys preview rows for the unselected panels -
		// unless half mode asked for the selected panel to have them.
		if panels > 1 && !expanded {
			surplus := total - CollapsedPanelHeight*panels
			preview := surplus / 2 / (panels - 1)
			collapsed = min(CollapsedPanelHeight+preview, PreviewPanelMaxHeight)
		}
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
	// Broadcast is the always-visible broadcast input between the grid and
	// the status bar. It shares its rows with the sidebar rather than
	// stretching under it, so the panel column keeps its full height. Empty
	// when the terminal is too short to hold it.
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
	return ComputeScreenLayout(width, height, ScreenNormal, AreaSidebar)
}

// ComputeScreenLayout is [ComputeLayout] for a given screen mode and focused
// area.
//
// Only [ScreenHalf] changes the geometry, and it changes exactly one number -
// how the columns are split between the sidebar and the grid:
//
//   - sidebar focused: the panel column takes half the width instead of a
//     quarter, so a group or session list can be read without opening it.
//   - grid or broadcast focused: the panel column shrinks to its minimum, so
//     the panes get every column it can spare.
//
// [ScreenFull] is a property of the grid, not of the frame: it draws one pane
// in the main area (see renderMain), which is what alt+z has always meant, so
// the rects here are the normal ones.
func ComputeScreenLayout(width, height int, mode ScreenMode, focus Area) Layout {
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

	sidebarWidth := 0
	if width >= SidebarMinWidth+MainMinWidth {
		switch {
		case mode == ScreenHalf && focus == AreaSidebar:
			// Half the terminal, and the cap that keeps the sidebar modest on
			// a wide screen does not apply: the user asked for the room.
			sidebarWidth = max(width/2, SidebarMinWidth)
		case mode == ScreenHalf:
			// The grid (or the bar under it) has the keyboard, so the panel
			// column keeps only what it needs to stay legible.
			sidebarWidth = SidebarMinWidth
		default:
			sidebarWidth = width / 4
			if sidebarWidth < SidebarMinWidth {
				sidebarWidth = SidebarMinWidth
			}
			if sidebarWidth > SidebarMaxWidth {
				sidebarWidth = SidebarMaxWidth
			}
		}
		// Never at the cost of the grid: on a narrow terminal the panel list
		// gives up its share rather than squeezing the output to nothing.
		if width-sidebarWidth < MainMinWidth {
			sidebarWidth = width - MainMinWidth
		}
	}

	if sidebarWidth > 0 {
		// The sidebar runs the whole body height, down to the status bar: the
		// broadcast bar sits beside it, under the grid only.
		l.Sidebar = Rect{X: 0, Y: 0, Width: sidebarWidth, Height: bodyHeight}
	}

	// The broadcast bar is taken off the grid's rows and degrades before the
	// grid does: a titled box when there is room, a bare line when it is
	// tight, gone when even the grid barely fits.
	barHeight := 0
	switch {
	case height >= MinHeight+BroadcastBarHeight:
		barHeight = BroadcastBarHeight
	case height >= MinHeight+1:
		barHeight = 1
	}
	if barHeight > 0 {
		bodyHeight -= barHeight
		l.Broadcast = Rect{X: sidebarWidth, Y: bodyHeight, Width: width - sidebarWidth, Height: barHeight}
	}
	l.Main = Rect{X: sidebarWidth, Y: 0, Width: width - sidebarWidth, Height: bodyHeight}

	return l
}
