package ui

import (
	"strings"
	"testing"
)

// shrinkFleet removes hosts from the fake fleet and delivers the change.
func shrinkFleet(t *testing.T, a App, fleet *fakeFleet, keep ...string) App {
	t.Helper()
	kept := make(map[string]bool, len(keep))
	for _, id := range keep {
		kept[id] = true
	}
	var ids []string
	for _, id := range fleet.ids {
		if kept[id] {
			ids = append(ids, id)
			continue
		}
		delete(fleet.sessions, id)
	}
	fleet.ids = ids
	model, _ := a.Update(HostsChangedMsg{Hosts: fleet.IDs()})
	return model.(App)
}

// The acceptance criterion: a host leaving does not move the remaining panes.
func TestGridKeepsItsShapeWhenAHostLeaves(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03", "web-04", "web-05", "web-06")
	before := a.Grid()
	if before.Columns < 2 || before.Rows < 2 {
		t.Fatalf("setup: 6 hosts tile as %dx%d", before.Columns, before.Rows)
	}

	a = shrinkFleet(t, a, fleet, "web-01", "web-02", "web-03", "web-04", "web-05")
	after := a.Grid()
	if after.Columns != before.Columns || after.Rows != before.Rows || after.PerPage != before.PerPage {
		t.Fatalf("the grid reflowed on its own: %dx%d (%d) -> %dx%d (%d)",
			before.Columns, before.Rows, before.PerPage,
			after.Columns, after.Rows, after.PerPage)
	}
	// The departed host's slot is still there - as a hole - so the positions
	// of the survivors cannot move (issue #169).
	if got := len(a.hostIDs()); got != 6 {
		t.Fatalf("%d slots visible, want 6 (5 hosts and a hole)", got)
	}
	if got := len(nonHoles(a.hostIDs())); got != 5 {
		t.Fatalf("%d hosts visible, want 5", got)
	}
}

// ctrl+r re-tiles for what is actually there, and asks for a PTY resize.
func TestCtrlRRetiles(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	if got := a.Grid().PerPage; got != 2 {
		t.Fatalf("setup: PerPage = %d", got)
	}

	a = shrinkFleet(t, a, fleet, "web-01")
	if got := a.Grid().PerPage; got != 2 {
		t.Fatalf("the shape was not kept: PerPage = %d", got)
	}

	model, cmd := a.Update(keyMsgFor(t, "ctrl+r"))
	a = model.(App)
	if got := a.Grid().PerPage; got != 1 {
		t.Fatalf("ctrl+r did not re-tile: PerPage = %d", got)
	}
	if cmd == nil {
		t.Fatal("ctrl+r produced no command; the PTYs would keep the old size")
	}
	if _, ok := cmd().(GridChangedMsg); !ok {
		t.Fatalf("ctrl+r produced a %T", cmd())
	}
}

// Joining hosts grow the grid immediately - a new pane has to appear
// somewhere, ctrl+r must never be required to see it.
func TestJoiningHostsGrowTheGridImmediately(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")

	fleet.ids = append(fleet.ids, "web-02")
	fleet.sessions["web-02"] = fleet.sessions["web-01"]
	model, _ := a.Update(HostsChangedMsg{Hosts: fleet.IDs()})
	a = model.(App)

	if got := a.Grid().PerPage; got != 2 {
		t.Fatalf("PerPage = %d after a host joined, want 2", got)
	}

	// And the raised shape is what a later departure keeps.
	a = shrinkFleet(t, a, fleet, "web-01")
	if got := a.Grid().PerPage; got != 2 {
		t.Fatalf("PerPage = %d after the join was followed by a departure, want the kept 2", got)
	}
}

// The freed cell renders as an empty frame where the host was; the survivors
// stay put.
func TestTheFreedCellRendersEmpty(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	a = shrinkFleet(t, a, fleet, "web-01")

	view := plain(a.View().Content)
	if !strings.Contains(view, "web-01") {
		t.Fatalf("the surviving pane vanished:\n%s", view)
	}
	if strings.Contains(view, "web-02") {
		t.Fatalf("the removed host is still drawn:\n%s", view)
	}
}

