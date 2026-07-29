package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// paneBody renders one host's scrollback into an area of width columns and
// height rows: sanitized, hard-wrapped, following the tail. It is a pure
// function of the buffer's current content, so two renders of the same state
// cannot disagree and the tests need no terminal.
func (a App) paneBody(id string, width, height int) string {
	if width <= 0 || height <= 0 || a.cfg.Fleet == nil {
		return ""
	}
	session, ok := a.cfg.Fleet.Session(id)
	if !ok {
		return ""
	}

	buf := session.Scrollback()
	raw := buf.Lines()
	if len(raw) == 0 && buf.Dropped() == 0 {
		return ""
	}

	lines := make([]string, 0, len(raw)+1)
	if dropped := buf.Dropped(); dropped > 0 {
		// The marker sits where the missing output was, so a reader scrolling
		// to the top learns the history is truncated rather than short.
		lines = append(lines, a.theme.Muted.Render(
			fmt.Sprintf("~ %d line%s dropped ~", dropped, plural(dropped))))
	}
	for _, line := range raw {
		lines = append(lines, sanitizeLine(line))
	}

	// Hardwrap keeps ANSI colours intact across the break and counts wide
	// characters correctly, which a naive byte slice would not.
	wrapped := strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), width, true), "\n")

	// Follow the tail: the newest output is what the user is watching for.
	// Scrolling back through the buffer is the navigation issue, not this one.
	if len(wrapped) > height {
		wrapped = wrapped[len(wrapped)-height:]
	}
	return strings.Join(wrapped, "\n")
}
