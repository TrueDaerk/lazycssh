package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// searchApp is a single pane holding numbered lines with matches every 100
// lines, focus left where the app-level commands live (the sidebar).
func searchApp(t *testing.T, matches ...int) (App, *fakeFleet) {
	t.Helper()

	a, fleet, _, _ := statusApp(t, "web-01")
	wanted := make(map[int]bool, len(matches))
	for _, m := range matches {
		wanted[m] = true
	}
	for i := 1; i <= 300; i++ {
		if wanted[i] {
			fleet.sessions["web-01"].Emitf("ERROR at %03d\n", i)
			continue
		}
		fleet.sessions["web-01"].Emitf("line-%03d\n", i)
	}
	return a, fleet
}

// typeTerm types a term into an already open search input and commits it.
func typeTerm(t *testing.T, a App, term string) App {
	t.Helper()
	for _, r := range term {
		a = press(t, a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
}

// slashSearch is the whole interaction from the UI command scope: / opens the
// input, the term is typed, enter commits it.
func slashSearch(t *testing.T, a App, term string) App {
	t.Helper()
	a = pressKey(t, a, "/")
	if !a.Searching() {
		t.Fatal("/ did not open the search input")
	}
	return typeTerm(t, a, term)
}

// The acceptance criterion for the routing: / opens the search where plain
// letters are commands, and enter lands on the newest match.
func TestSlashOpensTheSearchFromTheCommandScope(t *testing.T) {
	a, _ := searchApp(t, 50, 150, 250)

	a = slashSearch(t, a, "error")
	if a.Searching() {
		t.Fatal("enter did not close the search input")
	}
	if got := a.SearchTerm(); got != "error" {
		t.Fatalf("SearchTerm() = %q", got)
	}
	if view := plain(a.View().Content); !strings.Contains(view, "ERROR at 250") {
		t.Fatalf("enter did not jump to the newest match:\n%s", view)
	}
}

// The other half of the routing: a focused pane is a terminal, so / is a
// character for the host and never opens the search.
func TestSlashReachesTheHostWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")

	a = press(t, a, tea.KeyPressMsg{Code: '/', Text: "/"})
	if a.Searching() {
		t.Fatal("/ opened the search while typing to a host")
	}
	if got := string(fleet.sessions["web-01"].Written()); got != "/" {
		t.Fatalf("the host received %q, want the literal slash", got)
	}
}

// n and N walk the matches, older and newer, and the status bar counts where
// the cursor stands.
func TestMatchNavigationWithNAndShiftN(t *testing.T) {
	a, _ := searchApp(t, 50, 150, 250)

	a = slashSearch(t, a, "ERROR")
	if got := plain(a.View().Content); !strings.Contains(got, "3/3") {
		t.Fatalf("the newest match is not counted as 3/3:\n%s", got)
	}

	a = pressKey(t, a, "n")
	view := plain(a.View().Content)
	if !strings.Contains(view, "ERROR at 150") || !strings.Contains(view, "2/3") {
		t.Fatalf("n did not reach the older match:\n%s", view)
	}

	a = pressKey(t, a, "n")
	if view := plain(a.View().Content); !strings.Contains(view, "ERROR at 050") {
		t.Fatalf("n did not reach the oldest match:\n%s", view)
	}

	a = pressKey(t, a, "N")
	view = plain(a.View().Content)
	if !strings.Contains(view, "ERROR at 150") || !strings.Contains(view, "2/3") {
		t.Fatalf("N did not step back to the newer match:\n%s", view)
	}
}

// The documented choice: the search stops at the ends rather than wrapping, in
// both directions, exactly like pane movement.
func TestMatchNavigationStopsAtBothEnds(t *testing.T) {
	a, _ := searchApp(t, 50, 250)

	a = slashSearch(t, a, "ERROR")
	// Newest first: stepping newer has nowhere to go.
	before := a.View().Content
	a = pressKey(t, a, "N")
	if a.View().Content != before {
		t.Fatal("N wrapped past the newest match")
	}
	if got := plain(a.View().Content); !strings.Contains(got, "2/2") {
		t.Fatalf("the cursor left the newest match:\n%s", got)
	}

	a = pressKey(t, a, "n")
	if got := plain(a.View().Content); !strings.Contains(got, "1/2") {
		t.Fatalf("n did not reach the oldest match:\n%s", got)
	}
	oldest := a.View().Content
	a = pressKey(t, a, "n")
	if a.View().Content != oldest {
		t.Fatal("n wrapped past the oldest match")
	}
}

// A term nothing matches says so and moves nothing.
func TestSearchWithoutMatchesSaysSoAndDoesNotMove(t *testing.T) {
	a, _ := searchApp(t, 50)

	before := plain(a.View().Content)
	offset := a.scrollOffset("web-01")

	a = slashSearch(t, a, "no-such-line")
	view := plain(a.View().Content)
	if !strings.Contains(view, "no match") {
		t.Fatalf("a search with no matches does not say so:\n%s", view)
	}
	if got := a.scrollOffset("web-01"); got != offset {
		t.Fatalf("the viewport moved: offset %d, want %d", got, offset)
	}
	if position, total := a.MatchPosition(); position != 0 || total != 0 {
		t.Fatalf("MatchPosition() = %d/%d, want 0/0", position, total)
	}
	// The pane still shows what it showed, bar the status line's new word.
	if !strings.Contains(before, "line-300") || !strings.Contains(view, "line-300") {
		t.Fatalf("the pane no longer shows its tail:\n%s", view)
	}

	// Stepping from nothing is not a jump either.
	if a = pressKey(t, a, "n"); a.scrollOffset("web-01") != offset {
		t.Fatal("n moved the viewport with no matches to move to")
	}
}

// esc leaves the search: the highlight goes, the window goes back where it was,
// and the plain letters mean what they meant before again.
func TestEscapeLeavesTheSearchAndRestoresTheScroll(t *testing.T) {
	a, _ := searchApp(t, 50)

	// Somewhere other than the tail, so a restored position is distinguishable
	// from a reset one.
	a = pressKey(t, a, "shift+pgup")
	before := a.scrollOffset("web-01")
	if before == 0 {
		t.Fatal("setup: the pane is still following the tail")
	}

	a = slashSearch(t, a, "ERROR")
	if a.scrollOffset("web-01") == before {
		t.Fatal("the search did not move the window")
	}
	if a.CurrentMatch() < 0 {
		t.Fatal("the search did not land on a match")
	}

	a = pressKey(t, a, "esc")
	if a.SearchTerm() != "" {
		t.Fatalf("esc left the term %q", a.SearchTerm())
	}
	if a.CurrentMatch() >= 0 {
		t.Fatal("esc left the match cursor behind")
	}
	if got := a.scrollOffset("web-01"); got != before {
		t.Fatalf("esc left the window at %d, want the pre-search %d", got, before)
	}
	if view := plain(a.View().Content); strings.Contains(view, "search ") {
		t.Fatalf("the status bar still names a search:\n%s", view)
	}

	// The shadow is over: n is "connect a new host" again.
	if a = pressKey(t, a, "n"); !a.ConnectPromptOpen() {
		t.Fatal("n did not open the new-host prompt after the search ended")
	}
}

// The current match is louder than the others, so "3/17" is visible on the
// screen and not only in the status bar.
func TestCurrentMatchIsHighlightedDifferently(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("ERROR one\n")
	fleet.sessions["web-01"].Emit("quiet\n")
	fleet.sessions["web-01"].Emit("ERROR two\n")

	a = slashSearch(t, a, "ERROR")
	styled := a.View().Content
	if !strings.Contains(styled, a.theme.MatchCurrent.Render("ERROR two")) {
		t.Fatal("the current match does not carry the current-match style")
	}
	if !strings.Contains(styled, a.theme.Match.Render("ERROR one")) {
		t.Fatal("the other match does not carry the plain match style")
	}

	styled = pressKey(t, a, "n").View().Content
	if !strings.Contains(styled, a.theme.MatchCurrent.Render("ERROR one")) {
		t.Fatal("n did not move the current-match highlight")
	}
}

// While no search is live the letters belong to the app: n connects a host, and
// esc is not swallowed by a search that is not there.
func TestSearchKeysAreInertWithoutATerm(t *testing.T) {
	a, _ := searchApp(t)

	if next := pressKey(t, a, "n"); !next.ConnectPromptOpen() {
		t.Fatal("n did not open the new-host prompt while no search was live")
	}
	if next := pressKey(t, a, "N"); next.SearchTerm() != "" || next.Searching() {
		t.Fatal("N started a search of its own")
	}
}
