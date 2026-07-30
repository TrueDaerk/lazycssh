package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// click drives a left click at absolute terminal coordinates.
func click(t *testing.T, a App, x, y int) (App, tea.Msg) {
	t.Helper()
	model, cmd := a.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		return next, nil
	}
	return next, cmd()
}

// wheel drives one wheel notch at absolute terminal coordinates.
func wheel(t *testing.T, a App, x, y int, up bool) App {
	t.Helper()
	button := tea.MouseWheelDown
	if up {
		button = tea.MouseWheelUp
	}
	model, _ := a.Update(tea.MouseWheelMsg{X: x, Y: y, Button: button})
	return model.(App)
}

func TestRegionAt(t *testing.T) {
	l := ComputeLayout(120, 40)
	tests := []struct {
		name string
		x, y int
		want Region
	}{
		{"sidebar", l.Sidebar.X + 1, l.Sidebar.Y + 1, RegionSidebar},
		{"main", l.Main.X + 1, l.Main.Y + 1, RegionMain},
		{"broadcast", l.Broadcast.X + 1, l.Broadcast.Y, RegionBroadcast},
		{"status", 0, l.StatusBar.Y, RegionStatus},
		{"outside", 300, 300, RegionNone},
		{"negative", -1, -1, RegionNone},
	}
	for _, tc := range tests {
		if got := l.regionAt(tc.x, tc.y); got != tc.want {
			t.Errorf("%s: regionAt(%d,%d) = %v, want %v", tc.name, tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSidebarPanelAt(t *testing.T) {
	heights := []int{3, 20, 3, 3, 3} // panel 1 selected and expanded
	tests := []struct {
		name      string
		y         int
		wantPanel int
		wantRow   int
		wantOK    bool
	}{
		{"first title line", 0, 0, -1, true},
		{"first bottom border", 2, 0, -1, true},
		{"selected title", 3, 1, -1, true},
		{"selected first row", 4, 1, 0, true},
		{"selected fifth row", 8, 1, 4, true},
		{"selected bottom border", 22, 1, -1, true},
		{"third panel title", 23, 2, -1, true},
		{"past the end", 99, 0, 0, false},
	}
	for _, tc := range tests {
		panel, row, ok := sidebarPanelAt(heights, tc.y)
		if ok != tc.wantOK || (ok && (panel != tc.wantPanel || row != tc.wantRow)) {
			t.Errorf("%s: sidebarPanelAt(y=%d) = (%d, %d, %v), want (%d, %d, %v)",
				tc.name, tc.y, panel, row, ok, tc.wantPanel, tc.wantRow, tc.wantOK)
		}
	}
}

func TestPaneAt(t *testing.T) {
	main := Rect{X: 30, Y: 0, Width: 2 * MinPaneWidth, Height: 2 * MinPaneHeight}
	g := TileGrid(main, 4) // 2x2

	if index, ok := paneAt(g, main, 0, 4, 0, false, g.Cells[3].X+1, g.Cells[3].Y+1); !ok || index != 3 {
		t.Fatalf("paneAt(fourth cell) = (%d, %v)", index, ok)
	}
	// An empty slot on the last page is not a pane.
	if _, ok := paneAt(g, main, 0, 3, 0, false, g.Cells[3].X+1, g.Cells[3].Y+1); ok {
		t.Fatal("the empty slot resolved to a pane")
	}
	// Full screen: the whole main area is the focused pane.
	if index, ok := paneAt(g, main, 0, 4, 2, true, main.X+5, main.Y+5); !ok || index != 2 {
		t.Fatalf("paneAt(fullscreen) = (%d, %v)", index, ok)
	}
	// Outside the main area.
	if _, ok := paneAt(g, main, 0, 4, 0, false, 0, 0); ok {
		t.Fatal("a point outside the grid resolved to a pane")
	}
}

func TestPaneCloseHit(t *testing.T) {
	cell := Rect{X: 30, Y: 0, Width: 40, Height: 12}
	header := cell.Y + 1
	right := cell.X + cell.Width - 2

	if !paneCloseHit(cell, right, header) || !paneCloseHit(cell, right-2, header) {
		t.Fatal("a click on the [x] did not hit")
	}
	if paneCloseHit(cell, right-3, header) {
		t.Fatal("a click left of the [x] hit it")
	}
	if paneCloseHit(cell, right, header+1) {
		t.Fatal("a click below the header hit the [x]")
	}
}

// Clicking a pane focuses it - clicking a terminal is focusing it, so typing
// starts there.
func TestClickFocusesThePane(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01", "web-02", "web-03", "web-04")

	cell, ok := a.Grid().Cell(3)
	if !ok {
		t.Fatal("setup: no cell for the fourth pane")
	}
	a, _ = click(t, a, cell.X+2, cell.Y+3)

	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v after clicking a pane", a.Focus())
	}
	if a.FocusedHost() != "web-04" {
		t.Fatalf("FocusedHost() = %q", a.FocusedHost())
	}
}

// The [x] in the header closes a live host and removes a dead one, exactly
// like alt+x.
func TestClickOnTheCloseButton(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)

	cell, _ := a.Grid().Cell(0)
	_, msg := click(t, a, cell.X+cell.Width-2, cell.Y+1)
	if _, ok := msg.(CloseHostMsg); !ok {
		t.Fatalf("clicking [x] on a live host produced %T, want CloseHostMsg", msg)
	}

	fleet.fail(t, "web-01")
	a = syncFleet(t, a)
	_, msg = click(t, a, cell.X+cell.Width-2, cell.Y+1)
	if _, ok := msg.(RemoveHostMsg); !ok {
		t.Fatalf("clicking [x] on a dead host produced %T, want RemoveHostMsg", msg)
	}
}

// Clicking a sidebar row selects the panel and moves its cursor there.
func TestClickSelectsASidebarRow(t *testing.T) {
	a, _ := groupsApp(t) // Groups panel selected and expanded

	heights := SidebarHeights(a.Layout().Sidebar.Height, len(Panels()), int(PanelGroups))
	panelTop := heights[0]              // the Status box sits above
	a, _ = click(t, a, 2, panelTop+1+2) // title line, then the third body row

	if a.Panel() != PanelGroups || a.Focus() != AreaSidebar {
		t.Fatalf("Panel() = %v, Focus() = %v", a.Panel(), a.Focus())
	}
	if a.GroupCursor() != 2 {
		t.Fatalf("GroupCursor() = %d after clicking the third row", a.GroupCursor())
	}
}

// Clicking the broadcast bar hands it the keyboard.
func TestClickFocusesTheBroadcastBar(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	r := a.Layout().Broadcast
	a, _ = click(t, a, r.X+5, r.Y+1)
	if a.Focus() != AreaBroadcast {
		t.Fatalf("Focus() = %v after clicking the bar", a.Focus())
	}
}

// The wheel scrolls the pane under the pointer, not the focused one.
func TestWheelScrollsThePaneUnderThePointer(t *testing.T) {
	a, fleet := scrollApp(t, 200)
	a = pressKey(t, a, "ctrl+]") // focus away from the grid
	_ = fleet

	cell, _ := a.Grid().Cell(0)
	a = wheel(t, a, cell.X+2, cell.Y+3, true)
	if a.FollowingTail("web-01") {
		t.Fatal("a wheel notch did not scroll the pane back")
	}
	if a.Focus() == AreaGrid {
		t.Fatal("the wheel stole the focus")
	}

	for range 100 {
		a = wheel(t, a, cell.X+2, cell.Y+3, false)
	}
	if !a.FollowingTail("web-01") {
		t.Fatal("scrolling forward did not return to the tail")
	}
}

// The wheel over a sidebar list moves its cursor.
func TestWheelMovesTheSidebarCursor(t *testing.T) {
	a, _ := groupsApp(t)

	heights := SidebarHeights(a.Layout().Sidebar.Height, len(Panels()), int(PanelGroups))
	y := heights[0] + 2 // inside the Groups box
	a = wheel(t, a, 2, y, false)
	if a.GroupCursor() != 1 {
		t.Fatalf("GroupCursor() = %d after one wheel notch", a.GroupCursor())
	}
	a = wheel(t, a, 2, y, true)
	if a.GroupCursor() != 0 {
		t.Fatalf("GroupCursor() = %d after scrolling back up", a.GroupCursor())
	}
}
