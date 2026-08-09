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
// parsing. The sequence counts the markers: it is what tells a consumer that
// the command it sent has answered, rather than that an answer exists.
func TestFakeReportsExitCodes(t *testing.T) {
	events := make(chan Event, 8)
	f := NewFake("h1", hosts.Host{Alias: "h1"}, events)

	if _, seq := f.LastExit(); seq != 0 {
		t.Fatalf("a fresh session claims %d markers", seq)
	}

	f.ReportExit(0)
	if code, seq := f.LastExit(); code != 0 || seq != 1 {
		t.Fatalf("LastExit() = %d, %d after a success, want 0, 1", code, seq)
	}

	f.ReportExit(2)
	if code, seq := f.LastExit(); code != 2 || seq != 2 {
		t.Fatalf("LastExit() = %d, %d after a failure, want 2, 2", code, seq)
	}

	// The same marker the emulator swallows is also an event, exactly as it is
	// on a real session: a UI that redraws on ExitEvent must be drivable by
	// the fake.
	var got []ExitEvent
	for len(events) > 0 {
		if ev, ok := (<-events).(ExitEvent); ok {
			got = append(got, ev)
		}
	}
	if len(got) != 2 || got[0].Code != 0 || got[0].Seq != 1 || got[1].Code != 2 || got[1].Seq != 2 {
		t.Fatalf("the fake emitted %v, want codes 0,2 with sequences 1,2", got)
	}
}

// A repeated code is still a new command: the sequence moves even when the
// number does not, which is the only thing that separates "ran again and
// succeeded" from "nothing has happened since".
func TestExitSequenceAdvancesOnAnIdenticalCode(t *testing.T) {
	f := NewFake("h1", hosts.Host{Alias: "h1"}, nil)

	f.ReportExit(0)
	f.ReportExit(0)
	if code, seq := f.LastExit(); code != 0 || seq != 2 {
		t.Fatalf("LastExit() = %d, %d after two successes, want 0, 2", code, seq)
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

	// The hook was sent, but its echo is filtered out of the scrollback: the
	// pane starts clean (#201). The write below still lands after the setup
	// line, because stdin writes are ordered.
	if strings.Contains(s.Terminal().Text(), "PROMPT_COMMAND") {
		t.Fatal("the setup line's echo leaked into the scrollback")
	}

	// Graceful degradation: this tiny shell never ran the hook, so nothing has
	// been reported yet.
	if _, seq := s.LastExit(); seq != 0 {
		t.Fatalf("an exit status was invented before any marker arrived (%d markers)", seq)
	}

	if _, err := s.Write([]byte("oops\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the failure to be reported", func() bool {
		code, seq := s.LastExit()
		return seq > 0 && code == 1
	})

	if _, err := s.Write([]byte("ok\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitFor(t, "the success to replace it", func() bool {
		code, seq := s.LastExit()
		return seq > 1 && code == 0
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

	// By now the shell has echoed the setup line and the session has seen it;
	// the filter must have kept it out of the scrollback (#201).
	if got := s.Terminal().Text(); strings.Contains(got, "PROMPT_COMMAND") {
		t.Fatalf("the setup line's echo leaked into the scrollback: %q", got)
	}
}

// The marker never reaches the user: the emulator consumes the OSC sequence
// invisibly, but the scanner itself must not eat the bytes around it. The
// prompt's trailing space is a blank cell the terminal does not retain.
func TestMarkerLeavesTheOutputIntact(t *testing.T) {
	f := NewFake("h1", hosts.Host{Alias: "h1"}, nil)
	f.Emit("result\r\n\x1b]133;D;0\a$ ")

	got := f.Terminal().Text()
	if !strings.Contains(got, "result") || !strings.Contains(got, "$") {
		t.Fatalf("the scanner damaged the stream: %q", got)
	}
	if strings.Contains(got, "133;D") {
		t.Fatalf("the marker leaked into the pane text: %q", got)
	}
}
