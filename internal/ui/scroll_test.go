package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// scrollApp is a focused single pane with numbered scrollback lines.
func scrollApp(t *testing.T, lines int) (App, *fakeFleet) {
	t.Helper()
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= lines; i++ {
		fleet.sessions["web-01"].Emitf("line-%03d\n", i)
	}
	a = pressKey(t, a, "tab") // focus the grid
	return a, fleet
}

func ctrl(r rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl} }

// Scrolling back shows older output; G returns to the tail.
func TestScrollBackAndReturn(t *testing.T) {
	a, _ := scrollApp(t, 200)

	if view := plain(a.View().Content); !strings.Contains(view, "line-200") {
		t.Fatalf("the tail is not shown:\n%s", view)
	}

	for i := 0; i < 3; i++ {
		a = press(t, a, ctrl('u'))
	}
	view := plain(a.View().Content)
	if strings.Contains(view, "line-200") {
		t.Fatalf("scrolling back still shows the tail:\n%s", view)
	}
	if !strings.Contains(plain(a.View().Content), "scrollback +") {
		t.Fatalf("a scrolled pane does not say so in the status bar:\n%s", view)
	}

	a = pressKey(t, a, "G")
	if view := plain(a.View().Content); !strings.Contains(view, "line-200") {
		t.Fatalf("G did not return to the tail:\n%s", view)
	}
	if !a.FollowingTail("web-01") {
		t.Fatal("G did not resume following")
	}
}

// g jumps to the oldest retained output, which is where the dropped marker is.
func TestScrollToTop(t *testing.T) {
	a, _ := scrollApp(t, 200)
	a = pressKey(t, a, "g")
	if view := plain(a.View().Content); !strings.Contains(view, "line-001") {
		t.Fatalf("g did not reach the oldest line:\n%s", view)
	}
}

// The acceptance criterion: scrolling back does not stop new output from being
// buffered.
func TestScrolledPaneKeepsBuffering(t *testing.T) {
	a, fleet := scrollApp(t, 200)
	a = pressKey(t, a, "g")

	before := fleet.sessions["web-01"].Scrollback().Len()
	fleet.sessions["web-01"].Emit("fresh output after scrolling\n")
	if got := fleet.sessions["web-01"].Scrollback().Len(); got != before+1 {
		t.Fatalf("Len() = %d after new output, want %d", got, before+1)
	}
	if a.FollowingTail("web-01") {
		t.Fatal("new output yanked the pane back to the tail")
	}

	// The new line is there the moment the user returns.
	a = pressKey(t, a, "G")
	if view := plain(a.View().Content); !strings.Contains(view, "fresh output after scrolling") {
		t.Fatalf("the buffered line is missing at the tail:\n%s", view)
	}
}

// typeSearch opens the search and types a term.
func typeSearch(t *testing.T, a App, term string) App {
	t.Helper()
	a = pressKey(t, a, "/")
	if !a.Searching() {
		t.Fatal("/ did not open the search")
	}
	for _, r := range term {
		a = press(t, a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// Searching within one pane scrolls to the newest match and highlights it.
func TestSearchWithinAPane(t *testing.T) {
	a, fleet := scrollApp(t, 0)
	for i := 1; i <= 200; i++ {
		if i == 60 {
			fleet.sessions["web-01"].Emit("ERROR: broken pipe\n")
			continue
		}
		fleet.sessions["web-01"].Emitf("line-%03d\n", i)
	}

	a = typeSearch(t, a, "error")
	if a.Searching() {
		t.Fatal("enter did not close the search input")
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, "ERROR: broken pipe") {
		t.Fatalf("the search did not scroll to the match:\n%s", view)
	}
	if !strings.Contains(view, `search "error"`) {
		t.Fatalf("the active search is not named in the status bar:\n%s", view)
	}

	// The match line carries the match style.
	styled := a.View().Content
	if !strings.Contains(styled, a.theme.Match.Render("ERROR: broken pipe")) {
		t.Fatal("the matching line is not highlighted")
	}

	// esc clears the term and the highlight.
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.SearchTerm() != "" {
		t.Fatalf("esc left the term %q", a.SearchTerm())
	}
}

// [ and ] walk the matches, older and newer.
func TestSearchMatchNavigation(t *testing.T) {
	a, fleet := scrollApp(t, 0)
	for i := 1; i <= 300; i++ {
		if i%100 == 50 {
			fleet.sessions["web-01"].Emitf("ERROR at %03d\n", i)
			continue
		}
		fleet.sessions["web-01"].Emitf("line-%03d\n", i)
	}

	a = typeSearch(t, a, "ERROR")
	if view := plain(a.View().Content); !strings.Contains(view, "ERROR at 250") {
		t.Fatalf("the newest match is not on screen:\n%s", view)
	}

	a = pressKey(t, a, "[")
	if view := plain(a.View().Content); !strings.Contains(view, "ERROR at 150") {
		t.Fatalf("[ did not reach the older match:\n%s", view)
	}
	a = pressKey(t, a, "[")
	if view := plain(a.View().Content); !strings.Contains(view, "ERROR at 050") {
		t.Fatalf("[ did not reach the oldest match:\n%s", view)
	}
	// Running out of matches stays put rather than wrapping.
	a = pressKey(t, a, "[")
	if view := plain(a.View().Content); !strings.Contains(view, "ERROR at 050") {
		t.Fatalf("[ wrapped past the oldest match:\n%s", view)
	}

	a = pressKey(t, a, "]")
	if view := plain(a.View().Content); !strings.Contains(view, "ERROR at 150") {
		t.Fatalf("] did not step back to the newer match:\n%s", view)
	}
}

// The acceptance criterion: /find answers "which of my hosts printed this
// error" directly.
func TestCrossPaneFind(t *testing.T) {
	names := make([]string, 8)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a, fleet, _, _ := statusApp(t, names...)
	for _, id := range names {
		fleet.sessions[id].Emit("all quiet\n")
	}
	fleet.sessions["web-03"].Emit("ERROR: disk full\n")
	fleet.sessions["web-07"].Emit("error: disk full\n")

	a = pressKey(t, a, ":")
	for _, r := range "/find disk full" {
		a = press(t, a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})

	report := a.LastDelivery()
	if !strings.Contains(report, "2/8 hosts") ||
		!strings.Contains(report, "web-03") || !strings.Contains(report, "web-07") {
		t.Fatalf("the report does not answer which hosts matched: %q", report)
	}
	// The term is shared: every pane highlights it from now on.
	if a.SearchTerm() != "disk full" {
		t.Fatalf("SearchTerm() = %q", a.SearchTerm())
	}

	// /find with no term clears it.
	a = pressKey(t, a, ":")
	for _, r := range "/find" {
		a = press(t, a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.SearchTerm() != "" {
		t.Fatalf("/find did not clear the term: %q", a.SearchTerm())
	}
}

// A search matching nothing reports that, and /findx is not /find.
func TestFindEdgeCases(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("all quiet\n")

	a = pressKey(t, a, ":")
	for _, r := range "/find nothing-here" {
		a = press(t, a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := a.LastDelivery(); !strings.Contains(got, "matches no host") {
		t.Fatalf("an empty result reports %q", got)
	}

	if _, _, meta := a.applyFind("/findx pattern"); meta {
		t.Fatal("/findx was treated as /find")
	}
}
