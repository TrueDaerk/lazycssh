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
// Two grains, both keyboard-first per the interaction model: alt+y takes the
// visible window (scroll first to aim it), alt+d takes the whole retained
// scrollback. Clipboard text is plain: ANSI styling is stripped, because a
// paste target wants the ID or the error message, not the colours around it.

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
	return a, tea.SetClipboard(text)
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
	return a, tea.SetClipboard(text)
}
