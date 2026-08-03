package ui

import (
	"fmt"
	"strings"
	"testing"
)

// pagedApp builds an app whose hosts do not fit on one page.
func pagedApp(t *testing.T, hosts int, width, height int) App {
	t.Helper()

	a := resize(t, fleetApp(t, hosts), width, height)
	if a.Pages() < 2 {
		t.Fatalf("setup: %d hosts fit on one page at %dx%d", hosts, width, height)
	}
	// The paging tests drive the app level; paging while typing has its own
	// test.
	return pressKey(t, a, "ctrl+]")
}

func TestWindowShowsOnePageOfHosts(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)
	g := a.Grid()

	window := a.WindowHosts()
	if len(window) != g.PerPage {
		t.Fatalf("the window holds %d hosts, a page fits %d", len(window), g.PerPage)
	}
	if window[0] != "web-01" {
		t.Fatalf("the first page starts at %q", window[0])
	}
}

func TestPagingMovesTheWindowAndTheFocus(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)
	perPage := a.Grid().PerPage

	a = pressKey(t, a, "ctrl+shift+right")
	if a.Page() != 1 {
		t.Fatalf("Page() = %d after paging forward", a.Page())
	}
	if got := a.WindowHosts()[0]; got != a.hostIDs()[perPage] {
		t.Fatalf("the second page starts at %q", got)
	}
	// The pane that receives a keystroke is one the user can see.
	if a.FocusedHost() != a.hostIDs()[perPage] {
		t.Fatalf("FocusedHost() = %q after paging", a.FocusedHost())
	}

	a = pressKey(t, a, "ctrl+shift+left")
	if a.Page() != 0 {
		t.Fatalf("Page() = %d after paging back", a.Page())
	}
	if a.FocusedHost() != "web-01" {
		t.Fatalf("FocusedHost() = %q after paging back", a.FocusedHost())
	}
}

// The single navigator wraps (issue #147): back from the first page lands on
// the last, forward from the last lands on the first.
func TestPagingWrapsAtBothEnds(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)

	a = pressKey(t, a, "ctrl+shift+left")
	if got, want := a.Page(), a.Pages()-1; got != want {
		t.Fatalf("Page() = %d after wrapping backward, want %d", got, want)
	}
	if !contains(a.WindowHosts(), a.FocusedHost()) {
		t.Fatalf("the focused host %q is not on screen after the wrap", a.FocusedHost())
	}

	a = pressKey(t, a, "ctrl+shift+right")
	if a.Page() != 0 {
		t.Fatalf("Page() = %d after wrapping forward, want 0", a.Page())
	}
	if a.FocusedHost() != "web-01" {
		t.Fatalf("FocusedHost() = %q after wrapping forward", a.FocusedHost())
	}
}

// Moving the focus off the edge of a page turns the page rather than focusing a
// pane the user cannot see.
func TestMovingFocusTurnsThePage(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)
	perPage := a.Grid().PerPage

	for range perPage {
		a = pressKey(t, a, "alt+shift+right")
	}
	if a.Page() != 1 {
		t.Fatalf("Page() = %d after moving the focus past the page", a.Page())
	}
	if !contains(a.WindowHosts(), a.FocusedHost()) {
		t.Fatalf("the focused host %q is not on screen: %v", a.FocusedHost(), a.WindowHosts())
	}
}

// Plain ctrl+arrows page nothing any more (issue #208): they are readline word
// movement for the hosts, and IDEs and window managers swallow them anyway.
func TestPlainCtrlArrowsDoNotPage(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)

	a = pressKey(t, a, "ctrl+right")
	a = pressKey(t, a, "ctrl+left")
	if a.Page() != 0 {
		t.Fatalf("Page() = %d; plain ctrl+arrows must not page", a.Page())
	}
}

// Paging is pane management like the alt chords (issue #208): ctrl+shift+arrow
// turns the page while typing too, without leaving the pane's terminal.
func TestPagingWorksWhileTyping(t *testing.T) {
	a := resize(t, fleetApp(t, 12), 60, 14)
	if a.Pages() < 2 {
		t.Fatal("setup: the hosts fit on one page")
	}
	a = focusGrid(t, a)

	a = pressKey(t, a, "ctrl+shift+right")
	if a.Page() != 1 {
		t.Fatalf("Page() = %d after paging while typing", a.Page())
	}
	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v; paging must stay in typing", a.Focus())
	}
}

