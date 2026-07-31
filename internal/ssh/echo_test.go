package ssh

import (
	"strings"
	"testing"
)

// filterAll feeds chunks through a fresh filter and returns everything that
// passed, including what a final Stop releases.
func filterAll(t *testing.T, chunks ...string) string {
	t.Helper()
	f := newSetupEchoFilter()
	var out []byte
	for _, c := range chunks {
		out = append(out, f.Filter([]byte(c))...)
	}
	return string(append(out, f.Stop()...))
}

func TestSetupEchoFilter(t *testing.T) {
	echo := string(echoedSetup)
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain output passes", "welcome\r\n$ ", "welcome\r\n$ "},
		{"the echo is removed", "welcome\r\n " + echo + "\r\n$ ", "welcome\r\n \r\n$ "},
		{"both copies are removed", " " + echo + "\r\n$ " + echo + "\r\n$ ", " \r\n$ \r\n$ "},
		{"a third copy passes", echo + echo + echo, echo},
		{"output around the echo survives", "a" + echo + "b", "ab"},
		{"a false start is released", "PROMPT_COMMAND=off\r\n", "PROMPT_COMMAND=off\r\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := filterAll(t, tc.in); got != tc.want {
				t.Fatalf("Filter(%q) passed %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// An echo can be split at any byte by the network; the filter still removes it.
func TestSetupEchoFilterAcrossChunkBoundaries(t *testing.T) {
	stream := "welcome\r\n " + string(echoedSetup) + "\r\n$ "
	want := "welcome\r\n \r\n$ "
	for cut := 1; cut < len(stream); cut++ {
		if got := filterAll(t, stream[:cut], stream[cut:]); got != want {
			t.Fatalf("split at %d: passed %q, want %q", cut, got, want)
		}
	}
}

// A partial match is held back, not dropped: stopping the filter mid-match
// must release the held bytes.
func TestSetupEchoFilterStopReleasesHeldBytes(t *testing.T) {
	f := newSetupEchoFilter()
	partial := string(echoedSetup[:10])

	got := string(f.Filter([]byte("$ " + partial)))
	if got != "$ " {
		t.Fatalf("Filter held %q, want only %q emitted", got, "$ ")
	}
	if flushed := string(f.Stop()); flushed != partial {
		t.Fatalf("Stop() released %q, want %q", flushed, partial)
	}
	// After Stop everything passes through untouched.
	if got := string(f.Filter([]byte(string(echoedSetup)))); got != string(echoedSetup) {
		t.Fatalf("Filter after Stop changed the stream: %q", got)
	}
}

// The acceptance criterion for issue #201: connecting to a real shell over a
// real PTY leaves no trace of the setup line in the scrollback, while the
// user's own output still shows.
func TestSetupEchoNeverReachesTheScrollback(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	// "ok" answers with a hooked prompt: its marker also stands the filter
	// down, which is the moment the whole echo must already be gone.
	if _, err := s.Write([]byte("ok\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForOutput(t, s, "done")

	got := s.Scrollback().String()
	if strings.Contains(got, "PROMPT_COMMAND") {
		t.Fatalf("the setup line leaked into the scrollback: %q", got)
	}
	if !strings.Contains(got, "welcome") || !strings.Contains(got, "done") {
		t.Fatalf("the filter damaged real output: %q", got)
	}
}
