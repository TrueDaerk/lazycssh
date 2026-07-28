package ssh

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// newSession wires a session to the in-process test server.
func newSession(t *testing.T, srv *testServer, mutate func(*Config)) (Session, chan Event) {
	t.Helper()

	addr, port := srv.Addr()
	cfg := Config{
		Host: hosts.Host{Alias: "test-host", Addr: addr, User: "tester", Port: port},
		Auth: []ssh.AuthMethod{ssh.Password(srv.Password)},

		HostKeyCallback: srv.HostKeyCallback(),
		Timeout:         5 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}

	events := make(chan Event, 256)
	s := New("s1", cfg, events)
	t.Cleanup(func() { s.Close() })
	return s, events
}

func TestSessionConnectsAndReceivesOutput(t *testing.T) {
	srv := newTestServer(t)
	s, events := newSession(t, srv, nil)

	if got := s.State(); got != StatePending {
		t.Errorf("State() before Start = %s, want %s", got, StatePending)
	}

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := s.State(); got != StateConnected {
		t.Fatalf("State() after Start = %s, want %s", got, StateConnected)
	}

	assertStates(t, events, StateDialing, StateAuthenticating, StateConnected)
	waitForOutput(t, s, "welcome")

	if _, err := s.Write([]byte("hello\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	waitForOutput(t, s, "hello")
}

func TestSessionCapturesStderr(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	if _, err := s.Write([]byte("stderr\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Both streams land in the same buffer, interleaved as a terminal shows them.
	waitForOutput(t, s, "to stderr")
}

func TestSessionRequestsPtyAndResizes(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, func(c *Config) {
		c.Term = "screen-256color"
		c.Width, c.Height = 100, 40
	})

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	if got := srv.PtyTerm(); got != "screen-256color" {
		t.Errorf("server saw TERM %q, want %q", got, "screen-256color")
	}
	if w, h := srv.WindowSize(); w != 100 || h != 40 {
		t.Errorf("server saw initial size %dx%d, want 100x40", w, h)
	}

	if err := s.Resize(132, 50); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	waitFor(t, "the server to see the new window size", func() bool {
		w, h := srv.WindowSize()
		return w == 132 && h == 50
	})
}

func TestSessionResizeValidatesInput(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	for _, size := range [][2]int{{0, 24}, {80, 0}, {-1, -1}} {
		if err := s.Resize(size[0], size[1]); err == nil {
			t.Errorf("Resize(%d, %d) returned no error", size[0], size[1])
		}
	}
}

func TestSessionResizeBeforeStartIsRemembered(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	if err := s.Resize(120, 45); err != nil {
		t.Fatalf("Resize before Start: %v", err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	if w, h := srv.WindowSize(); w != 120 || h != 45 {
		t.Errorf("server saw %dx%d, want the size requested before Start, 120x45", w, h)
	}
}

func TestSessionReportsRemoteExit(t *testing.T) {
	srv := newTestServer(t)
	s, events := newSession(t, srv, nil)

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	if _, err := s.Write([]byte("exit\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitFor(t, "the session to report that it closed", func() bool {
		return s.State() == StateClosed
	})
	if err := s.Err(); err != nil {
		t.Errorf("Err() = %v, want nil: a shell exiting is not a transport failure", err)
	}
	drainFor(t, events, StateClosed)
}

func TestSessionRemoteExitStatusIsNotAFailure(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	// "fail" makes the remote shell exit with status 3.
	if _, err := s.Write([]byte("fail\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	waitFor(t, "the session to end", func() bool { return s.State().Done() })
	if got := s.State(); got != StateClosed {
		t.Errorf("State() = %s, want %s: a non-zero exit status ends the shell, it is not a transport error",
			got, StateClosed)
	}
}

func TestSessionRefusesToStartWithoutHostKeyVerification(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, func(c *Config) { c.HostKeyCallback = nil })

	err := s.Start(t.Context())
	if err == nil {
		t.Fatal("Start with no host key callback succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "host key") {
		t.Errorf("error = %q, want it to name host key verification", err)
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
}

func TestSessionFailsOnWrongHostKey(t *testing.T) {
	srv := newTestServer(t)
	other := newTestServer(t) // a different key

	s, events := newSession(t, srv, func(c *Config) { c.HostKeyCallback = other.HostKeyCallback() })

	if err := s.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded against a server presenting an unexpected key")
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
	drainFor(t, events, StateFailed)
}

func TestSessionFailsOnBadAuth(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, func(c *Config) {
		c.Auth = []ssh.AuthMethod{ssh.Password("wrong")}
	})

	err := s.Start(t.Context())
	if err == nil {
		t.Fatal("Start succeeded with a wrong password")
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
	if s.Err() == nil {
		t.Error("Err() = nil after a failed start")
	}
}

func TestSessionFailsOnUnreachableHost(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, func(c *Config) {
		// Port 1 on loopback: nothing listens there.
		c.Host.Port = 1
		c.Timeout = 2 * time.Second
	})

	if err := s.Start(t.Context()); err == nil {
		t.Fatal("Start succeeded against a closed port")
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
}

func TestSessionHonoursContextCancellation(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if err := s.Start(ctx); err == nil {
		t.Fatal("Start with a cancelled context succeeded")
	}
	if got := s.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
}

func TestSessionWriteBeforeStartFails(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	if _, err := s.Write([]byte("x")); err == nil {
		t.Error("Write before Start succeeded, want an error naming the state")
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitForOutput(t, s, "welcome")

	for i := 0; i < 3; i++ {
		if err := s.Close(); err != nil {
			t.Errorf("Close call %d: %v", i+1, err)
		}
	}
	if got := s.State(); got != StateClosed {
		t.Errorf("State() = %s, want %s", got, StateClosed)
	}
}

func TestSessionCloseWithoutStart(t *testing.T) {
	srv := newTestServer(t)
	s, _ := newSession(t, srv, nil)

	if err := s.Close(); err != nil {
		t.Errorf("Close on a session that never started: %v", err)
	}
}

// TestSessionLeaksNoGoroutines is the guarantee that one dead host does not
// accumulate goroutines over a long run with many reconnects.
func TestSessionLeaksNoGoroutines(t *testing.T) {
	srv := newTestServer(t)

	before := runtime.NumGoroutine()

	for i := 0; i < 10; i++ {
		s, _ := newSession(t, srv, nil)
		if err := s.Start(t.Context()); err != nil {
			t.Fatalf("Start %d: %v", i, err)
		}
		waitForOutput(t, s, "welcome")
		if _, err := s.Write([]byte("flood\r")); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
		// Close mid-stream: the reader goroutines must still end.
		if err := s.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}

	waitFor(t, "goroutines to settle", func() bool {
		return runtime.NumGoroutine() <= before+4 // the server keeps a few
	})
}

// TestSessionSurvivesAnUndrainedEventChannel is the backpressure guarantee at
// the session level: a UI that stops reading must not stall the transport.
func TestSessionSurvivesAnUndrainedEventChannel(t *testing.T) {
	srv := newTestServer(t)

	addr, port := srv.Addr()
	events := make(chan Event, 1) // deliberately far too small
	s := New("s1", Config{
		Host:            hosts.Host{Alias: "test-host", Addr: addr, User: "tester", Port: port},
		Auth:            []ssh.AuthMethod{ssh.Password(srv.Password)},
		HostKeyCallback: srv.HostKeyCallback(),
		Timeout:         5 * time.Second,
	}, events)
	t.Cleanup(func() { s.Close() })

	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := s.Write([]byte("flood\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Nobody drains events, yet the output must still arrive in the scrollback.
	waitFor(t, "flooded output to arrive despite an undrained event channel", func() bool {
		return s.Scrollback().Len() > 1000
	})

	// The dropped hints are counted rather than silently lost, so a UI that
	// looks stuck can be diagnosed.
	counter, ok := s.(interface{ DroppedEvents() int })
	if !ok {
		t.Fatal("the real session does not expose DroppedEvents")
	}
	if counter.DroppedEvents() == 0 {
		t.Error("DroppedEvents() = 0, want the discarded hints to have been counted")
	}
}

func TestStateString(t *testing.T) {
	tests := []struct {
		state State
		want  string
		done  bool
	}{
		{StatePending, "pending", false},
		{StateDialing, "dialing", false},
		{StateAuthenticating, "authenticating", false},
		{StateConnected, "connected", false},
		{StateFailed, "failed", true},
		{StateClosed, "closed", true},
		{State(99), "unknown(99)", false},
	}

	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("State(%d).String() = %q, want %q", int(tt.state), got, tt.want)
		}
		if got := tt.state.Done(); got != tt.done {
			t.Errorf("State(%d).Done() = %v, want %v", int(tt.state), got, tt.done)
		}
	}
}

// waitFor polls until cond holds or the test times out.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func waitForOutput(t *testing.T, s Session, want string) {
	t.Helper()
	waitFor(t, "output containing "+want, func() bool {
		return strings.Contains(s.Scrollback().String(), want)
	})
}

// assertStates checks that the given state transitions arrive in order,
// ignoring output notifications.
func assertStates(t *testing.T, events <-chan Event, want ...State) {
	t.Helper()

	for _, w := range want {
		drainFor(t, events, w)
	}
}

// drainFor reads events until the wanted state arrives.
func drainFor(t *testing.T, events <-chan Event, want State) {
	t.Helper()

	timeout := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if se, ok := ev.(StateEvent); ok {
				if se.State == want {
					return
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for the %s event", want)
		}
	}
}
