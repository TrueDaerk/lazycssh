package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Copying text out of a pane. Bubbletea owns the mouse, so the terminal's
// native selection cannot reach the pane content; instead the focused pane's
// text is pushed into the system clipboard over OSC 52, which works locally
// and through SSH. A terminal without OSC 52 support silently ignores the
// sequence — the status line reports what was attempted either way, so the
// failure mode is a wrong status line, never a crash.
//
// Silently ignoring it is common enough that OSC 52 alone is not a clipboard
// (issue #307): macOS Terminal.app never implemented it, and iTerm2 keeps it
// behind an off-by-default preference. A local run therefore also writes the
// text to the machine's own clipboard through [Config.Clipboard]; see
// internal/clipboard for why a run inside an SSH session does not.
//
// Two grains, both keyboard-first per the interaction model: alt+y takes the
// visible window (scroll first to aim it), alt+d takes the whole retained
// scrollback. Clipboard text is plain: ANSI styling is stripped, because a
// paste target wants the ID or the error message, not the colours around it.

// ClipboardWriter puts text on the clipboard of the machine lazycssh runs on,
// the fallback for terminals that ignore OSC 52. Nil means OSC 52 alone.
//
// Write is called from a [tea.Cmd], never inline in Update: the platform
// tools it drives are subprocesses, and the UI loop may not wait on one.
type ClipboardWriter interface {
	Write(text string) error
}

// clipboardCmd is the one way this package puts text on the clipboard: the
// OSC 52 sequence always, plus the local write when there is a writer. Both
// happen in the command, off the Update goroutine, and the command still
// resolves to bubbletea's own clipboard message — the local write is a
// fallback, never a replacement, because it cannot reach the user's machine
// when lazycssh runs over SSH.
func (a App) clipboardCmd(text string) tea.Cmd {
	osc := tea.SetClipboard(text)
	w := a.cfg.Clipboard
	if w == nil {
		return osc
	}
	return func() tea.Msg {
		// A machine without a working clipboard tool is not worth
		// interrupting a copy over: OSC 52 has already gone out, and the
		// status line said what was copied either way.
		_ = w.Write(text)
		return osc()
	}
}

// copyVisible copies the focused pane's visible text to the clipboard.
func (a App) copyVisible() (App, tea.Cmd) {
	id := a.FocusedHost()
	if id == "" {
		return a, nil
	}
	w, h := a.paneExtentAt(a.paneIndex)
	text := strings.TrimRight(ansi.Strip(a.paneBody(id, w, h)), "\n")
	if text == "" {
		a.lastDelivery = id + ": nothing to copy"
		return a, nil
	}
	a.lastDelivery = fmt.Sprintf("copied %s's visible text (OSC 52)", id)
	return a, a.clipboardCmd(text)
}

// copyScrollback copies the focused pane's entire retained content: the
// history that scrolled off the screen and the screen itself, as plain text.
func (a App) copyScrollback() (App, tea.Cmd) {
	id := a.FocusedHost()
	if id == "" {
		return a, nil
	}
	t := a.paneTerminal(id)
	if t == nil {
		return a, nil
	}

	text := t.Text()
	if text == "" {
		a.lastDelivery = id + ": nothing to copy"
		return a, nil
	}
	lines := strings.Count(text, "\n") + 1
	a.lastDelivery = fmt.Sprintf("copied %d line%s of %s's scrollback (OSC 52)",
		lines, plural(lines), id)
	return a, a.clipboardCmd(text)
}
