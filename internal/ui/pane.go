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
// host, the connection state and the last command's exit status, all read from
// the model's fleet snapshot - the fleet event that changed them refreshed it,
// so a change is on screen the moment the redraw happens.
//
// When the width cannot hold everything, the state goes first and the exit
// status second - a failure must outlive the state label - and the host name is
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

// droppedMarkerText is the dropped-output marker's text, a constant so the
// search can test it against the term without rendering the line.
const droppedMarkerText = "~ older output dropped ~"

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
// grid clipped to the area. No tail, no scroll offset, no search — the remote
// app owns the whole screen, exactly as it would in a plain terminal. The
// cursor is not painted in here; it is the frame's terminal cursor, placed by
// [App.paneCursor] when the pane has focus.
func terminalGrid(t *term.Emulator, width, height int) string {
	lines := strings.Split(t.Render(), "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
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
//
// It materializes the entire retained history — milliseconds and megabytes at
// the retention cap (issue #274) — so nothing on the per-frame render path may
// call it: the render fetches only its window through [App.paneContent], and
// the scroll clamps use [App.virtualLineCount]. What remains on this is the
// search, which needs every line's index.
func (a App) virtualLines(id string) []string {
	lines, _ := a.virtualLinesTop(id)
	return lines
}

// virtualLinesTop is virtualLines plus the index of the first screen row, so
// the cursor's screen coordinates can be mapped into the shared line space.
func (a App) virtualLinesTop(id string) ([]string, int) {
	c, ok := a.paneContent(id)
	if !ok {
		return nil, 0
	}
	lines := make([]string, c.total)
	for i := range lines {
		lines[i] = c.line(a, i)
	}
	return lines, c.screenTop
}

// paneContent is the shape of one host's virtual line space - the coordinate
// system of virtualLines - without the lines themselves: how many there are,
// where the screen starts, and enough to materialize any single line on
// demand. The render works from this so that a frame costs its window, not
// the retention cap (issue #274).
type paneContent struct {
	t *term.Emulator
	// marker says line 0 is the dropped-output marker.
	marker bool
	// histLen is how many retained lines scrolled off the screen.
	histLen int
	// screen is the visible rows, trimmed of the rows below the last content.
	screen []string
	// screenTop is the virtual index of the first screen row.
	screenTop int
	// total is the number of virtual lines.
	total int
}

// paneContent measures a host's pane content, through the frame's memo when
// one is live. ok is false when the host is unknown or has said nothing yet.
//
// Going through the memo is not only cheaper: the body and the cursor then
// read the same screen, and a reader goroutine writing between the two would
// otherwise be able to put the caret a row away from the text it belongs to.
// The memo's miss path goes through the cross-frame cache (rendercache.go),
// so a host that has said nothing since the last frame is not re-measured;
// the memo stays the per-frame pin that keeps one View on one snapshot.
func (a App) paneContent(id string) (paneContent, bool) {
	if a.memo == nil {
		return a.cachedPaneContent(id)
	}
	if hit, ok := a.memo.content[id]; ok {
		return hit.c, hit.ok
	}
	c, ok := a.cachedPaneContent(id)
	if a.memo.content == nil {
		a.memo.content = make(map[string]memoPaneContent, 1)
	}
	a.memo.content[id] = memoPaneContent{c: c, ok: ok}
	return c, ok
}

// measurePaneContent is [App.paneContent] without the memo.
func (a App) measurePaneContent(id string) (paneContent, bool) {
	t := a.paneTerminal(id)
	if t == nil || !t.HasOutput() {
		return paneContent{}, false
	}
	c := paneContent{t: t, marker: t.HistoryFull(), histLen: t.HistoryLen()}

	rows := strings.Split(t.Render(), "\n")
	last := -1
	for y := len(rows) - 1; y >= 0; y-- {
		if strings.TrimRight(ansi.Strip(rows[y]), " ") != "" {
			last = y
			break
		}
	}
	c.screen = rows[:last+1]
	c.screenTop = c.histLen
	if c.marker {
		c.screenTop++
	}
	c.total = c.screenTop + len(c.screen)
	return c, true
}

// line materializes one virtual line. An index past the end is the blank row
// a cursor may sit on after a line feed.
func (c paneContent) line(a App, i int) string {
	if c.marker {
		if i == 0 {
			// The marker sits where the missing output was, so a reader
			// scrolling to the top learns the history is truncated rather
			// than short.
			return a.theme.Muted.Render(droppedMarkerText)
		}
		i--
	}
	if i < c.histLen {
		return c.t.HistoryLine(i)
	}
	if j := i - c.histLen; j < len(c.screen) {
		return c.screen[j]
	}
	return ""
}

// virtualLineCount is how many lines virtualLines would return, without
// building them - what the scroll clamps need.
func (a App) virtualLineCount(id string) int {
	c, ok := a.paneContent(id)
	if !ok {
		return 0
	}
	return c.total
}

// paneWindow is the slice of a host's virtual line space one pane body shows,
// together with where the remote cursor falls in it. The render and the
// cursor placement both work from this, so what is drawn and where the caret
// goes cannot disagree (issue #292).
type paneWindow struct {
	c paneContent
	// start and end bound the window in virtual line indexes, end exclusive.
	start, end int
	// total is the virtual line count the window was cut from, which may be
	// one more than the content has: see [App.paneWindowFor].
	total int
	// cursorIdx is the virtual line the remote cursor stands on and cursorCol
	// its column.
	cursorIdx, cursorCol int
	// cursorLive reports that the pane could show a cursor at all: a connected
	// host, following the tail, with the remote app showing its cursor.
	// Whether the cursor lands inside the window is [paneWindow.cursorCell].
	cursorLive bool
}

// cursorCell locates the remote cursor inside the window, in cells counted
// from the window's top-left. ok is false when no cursor belongs on screen.
func (w paneWindow) cursorCell() (x, y int, ok bool) {
	y = w.cursorIdx - w.start
	return w.cursorCol, y, w.cursorLive && y >= 0 && y < w.end-w.start && w.cursorCol >= 0
}

// paneWindowFor computes the window a pane body of the given height shows.
func (a App) paneWindowFor(id string, height int) (paneWindow, bool) {
	t := a.paneTerminal(id)
	if t == nil {
		return paneWindow{}, false
	}
	c, ok := a.paneContent(id)
	if !ok || c.total == 0 {
		return paneWindow{}, false
	}

	cx, cy := t.CursorPosition()
	w := paneWindow{c: c, total: c.total, cursorIdx: c.screenTop + cy, cursorCol: cx}

	// The cursor may sit on the blank row below the last content — right
	// after a line feed. That row must exist to be drawn on, but only while
	// the pane follows the tail and could show a cursor at all.
	connected := a.state(id) == ssh.StateConnected
	if a.scrollOffset(id) == 0 && connected && w.total <= w.cursorIdx {
		w.total = w.cursorIdx + 1
	}

	offset := clamp(a.scrollOffset(id), 0, max(0, w.total-height))
	w.end = w.total - offset
	w.start = max(0, w.end-height)
	if offset == 0 {
		// Following the tail shows the screen, exactly like a real terminal:
		// after a `clear` the pane is empty even though the history above is
		// still there — scrolling up reaches it.
		w.start = max(w.start, c.screenTop)
	}
	// Scrolled back, the window is history: a cursor in it would claim input
	// goes somewhere it does not.
	w.cursorLive = offset == 0 && connected && t.CursorVisible()
	return w, true
}

// paneBody renders one host's terminal into an area of width columns and
// height rows: the live screen when following the tail, a window over
// [history ++ screen] when scrolled back, and the lines the active search
// matches highlighted. A pane whose remote app is on the alternate screen
// renders the live grid alone — no scroll, no search, no selection.
//
// The remote cursor is not painted into the body. The focused pane's caret is
// the frame's terminal cursor, so it blinks in the one pane the keyboard
// reaches instead of in every connected one (issue #292) — [App.paneCursor]
// places it — and the other broadcast targets get their mark painted onto the
// rendered body by [App.paintRemoteCursor] (issue #301). The body itself stays
// the text the pane holds, which is what the clipboard and the text selection
// read out of it.
func (a App) paneBody(id string, width, height int) string {
	if height <= 0 || width <= 0 {
		return ""
	}
	t := a.paneTerminal(id)
	if t == nil {
		return ""
	}
	if t.IsAltScreen() {
		return terminalGrid(t, width, height)
	}
	w, ok := a.paneWindowFor(id, height)
	if !ok {
		return ""
	}
	// Only the window is materialized: everything above it — the whole
	// retained history, at the cap — stays untouched cells in the emulator
	// (issue #274).
	window := make([]string, w.end-w.start)
	for i := range window {
		window[i] = w.c.line(a, w.start+i)
	}

	// The open auth question's answer is typed inline at the cursor, the way
	// a terminal takes it (issue #180). The echo carries its own cursor block,
	// because the answer is typed into the pane rather than into the remote.
	if echo, ok := a.inlineAnswerEcho(id); ok && w.cursorIdx >= 0 && w.cursorIdx < w.total {
		if i := w.cursorIdx - w.start; i >= 0 && i < len(window) {
			window[i] = spliceAt(window[i], w.cursorCol) + echo
		}
	}

	for i := range window {
		window[i] = ansi.Truncate(window[i], width, "")
	}

	if a.searchTerm != "" {
		cursor := a.matchCursor(id)
		for i, line := range window {
			if text := ansi.Strip(line); containsFold(text, a.searchTerm) {
				// The whole line takes the match style, its own colours
				// dropped: a highlight fighting the remote's colours would
				// lose. The line the search cursor stands on takes the louder
				// style, so 3/17 is visible on the screen too.
				style := a.theme.Match
				if w.start+i == cursor {
					style = a.theme.MatchCurrent
				}
				window[i] = style.Render(text)
			}
		}
	}

	return strings.Join(window, "\n")
}

// paneCursor is where the host's own cursor sits inside a pane body of width
// columns and height rows, in cells counted from the body's top-left corner.
//
// ok is false whenever the pane has no cursor to show: an unknown or silent
// host, a session that is not connected, a window scrolled back into history,
// a remote app that hid its cursor, an open auth question answering inline
// (its echo carries its own block), or a position outside the drawn area.
// The caller turns the cell into the frame's terminal cursor; see
// [App.frameCursor].
func (a App) paneCursor(id string, width, height int) (x, y int, ok bool) {
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	t := a.paneTerminal(id)
	if t == nil || a.state(id) != ssh.StateConnected || !t.CursorVisible() {
		return 0, 0, false
	}
	if _, answering := a.inlineAnswerEcho(id); answering {
		return 0, 0, false
	}

	if t.IsAltScreen() {
		// The full-screen app owns the grid, so its cursor is the grid's own
		// cell — no history, no scroll offset to map through.
		x, y = t.CursorPosition()
	} else {
		w, found := a.paneWindowFor(id, height)
		if !found {
			return 0, 0, false
		}
		var live bool
		if x, y, live = w.cursorCell(); !live {
			return 0, 0, false
		}
	}
	if x < 0 || x >= width || y < 0 || y >= height {
		return 0, 0, false
	}
	return x, y, true
}

// spliceAt cuts a rendered line at column x, padding with spaces when the
// line is shorter, so typed text can be appended at the cursor.
func spliceAt(line string, x int) string {
	if w := ansi.StringWidth(line); w <= x {
		return line + strings.Repeat(" ", x-w)
	}
	return ansi.Cut(line, 0, x)
}
