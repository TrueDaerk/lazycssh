package ui

import (
	"fmt"
	"strings"
	"testing"
)

// pressKeyCmd is pressKey, but keeps the command: copy is delivered as a
// tea.Cmd carrying the clipboard write.
func pressKeyCmd(t *testing.T, a App, keystroke string) (App, string) {
	t.Helper()

	model, cmd := a.Update(keyMsgFor(t, keystroke))
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	if cmd == nil {
		return next, ""
	}
	// tea.SetClipboard's message is an unexported string type; its rendered
	// form is the clipboard content, which is what the test asserts on.
	return next, fmt.Sprint(cmd())
}

// The acceptance criterion of issue 134: a working way to get text from a
// pane into the system clipboard. alt+y takes what is on screen.
func TestAltYCopiesTheVisiblePaneText(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("$ cat /etc/machine-id\ndeadbeef42\n")
	a = focusGrid(t, a)

	a, clip := pressKeyCmd(t, a, "alt+y")
	if !strings.Contains(clip, "deadbeef42") {
		t.Fatalf("the clipboard misses the pane text: %q", clip)
	}
	if strings.Contains(clip, "\x1b") {
		t.Fatalf("the clipboard carries ANSI sequences: %q", clip)
	}
	if !strings.Contains(a.lastDelivery, "web-01") || !strings.Contains(a.lastDelivery, "OSC 52") {
		t.Fatalf("the status line does not report the copy: %q", a.lastDelivery)
	}
}

// alt+d takes the whole retained scrollback, unwrapped and unstyled, clear
// markers excluded.
func TestAltDCopiesTheWholeScrollback(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 60; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}
	fleet.sessions["web-01"].Emit("\x1b[2J\x1b[31mafter-clear\x1b[0m\n")
	a = focusGrid(t, a)

	a, clip := pressKeyCmd(t, a, "alt+d")
	if !strings.Contains(clip, "line-01") || !strings.Contains(clip, "after-clear") {
		t.Fatalf("the clipboard misses scrollback lines: %q", clip)
	}
	if strings.Contains(clip, "\x1b") || strings.Contains(clip, "\x00") {
		t.Fatalf("the clipboard carries escapes or markers: %q", clip)
	}
	if !strings.Contains(a.lastDelivery, "61 lines") {
		t.Fatalf("the status line does not carry the line count: %q", a.lastDelivery)
	}
}

// Copy works while typing into the pane too: alt chords never reach the host.
func TestCopyWorksWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	fleet.sessions["web-01"].Emit("output\n")

	_, clip := pressKeyCmd(t, a, "alt+y")
	if !strings.Contains(clip, "output") {
		t.Fatalf("alt+y while typing copied %q", clip)
	}
	if got := fleet.sessions["web-01"].Written(); strings.Contains(got, "y") {
		t.Fatalf("the chord leaked to the host: %q", got)
	}
}

// An empty pane reports "nothing to copy" and sends no clipboard write.
func TestCopyOnAnEmptyPane(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	a = focusGrid(t, a)

	a, clip := pressKeyCmd(t, a, "alt+y")
	if clip != "" {
		t.Fatalf("an empty pane wrote %q to the clipboard", clip)
	}
	if !strings.Contains(a.lastDelivery, "nothing to copy") {
		t.Fatalf("the status line says %q", a.lastDelivery)
	}
}
