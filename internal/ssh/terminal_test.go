package ssh_test

import (
	"strings"
	"testing"

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
