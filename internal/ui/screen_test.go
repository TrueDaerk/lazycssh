package ui

import (
	"strings"
	"testing"
)

// screenCycle is the binding, in the form the terminal reports it.
const screenCycle = "alt++"

func TestScreenModeCycleAndNames(t *testing.T) {
	tests := []struct {
		mode ScreenMode
		next ScreenMode
		name string
	}{
		{ScreenNormal, ScreenHalf, "normal"},
		{ScreenHalf, ScreenFull, "half"},
		{ScreenFull, ScreenNormal, "full"},
		// An out-of-range mode cannot come from a binding; if one ever did, it
		// must lead back to the default rather than stranding the view.
		{ScreenMode(42), ScreenNormal, "normal"},
	}
	for _, tc := range tests {
		if got := tc.mode.Next(); got != tc.next {
			t.Errorf("ScreenMode(%d).Next() = %v, want %v", tc.mode, got, tc.next)
		}
		if got := tc.mode.String(); got != tc.name {
			t.Errorf("ScreenMode(%d).String() = %q, want %q", tc.mode, got, tc.name)
		}
	}
}

// The acceptance criterion: one binding cycles all three modes, from the grid
// and from the app level alike.
func TestScreenCycleBindingCyclesAllThreeModes(t *testing.T) {
	for _, area := range []struct {
		name  string
		build func(*testing.T) App
	}{
		{"grid", func(t *testing.T) App { return fleetApp(t, 6) }},
		{"app level", func(t *testing.T) App {
			return resize(t, testApp(), 200, 60)
		}},
	} {
		t.Run(area.name, func(t *testing.T) {
			a := area.build(t)
			if a.Screen() != ScreenNormal {
				t.Fatalf("setup: Screen() = %v", a.Screen())
			}

			for _, want := range []ScreenMode{ScreenHalf, ScreenFull, ScreenNormal} {
				a = pressKey(t, a, screenCycle)
				if a.Screen() != want {
					t.Fatalf("Screen() = %v, want %v", a.Screen(), want)
				}
				if a.FullScreen() != (want == ScreenFull) {
					t.Fatalf("FullScreen() = %v in %v mode", a.FullScreen(), want)
				}
			}
		})
	}
}

// alt+z keeps its old meaning: straight to full screen and straight back, from
// whatever mode the view is in.
func TestFullScreenBindingStillTogglesDirectly(t *testing.T) {
	a := fleetApp(t, 6)

	a = pressKey(t, a, "alt+z")
	if a.Screen() != ScreenFull {
		t.Fatalf("alt+z from normal left the view in %v", a.Screen())
	}
	a = pressKey(t, a, "alt+z")
	if a.Screen() != ScreenNormal {
		t.Fatalf("alt+z from full left the view in %v", a.Screen())
	}

	// From half mode it is one press to full, not two.
	a = pressKey(t, a, screenCycle)
	if a.Screen() != ScreenHalf {
		t.Fatalf("setup: Screen() = %v", a.Screen())
	}
	a = pressKey(t, a, "alt+z")
	if a.Screen() != ScreenFull {
		t.Fatalf("alt+z from half left the view in %v", a.Screen())
	}
}

// Half mode with the grid focused: the focused pane is roughly half the screen,
// which means fewer panes on the page and the rest paging.
func TestHalfModeEnlargesTheFocusedPane(t *testing.T) {
	a := fleetApp(t, 6)

	normal, ok := a.Grid().Cell(a.PaneIndex())
	if !ok {
		t.Fatal("setup: the focused pane has no cell")
	}

	a = pressKey(t, a, screenCycle)
	half, ok := a.Grid().Cell(a.PaneIndex())
	if !ok {
		t.Fatal("the focused pane has no cell in half mode")
	}

	if half.Width*half.Height <= normal.Width*normal.Height {
		t.Fatalf("half mode did not enlarge the pane: %+v then %+v", normal, half)
	}
	if a.Grid().PerPage > HalfScreenPanes {
		t.Fatalf("half mode shows %d panes per page, want at most %d",
			a.Grid().PerPage, HalfScreenPanes)
	}
	if a.Grid().Pages < 2 {
		t.Fatalf("six hosts fit on %d page(s) with two panes per page", a.Grid().Pages)
	}

	// The hidden hosts are announced in the grid, not only implied by a
	// smaller page: a capped page must never read as the whole run.
	view := plain(a.View().Content)
	if !strings.Contains(view, "ctrl+shift+→") {
		t.Fatalf("half mode hides hosts without saying so:\n%s", view)
	}
	if !strings.Contains(view, "screen half") {
		t.Fatalf("the status bar does not name the screen mode:\n%s", view)
	}

	// Full screen is one pane, and normal comes back to the full tiling.
	a = pressKey(t, a, screenCycle)
	if !strings.Contains(plain(a.View().Content), "screen full") {
		t.Fatal("the status bar does not name full screen")
	}
	a = pressKey(t, a, screenCycle)
	if got := a.Grid().PerPage; got != 6 {
		t.Fatalf("PerPage = %d back in normal mode, want the six hosts", got)
	}
	back := plain(a.View().Content)
	if strings.Contains(back, "screen half") || strings.Contains(back, "screen full") {
		t.Fatalf("normal mode still announces a screen mode:\n%s", back)
	}
}

