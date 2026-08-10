package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// referenceMatchLines is the pre-cache implementation (issue #278): the full
// walk over the virtual line space. The cache must agree with it exactly, in
// every state a run can reach.
func referenceMatchLines(a App, id string) []int {
	if a.searchTerm == "" {
		return nil
	}
	var out []int
	for i, line := range a.virtualLines(id) {
		if containsFold(ansi.Strip(line), a.searchTerm) {
			out = append(out, i)
		}
	}
	return out
}

// requireSameMatches compares the cached matchLines against the reference.
func requireSameMatches(t *testing.T, a App, id string) []int {
	t.Helper()
	got := a.matchLines(id)
	want := referenceMatchLines(a, id)
	if len(got) != len(want) {
		t.Fatalf("matchLines = %v, reference walk = %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("matchLines = %v, reference walk = %v", got, want)
		}
	}
	return got
}

// cappedSearchApp is one pane whose terminal retains cap lines, loaded with
// n numbered lines carrying ERROR every stride, and a committed search term.
func cappedSearchApp(t *testing.T, cap, n int) (App, *fakeFleet) {
	t.Helper()
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Terminal().SetHistorySize(cap)
	emitNumbered(t, fleet, 1, n)
	return slashSearch(t, a, "error"), fleet
}

// emitNumbered emits n lines starting at from, an ERROR line every 25th.
func emitNumbered(t *testing.T, fleet *fakeFleet, from, n int) {
	t.Helper()
	for i := from; i < from+n; i++ {
		if i%25 == 0 {
			fleet.sessions["web-01"].Emitf("ERROR at %05d\n", i)
			continue
		}
		fleet.sessions["web-01"].Emitf("line-%05d\n", i)
	}
}

// The cache's whole reason to exist is being reused across output arriving,
// including past the retention cap where every new line shifts all indices.
func TestCachedMatchesTrackOutputAcrossTheCap(t *testing.T) {
	a, fleet := cappedSearchApp(t, 200, 150)

	if got := requireSameMatches(t, a, "web-01"); len(got) == 0 {
		t.Fatal("no matches before the cap; the test would compare nothing")
	}

	// Push the history past the cap: from here on the front drops.
	emitNumbered(t, fleet, 151, 200)
	if !fleet.sessions["web-01"].Terminal().HistoryFull() {
		t.Fatal("350 lines through a 200-line cap did not fill the retention")
	}
	requireSameMatches(t, a, "web-01")

	// And keep dropping: the cached front must keep shifting, not go stale.
	emitNumbered(t, fleet, 351, 175)
	if got := requireSameMatches(t, a, "web-01"); len(got) == 0 {
		t.Fatal("no matches after the drops; the test compared nothing")
	}
}

// The dropped-output marker is a virtual line like any other: a term its text
// contains must keep matching at index 0 (unchanged semantics, issue #278).
func TestCachedMatchesIncludeTheDroppedMarker(t *testing.T) {
	a, _ := cappedSearchApp(t, 200, 400)
	a.searchTerm = "dropped"
	got := requireSameMatches(t, a, "web-01")
	if len(got) == 0 || got[0] != 0 {
		t.Fatalf("matchLines = %v, want the marker line at index 0", got)
	}
}

// A new term must not be answered from the old term's hits.
func TestCachedMatchesFollowATermChange(t *testing.T) {
	a, _ := cappedSearchApp(t, 200, 300)
	requireSameMatches(t, a, "web-01")

	a = a.exitSearch()
	a = slashSearch(t, a, "line-002")
	if got := requireSameMatches(t, a, "web-01"); len(got) == 0 {
		t.Fatal("the second term matched nothing; the test compared nothing")
	}
}

// A resize reflows retained lines in place; the cache must notice through the
// emulator's history generation and rescan rather than serve stale indices.
func TestCachedMatchesSurviveAResize(t *testing.T) {
	a, fleet := cappedSearchApp(t, 200, 300)
	requireSameMatches(t, a, "web-01")

	fleet.sessions["web-01"].Terminal().Resize(47, 24)
	requireSameMatches(t, a, "web-01")
}

// At caps within the compact working depth the emulator cannot count drops;
// the fallback is an honest rescan of a retention that small.
func TestCachedMatchesAtATinyCap(t *testing.T) {
	a, fleet := cappedSearchApp(t, 50, 300)
	requireSameMatches(t, a, "web-01")
	emitNumbered(t, fleet, 301, 100)
	if got := requireSameMatches(t, a, "web-01"); len(got) == 0 {
		t.Fatal("no matches at the tiny cap; the test compared nothing")
	}
}
