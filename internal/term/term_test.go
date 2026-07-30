package term_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

func TestAltScreenTransitions(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"starts on primary screen", "", false},
		{"1049 enters alt screen", "\x1b[?1049h", true},
		{"1049 leaves alt screen", "\x1b[?1049h\x1b[?1049l", false},
		{"1047 enters alt screen", "\x1b[?1047h", true},
		{"1047 leaves alt screen", "\x1b[?1047h\x1b[?1047l", false},
		// Legacy mode ?47 is not implemented by the vt emulator; every
		// terminfo in current use emits ?1049 (or ?1047).
		{"plain output stays primary", "hello\r\nworld\r\n", false},
		{"split across writes", "\x1b[?10", true}, // rest written below
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := term.New(80, 24)
			defer e.Close()

			if _, err := e.Write([]byte(tt.input)); err != nil {
				t.Fatalf("Write: %v", err)
			}
			if tt.name == "split across writes" {
				if _, err := e.Write([]byte("49h")); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}
			if got := e.IsAltScreen(); got != tt.want {
				t.Errorf("IsAltScreen() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRenderShowsWrittenOutput(t *testing.T) {
	e := term.New(80, 24)
	defer e.Close()

	e.Write([]byte("first line\r\nsecond line\r\n"))

	plain := ansi.Strip(e.Render())
	lines := strings.Split(plain, "\n")
	if len(lines) < 2 {
		t.Fatalf("Render() produced %d lines, want at least 2", len(lines))
	}
	if got := strings.TrimRight(lines[0], " "); got != "first line" {
		t.Errorf("line 0 = %q, want %q", got, "first line")
	}
	if got := strings.TrimRight(lines[1], " "); got != "second line" {
		t.Errorf("line 1 = %q, want %q", got, "second line")
	}
}

func TestPrimaryScreenSurvivesAltScreen(t *testing.T) {
	e := term.New(80, 24)
	defer e.Close()

	e.Write([]byte("shell prompt\r\n"))
	e.Write([]byte("\x1b[?1049h\x1b[2Jfull-screen app"))

	if got := ansi.Strip(e.Render()); !strings.Contains(got, "full-screen app") {
		t.Fatalf("alt screen Render() = %q, missing app content", got)
	}

	e.Write([]byte("\x1b[?1049l"))

	if e.IsAltScreen() {
		t.Fatal("still on alt screen after leave sequence")
	}
	if got := ansi.Strip(e.Render()); !strings.Contains(got, "shell prompt") {
		t.Errorf("primary screen Render() = %q, lost pre-app content", got)
	}
}

func TestCursorMovement(t *testing.T) {
	tests := []struct {
		name  string
		input string
		wantX int
		wantY int
	}{
		{"home position", "", 0, 0},
		{"absolute move", "\x1b[5;10H", 9, 4},
		{"move then text", "\x1b[5;10Habc", 12, 4},
		{"carriage return", "abcdef\r", 0, 0},
		{"newline advances row", "one\r\ntwo", 3, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := term.New(80, 24)
			defer e.Close()

			e.Write([]byte(tt.input))
			x, y := e.CursorPosition()
			if x != tt.wantX || y != tt.wantY {
				t.Errorf("CursorPosition() = (%d, %d), want (%d, %d)", x, y, tt.wantX, tt.wantY)
			}
		})
	}
}

func TestResize(t *testing.T) {
	tests := []struct {
		name       string
		newW, newH int
		wantW      int
		wantH      int
	}{
		{"grow", 100, 30, 100, 30},
		{"shrink", 40, 10, 40, 10},
		{"zero width clamps", 0, 30, 1, 30},
		{"negative height clamps", 100, -1, 100, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := term.New(80, 24)
			defer e.Close()

			e.Resize(tt.newW, tt.newH)
			w, h := e.Size()
			if w != tt.wantW || h != tt.wantH {
				t.Errorf("Size() after Resize = (%d, %d), want (%d, %d)", w, h, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestNewClampsSize(t *testing.T) {
	e := term.New(0, -5)
	defer e.Close()

	w, h := e.Size()
	if w != 1 || h != 1 {
		t.Errorf("Size() = (%d, %d), want (1, 1)", w, h)
	}
}

func TestReplyHandlerAnswersDeviceAttributesQuery(t *testing.T) {
	e := term.New(80, 24)
	defer e.Close()

	got := make(chan []byte, 1)
	e.SetReplyHandler(func(p []byte) {
		select {
		case got <- p:
		default:
		}
	})

	// CSI c: primary device attributes. vim sends this on startup and waits.
	e.Write([]byte("\x1b[c"))

	select {
	case reply := <-got:
		if len(reply) == 0 || reply[0] != 0x1b {
			t.Errorf("reply = %q, want an escape sequence", reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no reply to device attributes query")
	}
}

func TestQueryWithoutHandlerDoesNotBlockWrite(t *testing.T) {
	e := term.New(80, 24)
	defer e.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Without a drain, the vt emulator's Write blocks on its reply pipe.
		e.Write([]byte("\x1b[c\x1b[6n"))
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked on an unanswered terminal query")
	}
}

// TestConcurrentWriteAndRead exists for the race detector: the session's
// reader goroutine writes while the UI reads state.
func TestConcurrentWriteAndRead(t *testing.T) {
	e := term.New(80, 24)
	defer e.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			e.Write([]byte("line of output\r\n\x1b[?1049h\x1b[2Jgrid\x1b[?1049l"))
		}
	}()

	for i := 0; i < 200; i++ {
		e.IsAltScreen()
		e.Render()
		e.CursorPosition()
		e.Size()
	}
	<-done
}
