package ssh_test

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// The session's terminal emulator sees the same bytes as the scrollback, so
// screen state (alt screen, grid content) tracks what the host actually sent.
func TestFakeFeedsTerminalEmulator(t *testing.T) {
	f := ssh.NewFake("s1", hosts.Host{Alias: "web1"}, nil)
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer f.Close()

	if f.Terminal().IsAltScreen() {
		t.Fatal("alt screen before any output")
	}

	f.Emit("plain output\r\n")
	if got := ansi.Strip(f.Terminal().Render()); !strings.Contains(got, "plain output") {
		t.Errorf("Terminal().Render() = %q, missing emitted output", got)
	}

	f.Emit("\x1b[?1049hvim screen")
	if !f.Terminal().IsAltScreen() {
		t.Error("alt screen sequence not tracked")
	}

	f.Emit("\x1b[?1049l")
	if f.Terminal().IsAltScreen() {
		t.Error("alt screen leave not tracked")
	}
}

// A terminal query from the remote app is answered into the stdin of the
// session that asked — and only that one; replies never travel through
// broadcast.
func TestTerminalQueryReplyReachesOnlyTheAskingSession(t *testing.T) {
	asking := ssh.NewFake("s1", hosts.Host{Alias: "web1"}, nil)
	other := ssh.NewFake("s2", hosts.Host{Alias: "web2"}, nil)
	for _, f := range []*ssh.Fake{asking, other} {
		if err := f.Start(t.Context()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		defer f.Close()
	}

	// CSI c: primary device attributes. vim sends this on startup and waits.
	asking.Emit("\x1b[c")

	deadline := time.After(2 * time.Second)
	for !strings.Contains(asking.Written(), "\x1b") {
		select {
		case <-deadline:
			t.Fatalf("no reply reached the asking session's stdin; written = %q", asking.Written())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if got := other.Written(); got != "" {
		t.Fatalf("reply leaked to another session's stdin: %q", got)
	}
}

// Closing a session that just emitted a query neither panics nor deadlocks:
// the reply is dropped, not queued.
func TestCloseWithPendingQueryIsSafe(t *testing.T) {
	f := ssh.NewFake("s1", hosts.Host{Alias: "web1"}, nil)
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}

	f.Emit("\x1b[c\x1b[6n")
	done := make(chan struct{})
	go func() {
		defer close(done)
		f.Close()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked with a pending query reply")
	}
}

func TestFakeResizePropagatesToTerminal(t *testing.T) {
	f := ssh.NewFake("s1", hosts.Host{Alias: "web1"}, nil)
	if err := f.Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer f.Close()

	if err := f.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	w, h := f.Terminal().Size()
	if w != 120 || h != 40 {
		t.Errorf("Terminal().Size() = (%d, %d), want (120, 40)", w, h)
	}
}