// An explicit view change tiles for the new view: switching sessions must
// not inherit a shape kept from the old one.
func TestSessionSwitchResetsTheKeptShape(t *testing.T) {
	a, fleet := openTwo(t)
	_ = fleet

	// back (1 host) is in the foreground; keep a bigger shape alive by
	// switching to front (2 hosts) and back again.
	a = pressKey(t, a, "j")
	a = pressKey(t, a, "enter") // front: 2 hosts
	if got := a.Grid().PerPage; got != 2 {
		t.Fatalf("front tiles %d panes, want 2", got)
	}
	a = pressKey(t, a, "j")
	a = pressKey(t, a, "enter") // back: 1 host
	if got := a.Grid().PerPage; got != 1 {
		t.Fatalf("switching back kept the old shape: PerPage = %d", got)
	}
}

// While typing, ctrl+r is readline reverse-search: it belongs to the host.
func TestCtrlRIsForwardedWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	a = pressKey(t, a, "ctrl+r")

	if got := fleet.sessions["web-01"].Written(); got != "\x12" {
		t.Fatalf("the host received %q, want the raw ctrl+r byte", got)
	}
}

// The acceptance criterion of issue #169: a host leaving the middle of the
// grid leaves a hole in its slot; every other pane keeps its position.
func TestClosedHostLeavesAHole(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03", "web-04")

	a = shrinkFleet(t, a, fleet, "web-01", "web-03", "web-04")
	want := []string{"web-01", "", "web-03", "web-04"}
	got := a.hostIDs()
	if len(got) != len(want) {
		t.Fatalf("hostIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hostIDs() = %v, want %v", got, want)
		}
	}
}

// ctrl+r closes the holes: the survivors move together and the grid tiles for
// what is actually there.
func TestCtrlRCompactsHoles(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03", "web-04")

	a = shrinkFleet(t, a, fleet, "web-01", "web-03", "web-04")
	model, _ := a.Update(keyMsgFor(t, "ctrl+r"))
	a = model.(App)

	want := []string{"web-01", "web-03", "web-04"}
	got := a.hostIDs()
	if len(got) != len(want) {
		t.Fatalf("hostIDs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hostIDs() = %v, want %v", got, want)
		}
	}
}

// A hole is a grid position, not a host: pane movement steps over it and the
// focus can never rest on it.
func TestPaneFocusSkipsHoles(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "enter") // sidebar -> grid, pane 0

	a = shrinkFleet(t, a, fleet, "web-01", "web-03")
	if a.FocusedHost() != "web-01" {
		t.Fatalf("setup: FocusedHost() = %q", a.FocusedHost())
	}

	a = pressKey(t, a, "alt+right")
	if a.FocusedHost() != "web-03" {
		t.Fatalf("FocusedHost() = %q, want web-03 (the hole skipped)", a.FocusedHost())
	}
	if a.PaneIndex() != 2 {
		t.Fatalf("PaneIndex() = %d, want 2", a.PaneIndex())
	}

	a = pressKey(t, a, "alt+left")
	if a.FocusedHost() != "web-01" {
		t.Fatalf("FocusedHost() = %q, want web-01 (the hole skipped going back)", a.FocusedHost())
	}
}

// Closing the focused host moves the focus to the nearest real pane, never
// onto the hole it left.
func TestFocusLeavesTheDepartedHostsHole(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "enter")
	a = pressKey(t, a, "alt+right") // focus web-02
	if a.FocusedHost() != "web-02" {
		t.Fatalf("setup: FocusedHost() = %q", a.FocusedHost())
	}

	a = shrinkFleet(t, a, fleet, "web-01", "web-03")
	if got := a.FocusedHost(); got == "" || got == "web-02" {
		t.Fatalf("FocusedHost() = %q after closing the focused host", got)
	}
}

// Foregrounding a session is an explicit view change: its holes close up.
func TestSessionSwitchCompactsHoles(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03")
	a = shrinkFleet(t, a, fleet, "web-01", "web-03")
	if len(a.hostIDs()) != 3 {
		t.Fatalf("setup: hostIDs() = %v", a.hostIDs())
	}

	model, _ := a.Update(SessionOpenedMsg{Name: "prod-web", Hosts: nil})
	a = model.(App)
	want := []string{"web-01", "web-03"}
	got := a.hostIDs()
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("hostIDs() = %v, want %v", got, want)
	}
}
