package ui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// fleetApp builds an app over n hosts, sized and with the grid focused.
func fleetApp(t *testing.T, n int) App {
	t.Helper()

	hosts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		hosts = append(hosts, fmt.Sprintf("web-%02d", i))
	}
	a := resize(t, NewApp(Config{Hosts: hosts, Theme: Options{Dark: true}}), 120, 40)
	return focusGrid(t, a)
}

func TestArrowsMoveWithinTheGrid(t *testing.T) {
	a := fleetApp(t, 4)
	if a.Focus() != AreaGrid {
		t.Fatal("setup: the grid does not have focus")
	}
	if a.FocusedHost() != "web-01" {
		t.Fatalf("FocusedHost() = %q", a.FocusedHost())
	}

	a = pressKey(t, a, "alt+right")
	if a.FocusedHost() != "web-02" {
		t.Fatalf("FocusedHost() = %q after moving right", a.FocusedHost())
	}
	// Four hosts tile as a 2x2, so down is a row - two hosts - not one.
	a = pressKey(t, a, "alt+down")
	if a.FocusedHost() != "web-04" {
		t.Fatalf("FocusedHost() = %q after moving down", a.FocusedHost())
	}
	a = pressKey(t, a, "alt+left")
	if a.FocusedHost() != "web-03" {
		t.Fatalf("FocusedHost() = %q after moving left", a.FocusedHost())
	}
	a = pressKey(t, a, "alt+up")
	if a.FocusedHost() != "web-01" {
		t.Fatalf("FocusedHost() = %q after moving up", a.FocusedHost())
	}
}

// Stepping off the end onto the other end is how a user types into the machine
// at the far side of the fleet.
func TestPaneFocusDoesNotWrap(t *testing.T) {
	a := fleetApp(t, 3)

	a = pressKey(t, a, "alt+left")
	if a.PaneIndex() != 0 {
		t.Fatalf("PaneIndex() = %d after moving left from the first pane", a.PaneIndex())
	}

	for range 5 {
		a = pressKey(t, a, "alt+right")
	}
	if a.PaneIndex() != 2 {
		t.Fatalf("PaneIndex() = %d after running off the end", a.PaneIndex())
	}
}

func TestArrowsMoveWithinTheSidebar(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	if a.Focus() != AreaSidebar {
		t.Fatal("setup: the sidebar does not have focus")
	}

	a = pressKey(t, a, "j")
	if a.Panel() != PanelHosts {
		t.Fatalf("Panel() = %v after moving down", a.Panel())
	}
	a = pressKey(t, a, "k")
	if a.Panel() != PanelStatus {
		t.Fatalf("Panel() = %v after moving up", a.Panel())
	}
	a = pressKey(t, a, "k")
	if a.Panel() != PanelStatus {
		t.Fatal("the sidebar selection moved above the first panel")
	}

	for range 10 {
		a = pressKey(t, a, "j")
	}
	if a.Panel() != PanelCommandLog {
		t.Fatalf("Panel() = %v after running off the end", a.Panel())
	}
}

// A plain key means one thing at a time: while a pane is focused it is a
// keystroke for the host, and only the alt chords move the pane focus; at the
// app level the same letters drive the panel lists.
func TestTheSameKeyMeansOneThingAtATime(t *testing.T) {
	a := fleetApp(t, 4)

	panelBefore := a.Panel()
	a = pressKey(t, a, "alt+down")
	if a.Panel() != panelBefore {
		t.Fatal("a pane-management chord also moved the sidebar")
	}
	if a.PaneIndex() != a.Grid().Columns {
		t.Fatalf("PaneIndex() = %d, want one row down", a.PaneIndex())
	}

	a = pressKey(t, a, "ctrl+]") // back to the app level
	paneBefore := a.PaneIndex()
	a = pressKey(t, a, "j")
	if a.PaneIndex() != paneBefore {
		t.Fatal("a key handled by the sidebar also moved the pane focus")
	}
	if a.Panel() != PanelHosts {
		t.Fatalf("Panel() = %v", a.Panel())
	}
}

func TestEnterFromTheSidebarFocusesTheGrid(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if next.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v after enter", next.Focus())
	}
}

// The acceptance criterion: focus survives a change to the host list. The user
// stays on the machine they were looking at, not on the position it occupied.
func TestFocusSurvivesAChangedHostList(t *testing.T) {
	a := fleetApp(t, 4)
	a = pressKey(t, a, "alt+right")
	a = pressKey(t, a, "alt+right")
	if a.FocusedHost() != "web-03" {
		t.Fatalf("setup: FocusedHost() = %q", a.FocusedHost())
	}

	// A host earlier in the list goes away: the same machine keeps focus at its
	// new position.
	model, _ := a.Update(HostsChangedMsg{Hosts: []string{"web-01", "web-03", "web-04"}})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.FocusedHost() != "web-03" {
		t.Fatalf("FocusedHost() = %q after the list changed", a.FocusedHost())
	}
	if a.PaneIndex() != 1 {
		t.Fatalf("PaneIndex() = %d", a.PaneIndex())
	}
	if a.Focus() != AreaGrid {
		t.Fatal("the area with focus changed when the host list did")
	}
}

func TestFocusClampsWhenTheFocusedHostIsGone(t *testing.T) {
	a := fleetApp(t, 4)
	for range 3 {
		a = pressKey(t, a, "alt+right")
	}
	if a.FocusedHost() != "web-04" {
		t.Fatalf("setup: FocusedHost() = %q", a.FocusedHost())
	}

	model, _ := a.Update(HostsChangedMsg{Hosts: []string{"web-01", "web-02"}})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.FocusedHost() != "web-02" {
		t.Fatalf("FocusedHost() = %q after its host went away", a.FocusedHost())
	}
}

func TestFocusWithNoHosts(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "tab")

	if a.FocusedHost() != "" {
		t.Fatalf("FocusedHost() = %q with no hosts", a.FocusedHost())
	}
	a = pressKey(t, a, "alt+right")
	if a.PaneIndex() != 0 {
		t.Fatalf("PaneIndex() = %d with no hosts", a.PaneIndex())
	}

	model, _ := a.Update(HostsChangedMsg{Hosts: nil})
	if next, ok := model.(App); !ok || next.FocusedHost() != "" {
		t.Fatal("an empty host list did not survive")
	}
}

func TestFocusedPaneIsVisible(t *testing.T) {
	a := fleetApp(t, 3)
	first := a.View().Content
	a = pressKey(t, a, "alt+right")
	if a.View().Content == first {
		t.Fatal("moving the pane focus changed nothing on screen")
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		v, lo, hi, want int
	}{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{11, 0, 10, 10},
		{3, 0, -1, 0}, // an empty range, which is an empty host list
	}
	for _, tc := range tests {
		if got := clamp(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Fatalf("clamp(%d, %d, %d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}
