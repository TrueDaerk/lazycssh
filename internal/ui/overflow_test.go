package ui

import (
	"fmt"
	"strings"
	"testing"
)

// overflowApp is more hosts than statusApp's terminal can tile at the pane
// floor, so the grid pages.
func overflowApp(t *testing.T) App {
	t.Helper()
	names := make([]string, 30)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a, _, _, _ := statusApp(t, names...)
	return a
}

// The acceptance criterion: with panes hidden, the grid itself says how many
// and which key reaches them - the muted status bar counter is not enough.
func TestOverflowFooterNamesTheHiddenHosts(t *testing.T) {
	a := overflowApp(t)

	g := a.Grid()
	if g.Pages < 2 {
		t.Fatalf("setup: 30 hosts fit on %d page(s)", g.Pages)
	}
	hidden := 30 - len(a.WindowHosts())

	view := plain(a.View().Content)
	want := fmt.Sprintf("+%d more hosts — ctrl+→ · page 1/%d", hidden, g.Pages)
	if !strings.Contains(view, want) {
		t.Fatalf("the grid does not carry %q:\n%s", want, view)
	}
}

// Everything fits: no footer, no lost grid row.
func TestNoFooterWhenEverythingIsVisible(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01", "web-02")

	if a.overflowFooterVisible() {
		t.Fatal("two visible hosts, one session: nothing is hidden")
	}
	if got, want := a.gridArea(), a.Layout().Main; got != want {
		t.Fatalf("gridArea() = %+v, want the whole main area %+v", got, want)
	}
}

// The footer costs the grid one row, and the whole frame still fits the
// terminal exactly.
func TestFooterTakesOneRowOfTheGrid(t *testing.T) {
	a := overflowApp(t)

	if got, want := a.gridArea().Height, a.Layout().Main.Height-1; got != want {
		t.Fatalf("gridArea().Height = %d, want %d", got, want)
	}
	view := a.View().Content
	if got := strings.Count(view, "\n") + 1; got != a.Layout().Height {
		t.Fatalf("the frame is %d lines tall in a %d-line terminal", got, a.Layout().Height)
	}
}

// A split names its own navigation: the hidden chunks are reached with
// ctrl+right, not with the page keys.
func TestOverflowFooterNamesTheOtherChunks(t *testing.T) {
	a, fleet, router := splitApp(t)
	_, _ = fleet, router
	a = applySplitSize(t, a, "4")

	view := plain(a.View().Content)
	if !strings.Contains(view, "+6 hosts in other chunks — ctrl+→") {
		t.Fatalf("the grid does not name the hidden chunks:\n%s", view)
	}
}

// Another open session is announced too - a different group is a different
// kind of "more" and lives in the Sessions panel.
func TestOverflowFooterNamesTheOtherSessions(t *testing.T) {
	a, fleet := openTwo(t)
	fleet.connect(t, "db-01")

	view := plain(a.View().Content)
	if !strings.Contains(view, "2 more sessions — [3]") {
		t.Fatalf("the grid does not name the other sessions:\n%s", view)
	}
}

// Shrinking the terminal hides more panes; the indicator follows.
func TestFooterFollowsAResize(t *testing.T) {
	a := overflowApp(t)
	before := a.Grid().Pages

	a = resize(t, a, 60, 20)
	after := a.Grid().Pages
	if after <= before {
		t.Fatalf("shrinking did not raise the page count: %d -> %d", before, after)
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, fmt.Sprintf("page 1/%d", after)) {
		t.Fatalf("the indicator does not follow the resize:\n%s", view)
	}
}

// The words carry the meaning without colour.
func TestFooterSurvivesNoColor(t *testing.T) {
	names := make([]string, 30)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a := resize(t, NewApp(Config{Hosts: names, Theme: Options{NoColor: true}}), 120, 40)

	if !strings.Contains(plain(a.View().Content), "more hosts — ctrl+→") {
		t.Fatalf("the indicator vanished without colour:\n%s", plain(a.View().Content))
	}
}

// Full screen is an explicit zoom: no footer, the whole area is the pane.
func TestNoFooterInFullScreen(t *testing.T) {
	a := overflowApp(t)
	a = pressKey(t, a, "alt+z")

	if a.overflowFooterVisible() {
		t.Fatal("the footer is drawn over a full-screen pane")
	}
	if got, want := a.gridArea(), a.Layout().Main; got != want {
		t.Fatalf("gridArea() = %+v in full screen, want %+v", got, want)
	}
}
