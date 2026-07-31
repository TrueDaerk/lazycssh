package ssh

import (
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

func scanAll(t *testing.T, chunks ...string) []int {
	t.Helper()
	var got []int
	s := exitScanner{onExit: func(code int) { got = append(got, code) }}
	for _, c := range chunks {
		s.Scan([]byte(c))
	}
	return got
}

func TestExitScanner(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []int
	}{
		{"plain output carries no marker", "hello world\r\n", nil},
		{"a zero exit", "\x1b]133;D;0\a", []int{0}},
		{"a non-zero exit", "\x1b]133;D;127\a", []int{127}},
		{"marker embedded in output", "ls: no such file\r\n\x1b]133;D;2\a$ ", []int{2}},
		{"two markers in one chunk", "\x1b]133;D;1\a\x1b]133;D;0\a", []int{1, 0}},
		{"no digits is not a marker", "\x1b]133;D;\a", nil},
		{"too many digits is not a marker", "\x1b]133;D;1234\a", nil},
		{"a letter aborts the code", "\x1b]133;D;1x\a", nil},
		{"an unterminated marker reports nothing", "\x1b]133;D;1", nil},
		{"a different OSC is ignored", "\x1b]0;title\a\x1b]133;D;3\a", []int{3}},
		{"an aborted marker can restart", "\x1b]133;\x1b]133;D;4\a", []int{4}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := scanAll(t, tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("Scan(%q) reported %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Scan(%q) reported %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

// A marker can be split at any byte by the network; the scanner reassembles it.
func TestExitScannerAcrossChunkBoundaries(t *testing.T) {
	marker := "out\r\n\x1b]133;D;42\a$ "
	for cut := 1; cut < len(marker); cut++ {
		got := scanAll(t, marker[:cut], marker[cut:])
		if len(got) != 1 || got[0] != 42 {
			t.Fatalf("split at %d: reported %v, want [42]", cut, got)
		}
	}
	// And byte by byte.
	bytes := make([]string, 0, len(marker))
	for i := range marker {
		bytes = append(bytes, marker[i:i+1])
	}
	if got := scanAll(t, bytes...); len(got) != 1 || got[0] != 42 {
		t.Fatalf("byte-by-byte: reported %v, want [42]", got)
	}
}

// The fake feeds Emit through the same scanner, so UI tests exercise the real
// parsing.
func TestFakeReportsExitCodes(t *testing.T) {
	f := NewFake("h1", hosts.Host{Alias: "h1"}, nil)

	if _, ok := f.LastExit(); ok {
		t.Fatal("a fresh session claims an exit status")
	}

	f.ReportExit(0)
	if code, ok := f.LastExit(); !ok || code != 0 {
		t.Fatalf("LastExit() = %d, %v after a success", code, ok)
	}

	f.ReportExit(2)
	if code, ok := f.LastExit(); !ok || code != 2 {
		t.Fatalf("LastExit() = %d, %v after a failure", code, ok)
	}
}

// The acceptance criterion at the transport: a real session over a real PTY
// arms the hook, sees the marker and reports the code - and before any marker
// arrives it honestly reports that it does not know.
func TestSessionTracksExitCodes(t *testing.T) {
	srv := newTestServer(t)
	s, events := newSession(t, srv, nil)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	// The hook was sent, but its echo is filtered out (issue #201): the
	// scrollback must not show the setup line.
	if strings.Contains(s.Scrollback().String(), "PROMPT_COMMAND") {
		t.Fatal("the setup line's echo reached the scrollback")
	}

	// Graceful degradation: this tiny shell never ran the hook, so nothing has
	// been reported yet.
	if _, ok := s.LastExit(); ok {
		t.Fatal("an exit status was invented before any marker arrived")
	}

	if _, err := s.Write([]byte("oops\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the failure to be reported", func() bool {
		code, ok := s.LastExit()
		return ok && code == 1
	})

	if _, err := s.Write([]byte("ok\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the success to replace it", func() bool {
		code, ok := s.LastExit()
		return ok && code == 0
	})

	// The events carried the codes too, in order.
	var codes []int
	for {
		select {
		case ev := <-events:
			if exit, ok := ev.(ExitEvent); ok {
				codes = append(codes, exit.Code)
			}
			continue
		default:
		}
		break
	}
	if len(codes) < 2 || codes[len(codes)-2] != 1 || codes[len(codes)-1] != 0 {
		t.Fatalf("ExitEvents carried %v, want ...1, 0", codes)
	}
}

// The marker never reaches the user: it is stored in the scrollback verbatim
// and stripped by the renderer, but the scanner itself must not eat the bytes
// around it.
func TestMarkerLeavesTheOutputIntact(t *testing.T) {
	f := NewFake("h1", hosts.Host{Alias: "h1"}, nil)
	f.Emit("result\r\n\x1b]133;D;0\a$ ")

	got := f.Scrollback().String()
	if !strings.Contains(got, "result") || !strings.Contains(got, "$ ") {
		t.Fatalf("the scanner damaged the stream: %q", got)
	}
}
