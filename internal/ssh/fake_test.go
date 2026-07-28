package ssh

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// The fake must be usable everywhere a Session is expected; that is its entire
// reason to exist.
var _ Session = (*Fake)(nil)

func fakeHost() hosts.Host {
	return hosts.Host{Alias: "srv1", Addr: "srv1.example.com", User: "tester", Port: 22}
}

func TestFakeConnects(t *testing.T) {
	events := make(chan Event, 16)
	f := NewFake("s1", fakeHost(), events)
	f.Banner = "Welcome to srv1\r\n"

	if got := f.State(); got != StatePending {
		t.Errorf("State() before Start = %s, want %s", got, StatePending)
	}

	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := f.State(); got != StateConnected {
		t.Fatalf("State() = %s, want %s", got, StateConnected)
	}
	assertStates(t, events, StateDialing, StateAuthenticating, StateConnected)

	if got := f.Scrollback().String(); !strings.Contains(got, "Welcome to srv1") {
		t.Errorf("scrollback = %q, want the banner", got)
	}
}

func TestFakeSlowConnect(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	f.ConnectDelay = 50 * time.Millisecond

	start := time.Now()
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if elapsed := time.Since(start); elapsed < f.ConnectDelay {
		t.Errorf("Start returned after %v, want at least the scripted %v", elapsed, f.ConnectDelay)
	}
}

func TestFakeSlowConnectIsCancellable(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	f.ConnectDelay = 10 * time.Second

	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := f.Start(ctx)
	if err == nil {
		t.Fatal("Start with a cancelled context succeeded")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Start took %v to notice cancellation, want it to be prompt", elapsed)
	}
	if got := f.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
}

func TestFakeFailures(t *testing.T) {
	tests := []struct {
		name       string
		script     func(*Fake)
		wantState  State
		wantErrMsg string
		// wantSeen is the last state reached before the failure.
		wantSeen State
	}{
		{
			name:       "unreachable host fails while dialing",
			script:     func(f *Fake) { f.DialErr = errors.New("connection refused") },
			wantState:  StateFailed,
			wantErrMsg: "connection refused",
			wantSeen:   StateDialing,
		},
		{
			name:       "wrong credential fails while authenticating",
			script:     func(f *Fake) { f.AuthErr = errors.New("permission denied") },
			wantState:  StateFailed,
			wantErrMsg: "permission denied",
			wantSeen:   StateAuthenticating,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events := make(chan Event, 16)
			f := NewFake("s1", fakeHost(), events)
			tt.script(f)

			err := f.Start(t.Context())
			if err == nil {
				t.Fatal("Start succeeded, want the scripted failure")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantErrMsg)
			}
			if got := f.State(); got != tt.wantState {
				t.Errorf("State() = %s, want %s", got, tt.wantState)
			}
			if f.Err() == nil {
				t.Error("Err() = nil after a failed start")
			}
			assertStates(t, events, tt.wantSeen, StateFailed)
		})
	}
}

