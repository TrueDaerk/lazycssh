package ui

import "testing"

func TestComputeLayout(t *testing.T) {
	tests := []struct {
		name          string
		width, height int
		wantSidebar   bool
		wantTooSmall  bool
	}{
		{"typical terminal", 120, 40, true, false},
		{"wide terminal", 300, 60, true, false},
		{"the acceptance size", 40, 10, false, false},
		{"just wide enough for a sidebar", SidebarMinWidth + MainMinWidth, 20, true, false},
		{"one column too narrow for a sidebar", SidebarMinWidth + MainMinWidth - 1, 20, false, false},
		{"minimum size", MinWidth, MinHeight, false, false},
		{"too narrow", MinWidth - 1, 20, false, true},
		{"too short", 100, MinHeight - 1, false, true},
		{"zero", 0, 0, false, true},
		{"negative", -10, -4, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := ComputeLayout(tc.width, tc.height)

			if l.TooSmall != tc.wantTooSmall {
				t.Fatalf("TooSmall = %v, want %v", l.TooSmall, tc.wantTooSmall)
			}
			if l.TooSmall {
				return
			}
			if l.SidebarVisible() != tc.wantSidebar {
				t.Fatalf("SidebarVisible() = %v, want %v", l.SidebarVisible(), tc.wantSidebar)
			}

			if l.StatusBar.Height != StatusBarHeight {
				t.Fatalf("the status bar was dropped: %+v", l.StatusBar)
			}
			if l.StatusBar.Width != tc.width {
				t.Fatalf("StatusBar.Width = %d, want %d", l.StatusBar.Width, tc.width)
			}
			if got := l.Main.Width + l.Sidebar.Width; got != tc.width {
				t.Fatalf("sidebar and grid cover %d columns, the terminal has %d", got, tc.width)
			}
			if l.Main.Width < MainMinWidth && l.SidebarVisible() {
				t.Fatalf("the grid was squeezed to %d columns", l.Main.Width)
			}
			if got := l.Main.Height + l.Broadcast.Height + l.StatusBar.Height; got != tc.height {
				t.Fatalf("rows do not add up: %d + %d + %d != %d",
					l.Main.Height, l.Broadcast.Height, l.StatusBar.Height, tc.height)
			}
			if tc.height >= MinHeight+BroadcastBarHeight && l.Broadcast.Height != BroadcastBarHeight {
				t.Fatalf("Broadcast.Height = %d at %dx%d", l.Broadcast.Height, tc.width, tc.height)
			}
		})
	}
}

// A resize must never produce a negative rect: that is how a renderer panics.
func TestComputeLayoutNeverReturnsNegativeRects(t *testing.T) {
	for width := -5; width < 200; width++ {
		for height := -5; height < 60; height++ {
			l := ComputeLayout(width, height)
			for name, r := range map[string]Rect{
				"sidebar": l.Sidebar, "main": l.Main, "status": l.StatusBar,
			} {
				if r.Width < 0 || r.Height < 0 || r.X < 0 || r.Y < 0 {
					t.Fatalf("%dx%d: %s = %+v", width, height, name, r)
				}
			}
		}
	}
}

func TestSidebarWidthIsBounded(t *testing.T) {
	for width := SidebarMinWidth + MainMinWidth; width < 400; width++ {
		l := ComputeLayout(width, 40)
		if !l.SidebarVisible() {
			t.Fatalf("width %d: the sidebar vanished", width)
		}
		if l.Sidebar.Width > SidebarMaxWidth {
			t.Fatalf("width %d: sidebar is %d columns, max is %d",
				width, l.Sidebar.Width, SidebarMaxWidth)
		}
		if l.Sidebar.Width < min(SidebarMinWidth, width-MainMinWidth) {
			t.Fatalf("width %d: sidebar is %d columns", width, l.Sidebar.Width)
		}
	}
}

func TestMainStartsWhereTheSidebarEnds(t *testing.T) {
	l := ComputeLayout(120, 40)
	if l.Main.X != l.Sidebar.Width {
		t.Fatalf("Main.X = %d, sidebar is %d wide", l.Main.X, l.Sidebar.Width)
	}
	if l.Broadcast.Y != l.Main.Height {
		t.Fatalf("Broadcast.Y = %d, the body is %d tall", l.Broadcast.Y, l.Main.Height)
	}
	if l.StatusBar.Y != l.Main.Height+l.Broadcast.Height {
		t.Fatalf("StatusBar.Y = %d below a %d-tall body and %d-tall bar",
			l.StatusBar.Y, l.Main.Height, l.Broadcast.Height)
	}
}

func TestRectEmpty(t *testing.T) {
	tests := []struct {
		r    Rect
		want bool
	}{
		{Rect{Width: 10, Height: 2}, false},
		{Rect{Width: 0, Height: 2}, true},
		{Rect{Width: 10, Height: 0}, true},
		{Rect{Width: -1, Height: 5}, true},
	}
	for _, tc := range tests {
		if got := tc.r.Empty(); got != tc.want {
			t.Fatalf("%+v.Empty() = %v, want %v", tc.r, got, tc.want)
		}
	}
}
