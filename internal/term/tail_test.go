package term

import (
	"strings"
	"testing"
)

// TailText and TextLineCount exist so per-event readers stop materializing
// the whole retained history (issue #274). Their contract is equivalence:
// whatever Text returns, TextLineCount counts it and TailText returns its
// suffix.

func tailTestEmulators(t *testing.T) map[string]*Emulator {
	t.Helper()
	fresh := func() *Emulator {
		e := New(40, 6)
		t.Cleanup(func() { e.Close() })
		return e
	}

	empty := fresh()

	short := fresh()
	short.Write([]byte("one\r\ntwo\r\nthree"))

	scrolled := fresh()
	for i := 0; i < 500; i++ {
		scrolled.Write([]byte("history line with some text\r\n"))
	}
	scrolled.Write([]byte("the newest line"))

	blankTail := fresh()
	blankTail.Write([]byte("content\r\n\r\n\r\n"))

	styled := fresh()
	styled.Write([]byte("\x1b[31mred\x1b[0m plain\r\n\x1b[1mbold\x1b[0m\r\n"))

	altScreen := fresh()
	altScreen.Write([]byte("before\r\n"))
	altScreen.Write([]byte("\x1b[?1049h"))
	altScreen.Write([]byte("full screen app"))

	return map[string]*Emulator{
		"empty":      empty,
		"short":      short,
		"scrolled":   scrolled,
		"blank tail": blankTail,
		"styled":     styled,
		"alt screen": altScreen,
	}
}

func TestTextLineCountMatchesText(t *testing.T) {
	for name, e := range tailTestEmulators(t) {
		text := e.Text()
		want := 0
		if text != "" {
			want = len(strings.Split(text, "\n"))
		}
		if got := e.TextLineCount(); got != want {
			t.Errorf("%s: TextLineCount() = %d, want %d (text %q)", name, got, want, text)
		}
	}
}

func TestTailTextIsTextSuffix(t *testing.T) {
	for name, e := range tailTestEmulators(t) {
		text := e.Text()
		var lines []string
		if text != "" {
			lines = strings.Split(text, "\n")
		}
		for _, n := range []int{0, 1, 3, 200, 10000} {
			want := ""
			if n > 0 && len(lines) > 0 {
				from := max(0, len(lines)-n)
				want = strings.Join(lines[from:], "\n")
			}
			if got := e.TailText(n); got != want {
				t.Errorf("%s: TailText(%d) = %q, want %q", name, n, got, want)
			}
		}
	}
}