func TestFakeEchoAndResponses(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	f.EchoInput = true
	f.Responses = map[string]string{
		"hostname": "srv1\r\n",
		"whoami":   "root\r\n",
	}

	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if _, err := f.Write([]byte("hostname\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	got := f.Scrollback().String()
	if !strings.Contains(got, "hostname") {
		t.Errorf("scrollback = %q, want the echoed input", got)
	}
	if !strings.Contains(got, "srv1") {
		t.Errorf("scrollback = %q, want the scripted response", got)
	}
	if want := "hostname\r"; f.Written() != want {
		t.Errorf("Written() = %q, want %q", f.Written(), want)
	}
}

func TestFakeResponseNeedsACompleteLine(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	f.Responses = map[string]string{"hostname": "srv1\r\n"}

	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Typed one character at a time, the response must only fire on the return.
	for _, c := range "hostname" {
		if _, err := f.Write([]byte{byte(c)}); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if got := f.Scrollback().String(); strings.Contains(got, "srv1") {
		t.Fatalf("scrollback = %q, want no response before the line is complete", got)
	}

	if _, err := f.Write([]byte("\r")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := f.Scrollback().String(); !strings.Contains(got, "srv1") {
		t.Errorf("scrollback = %q, want the response once the line completed", got)
	}
}

func TestFakeWriteBeforeStartFails(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)

	if _, err := f.Write([]byte("x")); err == nil {
		t.Error("Write before Start succeeded, want an error")
	}
	if got := f.Written(); got != "" {
		t.Errorf("Written() = %q, want nothing recorded for a rejected write", got)
	}
}

func TestFakeDisconnectMidSession(t *testing.T) {
	events := make(chan Event, 16)
	f := NewFake("s1", fakeHost(), events)

	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	f.Emit("some output\r\n")
	f.Disconnect(ErrDisconnected())

	if got := f.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s", got, StateFailed)
	}
	if err := f.Err(); err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Errorf("Err() = %v, want the disconnect reason", err)
	}
	drainFor(t, events, StateFailed)

	// The scrollback of a dead session stays readable: the pane still shows
	// what the host said before it died.
	if got := f.Scrollback().String(); !strings.Contains(got, "some output") {
		t.Errorf("scrollback = %q, want the output from before the disconnect", got)
	}
	if _, err := f.Write([]byte("x")); err == nil {
		t.Error("Write to a disconnected session succeeded")
	}
}

func TestFakeExitWithStatus(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	f.ExitWithStatus(3)

	if got := f.State(); got != StateClosed {
		t.Errorf("State() = %s, want %s: a non-zero exit ends the shell, it is not a transport failure",
			got, StateClosed)
	}
	if err := f.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

func TestFakeFlood(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	f.Flood(20_000)

	if got, want := f.Scrollback().Len(), 10_000; got != want {
		t.Errorf("scrollback holds %d lines, want it capped at %d", got, want)
	}
	if f.Scrollback().Dropped() == 0 {
		t.Error("Dropped() = 0, want the flood to have overflowed the buffer")
	}
}

func TestFakeResize(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)

	if w, h := f.Size(); w != DefaultWidth || h != DefaultHeight {
		t.Errorf("initial size = %dx%d, want %dx%d", w, h, DefaultWidth, DefaultHeight)
	}

	if err := f.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if w, h := f.Size(); w != 120 || h != 40 {
		t.Errorf("size = %dx%d, want 120x40", w, h)
	}
	if got := f.Resizes(); got != 1 {
		t.Errorf("Resizes() = %d, want 1", got)
	}

	for _, size := range [][2]int{{0, 24}, {80, 0}, {-5, -5}} {
		if err := f.Resize(size[0], size[1]); err == nil {
			t.Errorf("Resize(%d, %d) returned no error", size[0], size[1])
		}
	}
	if got := f.Resizes(); got != 1 {
		t.Errorf("Resizes() = %d after rejected resizes, want it unchanged at 1", got)
	}
}

func TestFakeCloseIsIdempotent(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := f.Close(); err != nil {
			t.Errorf("Close call %d: %v", i+1, err)
		}
	}
	if got := f.State(); got != StateClosed {
		t.Errorf("State() = %s, want %s", got, StateClosed)
	}
}

func TestFakeCloseKeepsAFailureVisible(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	f.DialErr = errors.New("connection refused")
	f.Start(t.Context())

	if err := f.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := f.State(); got != StateFailed {
		t.Errorf("State() = %s, want %s: closing a failed session must not hide why it failed",
			got, StateFailed)
	}
	if f.Err() == nil {
		t.Error("Err() = nil after closing a failed session")
	}
}

func TestFakeNeverBlocksOnAnUndrainedChannel(t *testing.T) {
	events := make(chan Event, 1)
	f := NewFake("s1", fakeHost(), events)

	done := make(chan struct{})
	go func() {
		defer close(done)
		f.Start(t.Context())
		f.Flood(1000)
		f.Close()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the fake blocked on an undrained event channel; it must mirror the real session")
	}
}

func TestFakeIsConcurrencySafe(t *testing.T) {
	f := NewFake("s1", fakeHost(), nil)
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				f.Write([]byte("x\r"))
				f.Emit("out\r\n")
				_ = f.State()
				_ = f.Written()
				_ = f.Scrollback().Lines()
				f.Resize(80+j%10, 24)
			}
		}()
	}
	wg.Wait()
}
