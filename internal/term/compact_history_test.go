package term_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

// Retention deeper than the vt working depth (issue #277): most of the
// history lives as pre-rendered strings, only the newest lines as cell
// grids. These tests read across that boundary.

// deepFlood writes n numbered lines in one call, so the write path's own
// chunking and draining are exercised, not the test loop's.
func deepFlood(e *term.Emulator, n int) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "line %04d\r\n", i)
	}
	_, _ = e.Write([]byte(b.String()))
}

// Every line of a deep history reads back in order, whichever store it
// landed in, and Text joins them all.
func TestDeepHistoryKeepsAllLines(t *testing.T) {
	e := term.New(40, 3)
	defer e.Close()
	const total = 2000
	deepFlood(e, total)

	hist := e.HistoryLen()
	if hist != total-2 { // the last two lines are still on screen
		t.Fatalf("HistoryLen() = %d, want %d", hist, total-2)
	}
	for _, i := range []int{0, 1, hist / 2, hist - 300, hist - 1} {
		want := fmt.Sprintf("line %04d", i)
		if got := ansi.Strip(e.HistoryLine(i)); got != want {
			t.Fatalf("HistoryLine(%d) = %q, want %q", i, got, want)
		}
	}

	var b strings.Builder
	for i := 0; i < total; i++ {
		fmt.Fprintf(&b, "line %04d\n", i)
	}
	want := strings.TrimSuffix(b.String(), "\n")
	if got := e.Text(); got != want {
		t.Fatalf("Text() diverges from the flood (len %d vs %d)", len(got), len(want))
	}
	if got := e.TailText(300); got != lastLines(want, 300) {
		t.Fatalf("TailText(300) = %d bytes, want the last 300 flood lines", len(got))
	}
}

func lastLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[len(lines)-n:], "\n")
}

// The cap is exact and drops the oldest lines first, even when it is larger
// than the vt working depth so the compact ring does the evicting.
func TestDeepHistoryCapDropsOldest(t *testing.T) {
	e := term.New(40, 3)
	defer e.Close()
	e.SetHistorySize(500)
	deepFlood(e, 1000)

	if !e.HistoryFull() {
		t.Fatal("HistoryFull() = false after flooding past the cap")
	}
	hist := e.HistoryLen()
	if hist != 500 {
		t.Fatalf("HistoryLen() = %d, want the exact cap 500", hist)
	}
	// 998 lines scrolled off; the newest 500 survive: 498..997.
	if got, want := ansi.Strip(e.HistoryLine(0)), "line 0498"; got != want {
		t.Fatalf("oldest retained = %q, want %q", got, want)
	}
	if got, want := ansi.Strip(e.HistoryLine(499)), "line 0997"; got != want {
		t.Fatalf("newest retained = %q, want %q", got, want)
	}
}

// Shrinking the cap on a full retention keeps the newest lines.
func TestSetHistorySizeShrinksLive(t *testing.T) {
	e := term.New(40, 3)
	defer e.Close()
	deepFlood(e, 1000)

	e.SetHistorySize(100)
	if got := e.HistoryLen(); got != 100 {
		t.Fatalf("HistoryLen() = %d after shrink, want 100", got)
	}
	if got, want := ansi.Strip(e.HistoryLine(99)), "line 0997"; got != want {
		t.Fatalf("newest retained = %q, want %q", got, want)
	}
}

// A resize must not lose or corrupt the compact history. The compact lines
// do not reflow — they are frozen at the width they scrolled off with, and
// the pane clips them at render time — but they all stay readable.
func TestDeepHistorySurvivesResize(t *testing.T) {
	e := term.New(40, 5)
	defer e.Close()
	deepFlood(e, 1000)

	e.Resize(20, 5)
	if got, want := ansi.Strip(e.HistoryLine(0)), "line 0000"; got != want {
		t.Fatalf("after shrink, oldest = %q, want %q", got, want)
	}
	e.Resize(40, 5)
	if got, want := ansi.Strip(e.HistoryLine(0)), "line 0000"; got != want {
		t.Fatalf("after grow, oldest = %q, want %q", got, want)
	}
	if got := e.Text(); !strings.Contains(got, "line 0999") {
		t.Fatalf("newest line lost across resizes: %q", lastLines(got, 3))
	}
}

// Styling survives the move into the compact store: HistoryLine returns
// styled text either way.
func TestDeepHistoryKeepsStyling(t *testing.T) {
	e := term.New(40, 3)
	defer e.Close()
	var b strings.Builder
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&b, "\x1b[31mred %04d\x1b[m\r\n", i)
	}
	_, _ = e.Write([]byte(b.String()))

	got := e.HistoryLine(0)
	if ansi.Strip(got) != "red 0000" {
		t.Fatalf("HistoryLine(0) text = %q, want %q", ansi.Strip(got), "red 0000")
	}
	if got == ansi.Strip(got) {
		t.Fatal("HistoryLine(0) lost its styling in the compact store")
	}
}
