package term_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

// Lines that scroll off the top land in the retained history, oldest first.
func TestHistoryRetainsScrolledOffLines(t *testing.T) {
	e := term.New(20, 3)
	defer e.Close()

	for i := 1; i <= 5; i++ {
		fmt.Fprintf(e, "line %d\r\n", i)
	}

	if got := e.HistoryLen(); got != 3 {
		t.Fatalf("HistoryLen() = %d, want 3", got)
	}
	for i := 0; i < 3; i++ {
		want := fmt.Sprintf("line %d", i+1)
		if got := ansi.Strip(e.HistoryLine(i)); got != want {
			t.Fatalf("HistoryLine(%d) = %q, want %q", i, got, want)
		}
	}
	if got := e.HistoryLine(99); got != "" {
		t.Fatalf("HistoryLine(out of range) = %q, want empty", got)
	}
}

// Text is history plus screen, plain, trailing blank rows trimmed.
func TestTextJoinsHistoryAndScreen(t *testing.T) {
	e := term.New(20, 3)
	defer e.Close()

	for i := 1; i <= 4; i++ {
		fmt.Fprintf(e, "line %d\r\n", i)
	}
	e.Write([]byte("$ "))

	want := "line 1\nline 2\nline 3\nline 4\n$"
	if got := e.Text(); got != want {
		t.Fatalf("Text() = %q, want %q", got, want)
	}
}

// A remote `clear` empties the screen but never the history: ED 2 pushes the
// visible rows into the retention, and the ED 3 that terminfo often appends
// is filtered out before it can wipe it.
func TestClearKeepsHistory(t *testing.T) {
	tests := []struct {
		name  string
		clear string
	}{
		{"plain 2J", "\x1b[H\x1b[2J"},
		{"2J with E3 extension", "\x1b[H\x1b[2J\x1b[3J"},
		{"3J split across writes", "\x1b[H\x1b[2J\x1b[3"}, // "J" written below
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := term.New(20, 5)
			defer e.Close()

			e.Write([]byte("history line\r\n$ "))
			e.Write([]byte(tc.clear))
			if strings.HasSuffix(tc.clear, "\x1b[3") {
				e.Write([]byte("J"))
			}

			if got := ansi.Strip(e.Render()); strings.Contains(got, "history line") {
				t.Fatalf("screen not cleared: %q", got)
			}
			if got := e.Text(); !strings.Contains(got, "history line") {
				t.Fatalf("history lost after clear: %q", got)
			}
		})
	}
}

// A partial ESC[3 that turns out to be a different sequence is released, not
// swallowed.
func TestEd3GuardReleasesFalseStart(t *testing.T) {
	e := term.New(20, 5)
	defer e.Close()

	e.Write([]byte("\x1b[3"))
	e.Write([]byte("mred\x1b[0m")) // SGR 3 (italic), then text

	if got := ansi.Strip(e.Render()); !strings.Contains(got, "red") {
		t.Fatalf("false-start bytes were swallowed: %q", got)
	}
}

// HasOutput flips on the first write and stays.
func TestHasOutput(t *testing.T) {
	e := term.New(20, 5)
	defer e.Close()

	if e.HasOutput() {
		t.Fatal("a fresh emulator claims output")
	}
	e.Write([]byte("x"))
	if !e.HasOutput() {
		t.Fatal("HasOutput() = false after a write")
	}
}

// A width shrink reflows: a line wider than the new width wraps instead of
// being truncated, and a width grow joins it back — as if the terminal had
// always been that size.
func TestResizeReflowsWidth(t *testing.T) {
	e := term.New(20, 5)
	defer e.Close()

	e.Write([]byte("0123456789abcdef\r\n$ "))

	e.Resize(10, 5)
	if got := e.Text(); !strings.Contains(got, "0123456789\nabcdef") {
		t.Fatalf("shrink did not wrap the long line: %q", got)
	}

	e.Resize(20, 5)
	if got := e.Text(); !strings.Contains(got, "0123456789abcdef") {
		t.Fatalf("grow did not rejoin the wrapped line: %q", got)
	}
}

// Repeated shrink/grow cycles are lossless: the reflow cache remembers the
// hard line breaks, so a line exactly as wide as the terminal is not joined
// with its successor.
func TestReflowRoundTripKeepsHardBreaks(t *testing.T) {
	e := term.New(10, 5)
	defer e.Close()

	// Exactly 10 cells wide, then a distinct second line: the width heuristic
	// alone would read the full row as soft-wrapped.
	e.Write([]byte("0123456789\r\nnext\r\n$ "))

	e.Resize(6, 5)
	e.Resize(10, 5)

	if got := e.Text(); !strings.Contains(got, "0123456789\nnext") {
		t.Fatalf("hard break was lost in the round trip: %q", got)
	}
}

// A height shrink scrolls the top rows into the history instead of truncating
// the bottom: the prompt survives.
func TestHeightShrinkScrollsTopIntoHistory(t *testing.T) {
	e := term.New(20, 5)
	defer e.Close()

	e.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\n$ "))

	e.Resize(20, 3)

	if got := ansi.Strip(e.Render()); !strings.Contains(got, "$") {
		t.Fatalf("the prompt did not survive the shrink: %q", got)
	}
	if got := e.Text(); !strings.Contains(got, "one") {
		t.Fatalf("the top rows were truncated, not retained: %q", got)
	}
}

// SendKey encodes per the emulator's modes: cursor keys switch encoding when
// the remote app sets application cursor keys mode.
func TestSendKeyHonoursApplicationCursorKeys(t *testing.T) {
	e := term.New(20, 5)
	defer e.Close()

	got := make(chan []byte, 8)
	e.SetReplyHandler(func(p []byte) { got <- p })

	e.SendKey(term.KeyEvent{Code: term.KeyUp})
	if key := <-got; string(key) != "\x1b[A" {
		t.Fatalf("up = %q, want CSI A", key)
	}

	e.Write([]byte("\x1b[?1h")) // application cursor keys on
	e.SendKey(term.KeyEvent{Code: term.KeyUp})
	if key := <-got; string(key) != "\x1bOA" {
		t.Fatalf("up in application mode = %q, want SS3 A", key)
	}
}

// Paste honours bracketed paste mode.
func TestPasteHonoursBracketedPaste(t *testing.T) {
	e := term.New(20, 5)
	defer e.Close()

	got := make(chan []byte, 8)
	e.SetReplyHandler(func(p []byte) { got <- p })

	e.Paste("hi")
	if p := <-got; string(p) != "hi" {
		t.Fatalf("plain paste = %q, want %q", p, "hi")
	}

	e.Write([]byte("\x1b[?2004h")) // bracketed paste on
	e.Paste("hi")
	if p := <-got; !strings.HasPrefix(string(p), "\x1b[200~") {
		t.Fatalf("bracketed paste = %q, want it wrapped in 200~", p)
	}
}