// Cycling into half mode keeps the focused pane on screen: capping the page
// must not leave the keyboard pointed at a pane the user cannot see.
func TestHalfModeFollowsTheFocusedPane(t *testing.T) {
	a := fleetApp(t, 6)
	for range 5 {
		a = pressKey(t, a, "alt+shift+right")
	}
	if a.FocusedHost() != "web-06" {
		t.Fatalf("setup: FocusedHost() = %q", a.FocusedHost())
	}

	a = pressKey(t, a, screenCycle)
	if got := a.WindowHosts(); len(got) == 0 || got[len(got)-1] != "web-06" {
		t.Fatalf("the focused host is off screen in half mode: %v", got)
	}
	if !strings.Contains(plain(a.View().Content), "web-06") {
		t.Fatal("the focused pane is not drawn in half mode")
	}
}

// Half mode with the sidebar focused is about the sidebar: the panel column
// takes half the width and the selected panel takes the rows the previews had.
func TestHalfModeWidensTheSidebar(t *testing.T) {
	a := resize(t, testApp(), 200, 60)
	normalWidth := a.Layout().Sidebar.Width
	normalHeights := a.sidebarHeights()

	a = pressKey(t, a, screenCycle)
	if a.Screen() != ScreenHalf {
		t.Fatalf("setup: Screen() = %v", a.Screen())
	}

	half := a.Layout()
	if half.Sidebar.Width <= normalWidth {
		t.Fatalf("Sidebar.Width = %d in half mode, was %d", half.Sidebar.Width, normalWidth)
	}
	if half.Sidebar.Width+half.Main.Width != half.Width {
		t.Fatalf("the sidebar and the grid do not cover the terminal: %+v", half)
	}
	if half.Main.Width < MainMinWidth {
		t.Fatalf("the grid was squeezed to %d columns", half.Main.Width)
	}

	heights := a.sidebarHeights()
	selected := int(a.Panel())
	if heights[selected] <= normalHeights[selected] {
		t.Fatalf("the selected panel got %d rows in half mode, had %d",
			heights[selected], normalHeights[selected])
	}
	for i, h := range heights {
		if i != selected && h != CollapsedPanelHeight {
			t.Fatalf("unselected panel %d kept %d rows in half mode", i, h)
		}
	}
}

// Entering a pane in half mode hands the width back to the grid: half mode is
// always about whatever has the keyboard.
func TestHalfModeFollowsTheFocus(t *testing.T) {
	a := pressKey(t, resize(t, testApp(), 200, 60), screenCycle)
	sidebarFocused := a.Layout().Sidebar.Width

	a = focusGrid(t, a)
	gridFocused := a.Layout().Sidebar.Width
	if gridFocused >= sidebarFocused {
		t.Fatalf("Sidebar.Width = %d with the grid focused, was %d with the sidebar focused",
			gridFocused, sidebarFocused)
	}
	if gridFocused != SidebarMinWidth {
		t.Fatalf("Sidebar.Width = %d with the grid focused, want the minimum %d",
			gridFocused, SidebarMinWidth)
	}
}

// The reason the cycle is a chord: lazygit's plain + is a character a shell
// wants, and while a pane has the keyboard it has to reach the host.
func TestPlainPlusStillReachesTheHost(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = focusGrid(t, syncFleet(t, a))

	a = pressKey(t, a, "+")
	if a.Screen() != ScreenNormal {
		t.Fatalf("a plain + changed the screen mode to %v", a.Screen())
	}
	if got := fleet.sessions["web-01"].Written(); !strings.Contains(got, "+") {
		t.Fatalf("the host did not receive the plus: %q", got)
	}
}

// The mode is a view setting, so it does not outlive the run it was set for.
func TestScreenModeResetsWithTheRun(t *testing.T) {
	a := pressKey(t, fleetApp(t, 3), "alt+z")
	if a.Screen() != ScreenFull {
		t.Fatalf("setup: Screen() = %v", a.Screen())
	}
	if got := a.resetToStart().Screen(); got != ScreenNormal {
		t.Fatalf("Screen() = %v after the run emptied, want normal", got)
	}
}
