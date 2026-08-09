package ui

import tea "charm.land/bubbletea/v2"

// Screen modes are how much of the terminal the focused thing gets, lazygit
// style: normal tiles everything, half gives the focused area roughly half the
// screen, full gives it everything.
//
// The mode is a *view* setting, not a state of the run: it never changes who
// receives a keystroke, only how much room the thing under the keyboard has.

// ScreenMode is how much of the terminal the focused area takes.
type ScreenMode int

const (
	// ScreenNormal is the default tiling: a quarter-width sidebar and a grid
	// of equal panes.
	ScreenNormal ScreenMode = iota
	// ScreenHalf gives the focused area roughly half the terminal: a
	// half-width sidebar with the selected panel expanded, or a grid squeezed
	// to two panes so the focused one is half the width of the screen.
	ScreenHalf
	// ScreenFull is one pane in the whole main area - reading a stack trace,
	// driving an interactive program.
	ScreenFull
)

// HalfScreenPanes is how many panes the grid shows in half mode: the focused
// one and one neighbour, so "half the screen" is literally what the focused
// pane gets. The rest of the hosts page, and the overflow footer says so.
const HalfScreenPanes = 2

// String names the mode for the status bar and for test failures.
func (m ScreenMode) String() string {
	switch m {
	case ScreenNormal:
		return "normal"
	case ScreenHalf:
		return "half"
	case ScreenFull:
		return "full"
	default:
		return "normal"
	}
}

// Next is the cycle: normal → half → full → normal. An out-of-range mode
// cycles back to normal rather than wandering off, because a view setting must
// never be able to strand the user.
func (m ScreenMode) Next() ScreenMode {
	switch m {
	case ScreenNormal:
		return ScreenHalf
	case ScreenHalf:
		return ScreenFull
	default:
		return ScreenNormal
	}
}

// Screen is the current screen mode.
func (a App) Screen() ScreenMode { return a.screen }

// screenLabel is what the status bar says while the screen mode is not normal.
// Normal says nothing: the default needs no announcement, and the bar's space
// belongs to the things that change what a keystroke does.
func (a App) screenLabel() string {
	if a.screen == ScreenNormal {
		return ""
	}
	return "screen " + a.screen.String()
}

// setScreen switches the screen mode. The window follows the pane focus, so
// the pane the mode was cycled for is one the user can still see when the
// grid's shape changed under it, and the remotes are told about the new cell
// size.
func (a App) setScreen(m ScreenMode) (App, tea.Cmd) {
	a.screen = m
	return a.followFocus(), gridChanged()
}

// cycleScreen is the screen-mode binding: normal → half → full → normal.
func (a App) cycleScreen() (App, tea.Cmd) { return a.setScreen(a.screen.Next()) }

// toggleFullScreen is alt+z: the direct way in and out of full screen from any
// mode, kept as its own binding because zooming one pane is the thing users
// reach for mid-typing and it should never take two presses.
func (a App) toggleFullScreen() (App, tea.Cmd) {
	if a.screen == ScreenFull {
		return a.setScreen(ScreenNormal)
	}
	return a.setScreen(ScreenFull)
}

// syncLayout recomputes the frame after every message, because the geometry
// depends on more than the terminal size now: the screen mode and which area
// has the keyboard both move the column split, and focus changes all over the
// model. Recomputing once here is the only way the layout cannot drift out of
// step with the focus - the same reason the broadcast limit is resynced there.
//
// A model that has not been sized yet is left alone: ComputeLayout of a
// zero-sized terminal is "too small", and the first WindowSizeMsg is on its
// way.
func (a App) syncLayout() App {
	if a.layout.Width <= 0 || a.layout.Height <= 0 {
		return a
	}
	a.layout = ComputeScreenLayout(a.layout.Width, a.layout.Height, a.screen, a.focus)
	return a
}

// gridPaneCap is how many panes the grid may show on one page, 0 for "as many
// as fit". Half mode caps it while the grid has the keyboard: fewer, bigger
// panes is the whole point, and capping only under grid focus keeps half mode
// about whatever the user is actually looking at.
func (a App) gridPaneCap() int {
	if a.screen == ScreenHalf && a.focus == AreaGrid {
		return HalfScreenPanes
	}
	return 0
}

// sidebarExpanded reports whether the selected sidebar panel should take every
// row it can - half mode with the sidebar focused. The unselected panels give
// up their previews for it.
func (a App) sidebarExpanded() bool {
	return a.screen == ScreenHalf && a.focus == AreaSidebar
}

// sidebarHeights is the panel column's height split, computed the one way the
// renderer and the hit-testing both use it.
func (a App) sidebarHeights() []int {
	return SidebarHeightsMode(a.layout.Sidebar.Height, len(Panels()), int(a.panel), a.sidebarExpanded())
}
