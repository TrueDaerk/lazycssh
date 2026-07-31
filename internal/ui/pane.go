package ui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/term"
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

// altScreenTerminal returns the session's emulator when the remote app is on
// the alternate screen — the signal that a full-screen app (vim, htop, less)
// owns the pane — and nil otherwise.
func (a App) altScreenTerminal(id string) *term.Emulator {
	if a.cfg.Fleet == nil {
		return nil
	}
	session, ok := a.cfg.Fleet.Session(id)
	if !ok {
		return nil
	}
	t := session.Terminal()
	if t == nil || !t.IsAltScreen() {
		return nil
	}
	return t
}

// paneAltScreen reports whether the pane currently shows a full-screen app's
// live grid instead of scrollback text.
func (a App) paneAltScreen(id string) bool { return a.altScreenTerminal(id) != nil }

// terminalGrid renders the emulator's live screen into the pane body: the
// grid clipped to the area, with the remote app's cursor drawn where it says
// it is. No tail, no scroll offset, no search — the remote app owns the whole
// screen, exactly as it would in a plain terminal.
func (a App) terminalGrid(t *term.Emulator, width, height int) string {
	lines := strings.Split(t.Render(), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}

	if t.CursorVisible() {
		x, y := t.CursorPosition()
		if y >= 0 && y < len(lines) && x >= 0 && x < width {
			lines[y] = overlayCursor(lines[y], x, a.theme.Cursor)
		}
	}
	return strings.Join(lines, "\n")
}

// overlayCursor draws the cursor style over column x of a rendered line,
// keeping the surrounding ANSI colours intact.
func overlayCursor(line string, x int, style lipgloss.Style) string {
	if w := ansi.StringWidth(line); w <= x {
		line += strings.Repeat(" ", x-w)
		return line + style.Render(" ")
	}
	left := ansi.Cut(line, 0, x)
	ch := ansi.Strip(ansi.Cut(line, x, x+1))
	if ch == "" {
		ch = " "
	}
	right := ansi.Cut(line, x+1, ansi.StringWidth(line))
	return left + style.Render(ch) + right
}

// paneTerminal returns the session's emulator, or nil when the host is
// unknown.
func (a App) paneTerminal(id string) *term.Emulator {
	if a.cfg.Fleet == nil {
		return nil
	}
	session, ok := a.cfg.Fleet.Session(id)
	if !ok {
		return nil
	}
	return session.Terminal()
}

// virtualLines is one host's whole pane content as styled lines: the retained
// history that scrolled off the screen, then the screen's rows, with a muted
// marker on top when the retention cap has dropped older output. It is the
// coordinate space the scroll offset, the search and the render all share, so
// they cannot disagree about which line is which.
//
// Screen rows below both the cursor and the last content row are dropped: the
// window anchors on what the host said, not on the bottom of a screen that
// may (transiently, or in a test) be taller than the pane.
func (a App) virtualLines(id string) []string {
	lines, _ := a.virtualLinesTop(id)
	return lines
}

// virtualLinesTop is virtualLines plus the index of the first screen row, so
// the cursor's screen coordinates can be mapped into the shared line space.
func (a App) virtualLinesTop(id string) ([]string, int) {
	t := a.paneTerminal(id)
	if t == nil || !t.HasOutput() {
		return nil, 0
	}

	var lines []string
	if t.HistoryFull() {
		// The marker sits where the missing output was, so a reader scrolling
		// to the top learns the history is truncated rather than short.
		lines = append(lines, a.theme.Muted.Render("~ older output dropped ~"))
	}
	for i, n := 0, t.HistoryLen(); i < n; i++ {
		lines = append(lines, t.HistoryLine(i))
	}
	screenTop := len(lines)

	rows := strings.Split(t.Render(), "\n")
	last := -1
	for y := len(rows) - 1; y >= 0; y-- {
		if strings.TrimRight(ansi.Strip(rows[y]), " ") != "" {
			last = y
			break
		}
	}
	return append(lines, rows[:last+1]...), screenTop
}

// paneBody renders one host's terminal into an area of width columns and
// height rows: the live screen when following the tail, a window over
// [history ++ screen] when scrolled back, and the lines the active search
// matches highlighted. A pane whose remote app is on the alternate screen
// renders the live grid alone — no scroll, no search, no selection. A
// connected pane following the tail draws the remote cursor where the
// emulator says it is (issue #190).
func (a App) paneBody(id string, width, height int) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	t := a.paneTerminal(id)
	if t == nil {
		return ""
	}
	if t.IsAltScreen() {
		return a.terminalGrid(t, width, height)
	}
	lines, screenTop := a.virtualLinesTop(id)
	if len(lines) == 0 {
		return ""
	}

	// The cursor may sit on the blank row below the last content — right
	// after a line feed. That row must exist to be drawn on, but only while
	// the pane follows the tail and could show a cursor at all.
	cx, cy := t.CursorPosition()
	cursorIdx := screenTop + cy
	if a.scrollOffset(id) == 0 && a.state(id) == ssh.StateConnected {
		for len(lines) <= cursorIdx {
			lines = append(lines, "")
		}
	}

	// The open auth question's answer is typed inline at the cursor, the way
	// a terminal takes it (issue #180). The echo carries its own cursor block,
	// so the remote cursor is not drawn on top of it.
	echoing := false
	if echo, ok := a.inlineAnswerEcho(id); ok {
		if cursorIdx >= 0 && cursorIdx < len(lines) {
			lines[cursorIdx] = spliceAt(lines[cursorIdx], cx) + echo
			echoing = true
		}
	}

	offset := clamp(a.scrollOffset(id), 0, max(0, len(lines)-height))
	end := len(lines) - offset
	start := max(0, end-height)
	if offset == 0 {
		// Following the tail shows the screen, exactly like a real terminal:
		// after a `clear` the pane is empty even though the history above is
		// still there — scrolling up reaches it.
		start = max(start, screenTop)
	}
	window := append([]string(nil), lines[start:end]...)
	for i := range window {
		window[i] = ansi.Truncate(window[i], width, "")
	}

	if a.searchTerm != "" {
		for i, line := range window {
			if text := ansi.Strip(line); containsFold(text, a.searchTerm) {
				// The whole line takes the match style, its own colours
				// dropped: a highlight fighting the remote's colours would
				// lose.
				window[i] = a.theme.Match.Render(text)
			}
		}
	}

	if offset == 0 && !echoing && a.state(id) == ssh.StateConnected && t.CursorVisible() {
		if i := cursorIdx - start; i >= 0 && i < len(window) && cx >= 0 && cx < width {
			window[i] = overlayCursor(window[i], cx, a.theme.Cursor)
		}
	}
	return strings.Join(window, "\n")
}

// spliceAt cuts a rendered line at column x, padding with spaces when the
// line is shorter, so typed text can be appended at the cursor.
func spliceAt(line string, x int) string {
	if w := ansi.StringWidth(line); w <= x {
		return line + strings.Repeat(" ", x-w)
	}
	return ansi.Cut(line, 0, x)
}
