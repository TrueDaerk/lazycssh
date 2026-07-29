package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// paneHeader renders the one line that identifies a pane: its number, its host
// and the connection state, all read from live state at render time so a state
// change is on screen the moment the redraw happens.
//
// When the width cannot hold everything, the state goes first and the host
// name is truncated from the left: in a fleet of web-01…web-40 the suffix is
// the distinguishing part, and a header full of identical prefixes says
// nothing.
func (a App) paneHeader(host, width int, focused bool) string {
	if host < 0 || host >= len(a.hostIDs()) || width <= 0 {
		return ""
	}
	id := a.hostIDs()[host]
	number := fmt.Sprintf("%d ", host+1)

	state := a.state(id)
	label := " " + state.String()

	avail := width - len(number) - len([]rune(label))
	if avail < minHeaderName {
		label = ""
		avail = width - len(number)
	}
	name := truncateLeft(id, max(0, avail))

	style := a.theme.Muted
	if focused {
		style = a.theme.Cursor
	} else if host == a.paneIndex {
		style = a.theme.Selected
	}

	line := style.Render(number + name)
	if label != "" {
		line += a.theme.State(state).Render(label)
	}
	return line
}

// minHeaderName is the least of a host name worth showing next to the state.
// Below it the state gives up its space instead: an unreadable name helps no
// one, and the border colour still carries the focus.
const minHeaderName = 6

// truncateLeft shortens s to at most width runes by cutting from the left,
// marking the cut with "…". The suffix survives because it is the part that
// distinguishes host-01 from host-40.
func truncateLeft(s string, width int) string {
	r := []rune(s)
	if len(r) <= width {
		return s
	}
	if width < 2 {
		return string(r[len(r)-width:])
	}
	return "…" + string(r[len(r)-width+1:])
}

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
