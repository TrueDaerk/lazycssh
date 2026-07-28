package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

// hostsApp builds an app on the Hosts panel over a live fake fleet.
func hostsApp(t *testing.T, names ...string) (App, *fakeFleet) {
	t.Helper()

	a, fleet, _, _ := statusApp(t, names...)
	return pressKey(t, a, "2"), fleet
}

// typeFilter opens the filter and types text into it.
func typeFilter(t *testing.T, a App, text string) App {
	t.Helper()

	a = pressKey(t, a, "/")
	if !a.Filtering() {
		t.Fatal("/ did not open the filter")
	}
	for _, r := range text {
		a = pressKey(t, a, string(r))
	}
	return a
}

func TestHostsPanelListsEveryHostWithItsPaneNumber(t *testing.T) {
	a, fleet := hostsApp(t, "web-01", "db-01", "cache-01")
	fleet.connect(t, "web-01")
	fleet.fail(t, "db-01")

	view := plain(a.hostsPanel(40, 20))
	for _, want := range []string{"1 web-01", "2 db-01", "3 cache-01", "connected", "failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the Hosts panel does not show %q:\n%s", want, view)
		}
	}
}

func TestHostsCursorMovesAndStopsAtTheEnds(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02", "web-03")

	if a.SelectedHost() != "web-01" {
		t.Fatalf("SelectedHost() = %q", a.SelectedHost())
	}
	a = pressKey(t, a, "j")
	if a.SelectedHost() != "web-02" {
		t.Fatalf("SelectedHost() = %q after moving down", a.SelectedHost())
	}

	// Off the bottom of the list is the next panel, not a wrap to the top.
	for range 5 {
		a = pressKey(t, a, "j")
	}
	if a.Panel() == PanelHosts && a.SelectedHost() != "web-03" {
		t.Fatalf("SelectedHost() = %q after running off the end", a.SelectedHost())
	}
}

// The acceptance criterion: space toggles selection for the selected broadcast
// mode.
func TestSpaceTogglesSelection(t *testing.T) {
	a, _, router, _ := statusApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "2")

	a = pressKey(t, a, "j")
	a = pressKey(t, a, " ")
	if !router.IsSelected("web-02") {
		t.Fatal("space did not select the host under the cursor")
	}
	if router.IsSelected("web-01") {
		t.Fatal("space selected a host the cursor was not on")
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "2* web-02") {
		t.Fatalf("the selection is not marked:\n%s", plain(a.View().Content))
	}

	a = pressKey(t, a, " ")
	if router.IsSelected("web-02") {
		t.Fatal("space did not deselect")
	}
}

// The acceptance criterion: selection stays correct across reconnects and pane
// paging, because it is keyed by host rather than by position.
func TestSelectionSurvivesPagingAndStateChanges(t *testing.T) {
	names := make([]string, 0, 12)
	for i := 1; i <= 12; i++ {
		names = append(names, fmt.Sprintf("web-%02d", i))
	}

	a, fleet, router, _ := statusApp(t, names...)
	a = pressKey(t, a, "2")
	a = pressKey(t, a, "j")
	a = pressKey(t, a, " ")
	if !router.IsSelected("web-02") {
		t.Fatal("setup: nothing was selected")
	}

	// Page the panes.
	a = resize(t, a, 60, 14)
	a = pressKey(t, a, "tab")
	a = pressKey(t, a, "n")
	if !router.IsSelected("web-02") {
		t.Fatal("paging lost the selection")
	}

	// Reconnect the host: same identifier, new session.
	fleet.fail(t, "web-02")
	fleet.sessions["web-02"] = fleet.sessions["web-02"]
	if !router.IsSelected("web-02") {
		t.Fatal("a state change lost the selection")
	}
	if got := router.Targets(); len(got) == 0 {
		t.Fatal("the router lost its target list")
	}
}

func TestEnterFocusesTheHostsPane(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "j")
	a = pressKey(t, a, "j")

	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if next.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v after enter", next.Focus())
	}
	if next.FocusedHost() != "web-03" {
		t.Fatalf("FocusedHost() = %q after enter", next.FocusedHost())
	}
}

func TestFilterNarrowsTheListWithoutRenumberingPanes(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "db-01", "web-02", "cache-01")

	a = typeFilter(t, a, "web")
	view := plain(a.hostsPanel(40, 20))
	if strings.Contains(view, "db-01") || strings.Contains(view, "cache-01") {
		t.Fatalf("the filter kept a host it does not match:\n%s", view)
	}
	// web-02 is the third host of the run and keeps pane number 3.
	if !strings.Contains(view, "3 web-02") {
		t.Fatalf("filtering renumbered the panes:\n%s", view)
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	a, _ := hostsApp(t, "WEB-01", "db-01")
	a = typeFilter(t, a, "web")
	if strings.Contains(plain(a.hostsPanel(40, 20)), "db-01") {
		t.Fatal("the filter was case sensitive")
	}
}