// The window is not the working set: paging changes what is on screen, never
// which hosts a command is about.
func TestPagingDoesNotChangeTheHostList(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)
	before := strings.Join(a.hostIDs(), ",")

	a = pressKey(t, a, "ctrl+shift+right")
	if got := strings.Join(a.hostIDs(), ","); got != before {
		t.Fatalf("paging changed the run's hosts:\n%s\n%s", before, got)
	}
}

func TestPageIndicatorOnlyWhenItPages(t *testing.T) {
	one := resize(t, fleetApp(t, 4), 200, 60)
	if one.Pages() != 1 {
		t.Fatalf("setup: 4 hosts need %d pages at 200x60", one.Pages())
	}
	if strings.Contains(plain(one.View().Content), "page ") {
		t.Fatalf("a single page rendered a page indicator:\n%s", plain(one.View().Content))
	}

	many := pressKey(t, pagedApp(t, 12, 60, 14), "ctrl+]")
	view := plain(many.View().Content)
	if !strings.Contains(view, "page 1/") {
		t.Fatalf("no page indicator while paging:\n%s", view)
	}
}

// The terminal can shrink under a paged window: more pages, and the page the
// user was on may no longer exist.
func TestPageClampsWhenTheTerminalChanges(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)
	for range 20 {
		a = pressKey(t, a, "ctrl+shift+right")
	}
	last := a.Page()

	a = resize(t, a, 240, 62)
	if a.Pages() != 1 {
		t.Fatalf("12 hosts still need %d pages at 240x62", a.Pages())
	}
	if a.Page() != 0 {
		t.Fatalf("Page() = %d after the window grew, want 0", a.Page())
	}
	if got := len(a.WindowHosts()); got != 12 {
		t.Fatalf("the window holds %d of 12 hosts", got)
	}

	a = resize(t, a, 60, 14)
	if a.Page() > a.Pages()-1 {
		t.Fatalf("Page() = %d with %d pages", a.Page(), a.Pages())
	}
	if a.Page() != last {
		t.Logf("page restored to %d rather than %d, which is fine as long as it exists",
			a.Page(), last)
	}
}

func TestWindowWithNoHosts(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	if got := a.WindowHosts(); got != nil {
		t.Fatalf("WindowHosts() = %v with no hosts", got)
	}
	if a.Page() != 0 || a.Pages() != 0 {
		t.Fatalf("Page()/Pages() = %d/%d with no hosts", a.Page(), a.Pages())
	}
	if got := a.windowLabel(); got != "" {
		t.Fatalf("windowLabel() = %q with no hosts", got)
	}
}

func TestPagingWithASinglePageDoesNothing(t *testing.T) {
	a := pressKey(t, resize(t, fleetApp(t, 4), 200, 60), "ctrl+]")
	before := a.View().Content

	a = pressKey(t, a, "ctrl+shift+right")
	if a.Page() != 0 || a.View().Content != before {
		t.Fatal("paging moved a window that has only one page")
	}
}

// contains reports whether the slice holds v.
func contains(all []string, v string) bool {
	for _, s := range all {
		if s == v {
			return true
		}
	}
	return false
}

// The acceptance criterion for issue #199: a run that needs more than one page
// broadcasts to the page on screen, not to the hosts behind it.
func TestBroadcastStopsAtThePage(t *testing.T) {
	names := make([]string, 10)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a, fleet, router, _ := statusApp(t, names...)
	router.Attach(fleetSessions{fleet})
	for _, id := range names {
		fleet.connect(t, id)
	}

	// A terminal too small for ten panes: the grid pages, the broadcast must
	// page with it.
	a = pressKey(t, resize(t, a, 120, 30), "ctrl+]")
	if a.Pages() < 2 {
		t.Fatalf("setup: ten hosts fit on one page (%d pages)", a.Pages())
	}

	first := a.WindowHosts()
	if len(first) >= len(names) {
		t.Fatalf("setup: the page holds every host (%d)", len(first))
	}
	if got := strings.Join(router.Targets(), ","); got != strings.Join(first, ",") {
		t.Fatalf("broadcast reaches %q, want the page %q", got, first)
	}

	a = pressKey(t, a, "ctrl+shift+right")
	second := a.WindowHosts()
	if strings.Join(second, ",") == strings.Join(first, ",") {
		t.Fatal("ctrl+shift+right did not turn the page")
	}
	if got := strings.Join(router.Targets(), ","); got != strings.Join(second, ",") {
		t.Fatalf("broadcast reaches %q after paging, want %q", got, second)
	}
	for _, id := range first {
		if contains(router.Targets(), id) && !contains(second, id) {
			t.Fatalf("broadcast still reaches %s, which left the screen", id)
		}
	}
}
