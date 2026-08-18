package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Mouse text selection inside a pane (issue #149). lazycssh owns the mouse,
// so the terminal's native selection cannot reach the pane content; dragging
// the left button over a pane's body highlights the covered text instead.
// Releasing the drag copies it over OSC 52 immediately (copy-on-select,
// issue #302) - the same clipboard path as alt+y - and ctrl+c/super+c copy it
// again on demand while the selection stays live.
//
// The selection is anchored to **screen cells** of the pane's body, clipped
// to that one pane, and remembers the exact view it was made over: the pane's
// body rect, its page, split chunk and scroll offset. Any change to that view
// - a scroll, a page or chunk turn, a retile, the pane closing, focus leaving
// the grid - makes the selection stale and it silently disappears; that is
// the documented answer to "what does scrolling under a selection do": it
// clears. New output under a tail-following pane redraws beneath the
// highlight without moving it, the way a terminal's alternate screen would -
// the reader goroutines are never involved, because the highlight is applied
// at render time from the model alone.
type textSelection struct {
	// active is a pressed-and-not-abandoned selection; dragged says motion
	// arrived since the press, which is what separates a selection from a
	// plain click.
	active  bool
	dragged bool
	// host and index name the pane that owns the drag.
	host  string
	index int
	// body is the pane's body rect at the press; the drag is clamped to it.
	body Rect
	// The view the selection was made over. If any of it changes the
	// selection is stale.
	scroll int
	page   int
	chunk  int
	screen ScreenMode
	// The anchor (press) and head (current) cells, absolute screen
	// coordinates clamped into body.
	startX, startY, x, y int
}

// paneBodyRect is the inner text area of the pane at index as an absolute
// rect: inside the border, below the header. It is the selection's clip
// region and must match the arithmetic of paneExtentAt.
func (a App) paneBodyRect(index int) (Rect, bool) {
	cell, ok := a.paneCell(index)
	if !ok {
		return Rect{}, false
	}
	body := Rect{X: cell.X + 1, Y: cell.Y + 2, Width: cell.Width - 2, Height: cell.Height - 3}
	if body.Width <= 0 || body.Height <= 0 {
		return Rect{}, false
	}
	return body, true
}

// beginTextSelection arms a selection at a press inside a pane body. It is
// not a selection yet - only motion makes it one; a plain click clears any
// previous selection instead.
func (a App) beginTextSelection(index int, x, y int) App {
	body, ok := a.paneBodyRect(index)
	if !ok || !body.contains(x, y) {
		a.textSel = textSelection{}
		return a
	}
	id := a.hostIDAt(index)
	if id == "" {
		a.textSel = textSelection{}
		return a
	}
	a.textSel = textSelection{
		active: true,
		host:   id,
		index:  index,
		body:   body,
		scroll: a.scrollOffset(id),
		page:   a.clampedPage(a.grid()),
		chunk:  a.splitChunk,
		screen: a.screen,
		startX: x, startY: y, x: x, y: y,
	}
	return a
}

// handleMouseMotion extends an armed selection while the left button drags.
// The head is clamped to the owning pane's body: a drag that leaves the pane
// selects up to its edge, never into a neighbour.
func (a App) handleMouseMotion(msg tea.MouseMotionMsg) App {
	if !a.textSel.active || msg.Button != tea.MouseLeft {
		return a
	}
	a.textSel.x = clamp(msg.X, a.textSel.body.X, a.textSel.body.X+a.textSel.body.Width-1)
	a.textSel.y = clamp(msg.Y, a.textSel.body.Y, a.textSel.body.Y+a.textSel.body.Height-1)
	a.textSel.dragged = a.textSel.dragged ||
		a.textSel.x != a.textSel.startX || a.textSel.y != a.textSel.startY
	return a
}

// handleMouseRelease finishes the drag: with motion the selection stays
// visible and is copied to the clipboard immediately (copy-on-select, issue
// #302) - so cmd+v works without cmd+c ever having to reach the app. Without
// motion the press was a plain click and clears instead, same as before.
func (a App) handleMouseRelease(msg tea.MouseReleaseMsg) (App, tea.Cmd) {
	if msg.Button != tea.MouseLeft || !a.textSel.active {
		return a, nil
	}
	if !a.textSel.dragged {
		a.textSel = textSelection{}
		return a, nil
	}
	if !a.textSelectionValid() {
		return a, nil
	}
	host := a.textSel.host
	text, count := a.selectionText()
	return a.reportCopy(host, text, count)
}

// clearTextSelection drops the selection.
func (a App) clearTextSelection() App {
	a.textSel = textSelection{}
	return a
}

// TextSelectionActive reports whether a drag-made selection is currently
// live, which is what decides whether ctrl+c copies or interrupts.
func (a App) TextSelectionActive() bool { return a.textSelectionValid() }

// textSelectionValid reports whether the selection still describes what is on
// screen: same pane under the same index, same body rect, same page, chunk,
// zoom and scroll offset. Anything that changed the view makes it stale.
func (a App) textSelectionValid() bool {
	s := a.textSel
	if !s.active || !s.dragged {
		return false
	}
	if a.focus != AreaGrid && a.focus != AreaBroadcast {
		// The selection lives in the grid; tabbing away from it (and the
		// bar, whose ctrl+c must keep working on it) is a focus change.
		return false
	}
	if a.hostIDAt(s.index) != s.host || a.scrollOffset(s.host) != s.scroll {
		return false
	}
	if a.clampedPage(a.grid()) != s.page || a.splitChunk != s.chunk || a.screen != s.screen {
		return false
	}
	body, ok := a.paneBodyRect(s.index)
	return ok && body == s.body
}

// textSelSpan returns the selected display-column range [lo, hi) on body row
// row (0-based inside the pane body) - stream-shaped, like a terminal: the
// first row runs from the anchor to its end, middle rows are whole, the last
// row ends at the head.
func (s textSelection) textSelSpan(row int) (lo, hi int, ok bool) {
	y1, x1 := s.startY-s.body.Y, s.startX-s.body.X
	y2, x2 := s.y-s.body.Y, s.x-s.body.X
	if y1 > y2 || (y1 == y2 && x1 > x2) {
		y1, x1, y2, x2 = y2, x2, y1, x1
	}
	if row < y1 || row > y2 {
		return 0, 0, false
	}
	lo, hi = 0, s.body.Width
	if row == y1 {
		lo = x1
	}
	if row == y2 {
		hi = x2 + 1
	}
	if lo >= hi {
		return 0, 0, false
	}
	return lo, hi, true
}

// highlightSelection applies the selection style to the rendered body lines
// of the selection's own pane. Widths are preserved cut for cut, so the
// highlight can never shift the layout by a cell.
func (a App) highlightSelection(id string, width int, body string) string {
	if !a.textSelectionValid() || a.textSel.host != id || a.textSel.body.Width != width {
		return body
	}
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		lo, hi, ok := a.textSel.textSelSpan(i)
		if !ok {
			continue
		}
		plain := ansi.Strip(line)
		w := ansi.StringWidth(plain)
		if lo >= w {
			continue
		}
		hi = min(hi, w)
		lines[i] = ansi.Cut(line, 0, lo) +
			a.theme.Selection.Render(ansi.Cut(plain, lo, hi)) +
			ansi.Cut(line, hi, w)
	}
	return strings.Join(lines, "\n")
}

