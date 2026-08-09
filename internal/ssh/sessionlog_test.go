package ssh

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
)

// logSink is a concurrency-safe writer standing in for a session log: the
// stdout and stderr pumps write to it from their own goroutines.
type logSink struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logSink) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logSink) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// waitForLog polls until the sink contains want, mirroring waitForOutput.
func waitForLog(t *testing.T, sink *logSink, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(sink.String(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("log never contained %q; log = %q", want, sink.String())
}

func TestSessionCopiesOutputToTheLog(t *testing.T) {
	srv := newTestServer(t)
	sink := &logSink{}
	s, _ := newSession(t, srv, func(c *Config) { c.Log = sink })

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")
	waitForLog(t, sink, "welcome")

	// Both streams reach the log, the way they reach the emulator.
	if _, err := s.Write([]byte("stderr\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForLog(t, sink, "to stderr")
}

func TestSessionWithoutALogStillRuns(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")
}