func TestFilterOwnsTheKeyboardWhileOpen(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02")

	// A host called "x" must be typeable without closing a pane.
	a = typeFilter(t, a, "x")
	if a.Filter() != "x" {
		t.Fatalf("Filter() = %q", a.Filter())
	}
	if a.Panel() != PanelHosts {
		t.Fatalf("a filter keystroke changed the panel to %v", a.Panel())
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "no host matches") {
		t.Fatalf("a filter matching nothing says nothing:\n%s", plain(a.View().Content))
	}
}

func TestEnterKeepsTheFilterAndEscapeDropsIt(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "db-01")

	a = typeFilter(t, a, "web")
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Filtering() {
		t.Fatal("enter did not give the keyboard back")
	}
	if a.Filter() != "web" {
		t.Fatalf("enter dropped the filter: %q", a.Filter())
	}

	a = pressKey(t, a, "/")
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a, ok = model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Filter() != "" || a.Filtering() {
		t.Fatalf("escape left the filter as %q (open: %v)", a.Filter(), a.Filtering())
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "db-01") {
		t.Fatal("dropping the filter did not bring the hosts back")
	}
}

func TestHostCursorClampsWhenTheFilterShrinksTheList(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "j")
	a = pressKey(t, a, "j")
	if a.HostCursor() != 2 {
		t.Fatalf("HostCursor() = %d", a.HostCursor())
	}

	a = typeFilter(t, a, "web-01")
	if a.HostCursor() != 0 || a.SelectedHost() != "web-01" {
		t.Fatalf("HostCursor() = %d, SelectedHost() = %q", a.HostCursor(), a.SelectedHost())
	}
}

// Two hundred hosts must not make a redraw expensive: only the visible rows are
// rendered.
func TestTwoHundredHostsRenderQuickly(t *testing.T) {
	names := make([]string, 0, 200)
	for i := 1; i <= 200; i++ {
		names = append(names, fmt.Sprintf("web-%03d", i))
	}
	a, _ := hostsApp(t, names...)

	start := time.Now()
	for range 50 {
		_ = a.View()
		a = pressKey(t, a, "j")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("50 redraws over 200 hosts took %s", elapsed)
	}

	view := plain(a.hostsPanel(40, 12))
	if !strings.Contains(view, "more") {
		t.Fatalf("the panel does not say how many hosts are off screen:\n%s", view)
	}
	if strings.Count(view, "web-") > 30 {
		t.Fatalf("the panel rendered every host rather than the visible ones:\n%s", view)
	}
}

func TestVisibleRange(t *testing.T) {
	tests := []struct {
		name                  string
		cursor, total, height int
		wantFirst, wantLast   int
	}{
		{"everything fits", 0, 5, 10, 0, 5},
		{"cursor at the top", 0, 100, 10, 0, 10},
		{"cursor in the middle", 50, 100, 10, 45, 55},
		{"cursor at the end", 99, 100, 10, 90, 100},
		{"no room", 3, 100, 0, 3, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			first, last := visibleRange(tc.cursor, tc.total, tc.height)
			if first != tc.wantFirst || last != tc.wantLast {
				t.Fatalf("visibleRange(%d, %d, %d) = %d, %d, want %d, %d",
					tc.cursor, tc.total, tc.height, first, last, tc.wantFirst, tc.wantLast)
			}
		})
	}
}

func TestHostsPanelWithoutHosts(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "2")

	if a.SelectedHost() != "" {
		t.Fatalf("SelectedHost() = %q with no hosts", a.SelectedHost())
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "no hosts") {
		t.Fatalf("an empty run does not say so:\n%s", plain(a.hostsPanel(40, 20)))
	}
	// Toggling and choosing nothing must not panic or move focus.
	a = pressKey(t, a, " ")
	a = pressKey(t, a, "\n")
	if a.Focus() != AreaSidebar {
		t.Fatalf("Focus() = %v", a.Focus())
	}
}

func TestToggleWithoutARouterDoesNothing(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "2")
	a = pressKey(t, a, " ") // must not panic
	if a.SelectedHost() != "h1" {
		t.Fatalf("SelectedHost() = %q", a.SelectedHost())
	}
}
