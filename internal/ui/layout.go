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
)

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
	// StatusBar is the bar along the bottom.
	StatusBar Rect
	// TooSmall reports that the terminal cannot hold the interface at all. The
	// root model renders a single line saying so instead of drawing.
	TooSmall bool
}

// SidebarVisible reports whether the panel list is drawn at this size.
func (l Layout) SidebarVisible() bool { return !l.Sidebar.Empty() }

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
