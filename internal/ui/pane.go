package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/scrollback"
)

// paneCloseButton is the clickable close control at the right end of a pane
// header. Literal characters, so it survives a terminal without colour, and a
// fixed width, so the hit-test arithmetic cannot drift from the rendering.
const paneCloseButton = "[x]"

// paneHeader renders the one line that identifies a pane: its number, its
// host, the connection state and the last exit code, all read from the model's
// fleet snapshot - the fleet event that changed them refreshed it, so a change
// is on screen the moment the redraw happens.
//
// When the width cannot hold everything, the state goes first and the exit
// code second - a failure must outlive the state label - and the host name is
// truncated from the left: in a fleet of web-01…web-40 the suffix is the
// distinguishing part, and a header full of identical prefixes says nothing.
func (a App) paneHeader(host, width int, focused bool) string {
	ids := a.hostIDs()
	if host < 0 || host >= len(ids) || width <= 0 {
		return ""
	}
	id := ids[host]
	number := fmt.Sprintf("%d ", host+1)

	state := a.state(id)
	stateLabel := " " + state.String()

	exitLabel := a.exitLabel(id)
	exitWidth := 0
	if exitLabel != "" {
		exitWidth = 1 + len([]rune(ansi.Strip(exitLabel)))
	}

	buttonWidth := 0
	if width >= len(number)+minHeaderName+len(paneCloseButton)+1 {
		buttonWidth = len(paneCloseButton) + 1
	}

	avail := width - len(number) - len([]rune(stateLabel)) - exitWidth - buttonWidth
	if avail < minHeaderName {
		stateLabel = ""
		avail = width - len(number) - exitWidth - buttonWidth
	}
	if avail < minHeaderName {
		exitLabel = ""
		avail = width - len(number) - buttonWidth
	}
	name := truncateLeft(id, max(0, avail))

	style := a.theme.Muted
	if focused {
		style = a.theme.Cursor
	} else if host == a.paneIndex {
		style = a.theme.Selected
	}

	line := style.Render(number + name)
	if stateLabel != "" {
		line += a.theme.State(state).Render(stateLabel)
	}
	if exitLabel != "" {
		line += " " + exitLabel
	}
	if buttonWidth > 0 {
		// The button sits flush right, where paneCloseHit expects it.
		if pad := width - lipgloss.Width(line) - len(paneCloseButton); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		line += a.theme.PaneButton.Render(paneCloseButton)
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

// wrappedLines is one host's scrollback as the pane draws it: sanitized,
// dropped-marker first, hard-wrapped at the width. It is a pure function of
// the buffer's current content, so two renders of the same state cannot
// disagree and the tests need no terminal.
func (a App) wrappedLines(id string, width int) []string {
	lines, _ := a.wrappedLinesTail(id, width)
	return lines
}

// wrappedLinesTail is wrappedLines plus where the tail view begins: the first
// wrapped line after the last clear-screen marker, 0 when the host never
// cleared. A pane following the tail starts there, so "clear" leaves an
// apparently empty pane while the history above it stays scrollable.
//
// This is the one live read left in the render path, deliberately: the
// scrollback buffer is internally synchronized (scrollback.Buffer holds its
// own mutex and Lines returns a copy), and snapshotting whole scrollbacks
// into the model on every output event would copy far more than a redraw
// reads. Session state, by contrast, renders only from the model's snapshot.
func (a App) wrappedLinesTail(id string, width int) ([]string, int) {
	if width <= 0 || a.cfg.Fleet == nil {
		return nil, 0
	}
	session, ok := a.cfg.Fleet.Session(id)
	if !ok {
		return nil, 0
	}

	buf := session.Scrollback()
	raw := buf.Lines()
	if len(raw) == 0 && buf.Dropped() == 0 {
		return nil, 0
	}

	lines := make([]string, 0, len(raw)+1)
	if dropped := buf.Dropped(); dropped > 0 {
		// The marker sits where the missing output was, so a reader scrolling
		// to the top learns the history is truncated rather than short.
		lines = append(lines, a.theme.Muted.Render(
			fmt.Sprintf("~ %d line%s dropped ~", dropped, plural(dropped))))
	}
	cleared := -1 // index in lines of the last clear marker
	for _, line := range raw {
		if line == scrollback.ClearMark {
			cleared = len(lines)
			lines = append(lines, a.theme.Muted.Render("~ screen cleared ~"))
			continue
		}
		lines = append(lines, sanitizeLine(line))
	}

	// Hardwrap keeps ANSI colours intact across the break and counts wide
	// characters correctly, which a naive byte slice would not. Wrapping the
	// two halves separately keeps the marker's wrapped position known; each
	// line wraps independently, so the split changes nothing else.
	if cleared < 0 {
		return strings.Split(ansi.Hardwrap(strings.Join(lines, "\n"), width, true), "\n"), 0
	}
	head := strings.Split(ansi.Hardwrap(strings.Join(lines[:cleared+1], "\n"), width, true), "\n")
	if cleared+1 == len(lines) {
		return head, len(head)
	}
	tail := strings.Split(ansi.Hardwrap(strings.Join(lines[cleared+1:], "\n"), width, true), "\n")
	return append(head, tail...), len(head)
}

// paneBody renders one host's scrollback into an area of width columns and
// height rows, following the tail unless the pane is scrolled back, and
// highlighting the lines the active search matches.
func (a App) paneBody(id string, width, height int) string {
	if height <= 0 {
		return ""
	}
	wrapped, tailStart := a.wrappedLinesTail(id, width)
	if len(wrapped) == 0 {
		return ""
	}

	offset := clamp(a.scrollOffset(id), 0, max(0, len(wrapped)-height))
	end := len(wrapped) - offset
	start := max(0, end-height)
	if offset == 0 {
		// Following the tail after a clear shows only what came after it -
		// the cleared pane a terminal would show - while scrolling up still
		// reaches the history and the marker where the clear happened.
		start = max(start, tailStart)
	}
	window := wrapped[start:end]

	if a.searchTerm == "" {
		return strings.Join(window, "\n")
	}

	out := make([]string, len(window))
	for i, line := range window {
		if text := ansi.Strip(line); containsFold(text, a.searchTerm) {
			// The whole line takes the match style, its own colours dropped:
			// a highlight fighting the remote's colours would lose.
			out[i] = a.theme.Match.Render(text)
			continue
		}
		out[i] = line
	}
	return strings.Join(out, "\n")
}