// selectionText extracts the selected text: ANSI stripped, trailing
// whitespace trimmed per line, rows joined with newlines - what a paste
// target wants.
func (a App) selectionText() (string, int) {
	s := a.textSel
	body := a.paneBody(s.host, s.body.Width, s.body.Height)
	lines := strings.Split(body, "\n")
	var out []string
	for i := range lines {
		lo, hi, ok := s.textSelSpan(i)
		if !ok || i >= len(lines) {
			continue
		}
		plain := ansi.Strip(lines[i])
		hi = min(hi, ansi.StringWidth(plain))
		if lo >= hi {
			out = append(out, "")
			continue
		}
		out = append(out, strings.TrimRight(ansi.Cut(plain, lo, hi), " \t"))
	}
	text := strings.Join(out, "\n")
	if strings.TrimSpace(text) == "" {
		return "", 0
	}
	return text, len(out)
}

// copyTextSelection is ctrl+c/super+c with a live selection: the text goes to
// the clipboard over OSC 52, the selection clears, and the status line says
// what happened - a user who expected an interrupt must see why none was
// sent.
func (a App) copyTextSelection() (App, tea.Cmd) {
	host := a.textSel.host
	text, count := a.selectionText()
	a = a.clearTextSelection()
	return a.reportCopy(host, text, count)
}

// reportCopy sets the status line for a selection copy and, if there was
// text, emits the OSC 52 clipboard command. Shared by the ctrl+c/super+c path
// and copy-on-select, so both report the copy the same way.
func (a App) reportCopy(host, text string, count int) (App, tea.Cmd) {
	if text == "" {
		a.lastDelivery = "selection was empty — nothing copied, nothing sent"
		return a, nil
	}
	a.lastDelivery = fmt.Sprintf("copied %d line%s from %s (OSC 52) — no interrupt sent",
		count, plural(count), host)
	return a, a.clipboardCmd(text)
}
