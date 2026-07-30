package ui

import (
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
	return a
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

	a = pressKey(t, a, "alt+n")
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

	a = pressKey(t, a, "alt+p")
	if a.Page() != 0 {
		t.Fatalf("Page() = %d after paging back", a.Page())
	}
	if a.FocusedHost() != "web-01" {
		t.Fatalf("FocusedHost() = %q after paging back", a.FocusedHost())
	}
}

func TestPagingStopsAtBothEnds(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)

	a = pressKey(t, a, "alt+p")
	if a.Page() != 0 {
		t.Fatalf("Page() = %d after paging back from the first page", a.Page())
	}

	for range 20 {
		a = pressKey(t, a, "alt+n")
	}
	if got, want := a.Page(), a.Pages()-1; got != want {
		t.Fatalf("Page() = %d after running off the end, want %d", got, want)
	}
}

// Moving the focus off the edge of a page turns the page rather than focusing a
// pane the user cannot see.
func TestMovingFocusTurnsThePage(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)
	perPage := a.Grid().PerPage

	for range perPage {
		a = pressKey(t, a, "alt+right")
	}
	if a.Page() != 1 {
		t.Fatalf("Page() = %d after moving the focus past the page", a.Page())
	}
	if !contains(a.WindowHosts(), a.FocusedHost()) {
		t.Fatalf("the focused host %q is not on screen: %v", a.FocusedHost(), a.WindowHosts())
	}
}

// The window is not the working set: paging changes what is on screen, never
// which hosts a command is about.
func TestPagingDoesNotChangeTheHostList(t *testing.T) {
	a := pagedApp(t, 12, 60, 14)
	before := strings.Join(a.hostIDs(), ",")

	a = pressKey(t, a, "alt+n")
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
		a = pressKey(t, a, "alt+n")
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
	a := resize(t, fleetApp(t, 4), 200, 60)
	before := a.View().Content

	a = pressKey(t, a, "alt+n")
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
