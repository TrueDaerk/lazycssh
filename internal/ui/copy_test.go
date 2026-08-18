package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeClipboard records what the local clipboard fallback was handed.
type fakeClipboard struct {
	written []string
	err     error
}

func (c *fakeClipboard) Write(text string) error {
	c.written = append(c.written, text)
	return c.err
}

// The OSC 52 sequence is not a clipboard on its own: terminals that ignore it
// (macOS Terminal.app has no support at all, iTerm2 keeps it off by default)
// left every copy silently unpastable (issue #307). A local run therefore
// writes the same text to the machine's own clipboard as well, and the
// command still resolves to bubbletea's clipboard message so OSC 52 goes out
// unchanged for the run over SSH that only it can serve.
func TestCopyAlsoWritesTheLocalClipboard(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	board := &fakeClipboard{}
	a.cfg.Clipboard = board
	fleet.sessions["web-01"].Emit("deadbeef42\n")
	a = focusGrid(t, a)

	_, clip := pressKeyCmd(t, a, "alt+y")

	if !strings.Contains(clip, "deadbeef42") {
		t.Fatalf("OSC 52 no longer carries the text: %q", clip)
	}
	if len(board.written) != 1 || !strings.Contains(board.written[0], "deadbeef42") {
		t.Fatalf("the local clipboard got %q, want the copied text once", board.written)
	}
}

// A machine whose clipboard tool is missing or broken must not lose the copy:
// OSC 52 has already gone out, and a failed fallback is not worth an error.
func TestCopySurvivesAFailingLocalClipboard(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	a.cfg.Clipboard = &fakeClipboard{err: errors.New("no clipboard tool")}
	fleet.sessions["web-01"].Emit("deadbeef42\n")
	a = focusGrid(t, a)

	_, clip := pressKeyCmd(t, a, "alt+y")

	if !strings.Contains(clip, "deadbeef42") {
		t.Fatalf("a failing local clipboard swallowed the copy: %q", clip)
	}
}

// The mouse selection takes the same path, which is the copy issue #307 was
// reported against: select in a focused pane, press ctrl+c/cmd+c, paste
// elsewhere.
func TestSelectionCopyAlsoWritesTheLocalClipboard(t *testing.T) {
	a, _, body := selApp(t)
	board := &fakeClipboard{}
	a.cfg.Clipboard = board

	a, release := dragCmd(t, a, body.X, body.Y, body.X+4, body.Y)
	if release == nil {
		t.Fatal("copy-on-select emitted no clipboard command")
	}
	release()
	if _, clip := superC(t, a); !strings.Contains(clip, "alpha") {
		t.Fatalf("OSC 52 no longer carries the selection: %q", clip)
	}

	// Once on release (copy-on-select), once for the chord.
	if len(board.written) != 2 {
		t.Fatalf("the local clipboard got %q, want the copy-on-select and the chord", board.written)
	}
	for _, got := range board.written {
		if !strings.Contains(got, "alpha") {
			t.Fatalf("the local clipboard got %q, want the selected text", got)
		}
	}
}

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
	if !strings.Contains(a.lastDelivery, "lines of web-01's scrollback") {
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
