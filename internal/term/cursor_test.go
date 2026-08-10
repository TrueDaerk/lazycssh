package term

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// feedLines writes n numbered plain-text lines starting at from.
func feedLines(t *testing.T, e *Emulator, from, n int) {
	t.Helper()
	var b strings.Builder
	for i := from; i < from+n; i++ {
		fmt.Fprintf(&b, "line-%05d\r\n", i)
	}
	if _, err := e.Write([]byte(b.String())); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

// TestHistoryCursorAnchorsLinesAcrossDrops pins the property the UI's search
// match cache is built on (issue #278): while Gen holds, Start+i is a stable
// address — the line a cursor located before more output dropped the front is
// the same line at its shifted index afterwards.
func TestHistoryCursorAnchorsLinesAcrossDrops(t *testing.T) {
	e := New(80, 24)
	e.SetHistorySize(200)
	feedLines(t, e, 0, 300)

	before := e.HistoryCursor()
	if !before.Exact {
		t.Fatalf("HistoryCursor().Exact = false at a cap above the working depth")
	}
	if before.Len != e.HistoryLen() {
		t.Fatalf("cursor Len = %d, HistoryLen = %d", before.Len, e.HistoryLen())
	}
	if before.Start == 0 {
		t.Fatal("300 lines through a 200-line cap dropped nothing")
	}
	texts := make(map[uint64]string, before.Len)
	for i := 0; i < before.Len; i++ {
		texts[before.Start+uint64(i)] = ansi.Strip(e.HistoryLine(i))
	}

	feedLines(t, e, 300, 150)
	after := e.HistoryCursor()
	if after.Gen != before.Gen {
		t.Fatalf("plain output bumped Gen from %d to %d", before.Gen, after.Gen)
	}
	if after.Start <= before.Start {
		t.Fatalf("Start did not advance: %d then %d", before.Start, after.Start)
	}
	compared := 0
	for i := 0; i < after.Len; i++ {
		want, ok := texts[after.Start+uint64(i)]
		if !ok {
			continue
		}
		if got := ansi.Strip(e.HistoryLine(i)); got != want {
			t.Fatalf("line at absolute index %d changed: %q then %q",
				after.Start+uint64(i), want, got)
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("no retained line survived into the second snapshot; the test compared nothing")
	}
}

// TestHistoryCursorGenMovesWhenLinesAreRewritten covers the two in-place
// rewrites incremental readers must restart on.
func TestHistoryCursorGenMovesWhenLinesAreRewritten(t *testing.T) {
	e := New(80, 24)
	e.SetHistorySize(500)
	feedLines(t, e, 0, 100)
	gen := e.HistoryCursor().Gen

	e.Resize(60, 24)
	resized := e.HistoryCursor().Gen
	if resized == gen {
		t.Fatal("a width resize reflows the working depth but kept Gen")
	}

	e.SetHistorySize(300)
	if e.HistoryCursor().Gen == resized {
		t.Fatal("a retention change kept Gen")
	}
}

// TestHistoryCursorInexactAtSmallCaps: at or below the compact working depth
// the vt scrollback evicts on its own, so Start cannot be vouched for.
func TestHistoryCursorInexactAtSmallCaps(t *testing.T) {
	e := New(80, 24)
	e.SetHistorySize(50)
	feedLines(t, e, 0, 200)
	if c := e.HistoryCursor(); c.Exact {
		t.Fatalf("HistoryCursor().Exact = true at cap 50, working depth %d", compactWorkingDepth)
	}
}
