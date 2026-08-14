package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// The simulated remote cursor (issue #301).
//
// A broadcast goes to many hosts at once, but a frame has exactly one terminal
// cursor and the focused pane owns it (issue #292). Everything the other
// targets are doing with that keystroke — a shell that echoed it, one that is
// still at a password prompt, one whose line is three characters shorter
// because its history differs — was invisible.
//
// So every pane in the broadcast set that does *not* own the caret paints its
// host's cursor position itself, as a styled cell in the rendered body. It is
// a mark, not a caret: it does not blink, it does not move the terminal's
// cursor, and it says only "this is where the next character lands on this
// host". Panes outside the set draw nothing, because a mark there would claim
// a keystroke goes somewhere it does not — the same rule the real caret obeys.
//
// The mark is part of [paneKey], so the cross-frame render cache (#293) redraws
// a pane whose cursor moved, and only such a pane.

// cursorMark is the cell a pane paints its simulated remote cursor on, in the
// body's own coordinates. Comparable, so it can live in [paneKey].
type cursorMark struct {
	x, y int
	on   bool
}

// remoteCursorMark is the mark a pane draws, off when it draws none.
func (a App) remoteCursorMark(id string, width, height int) cursorMark {
	if !a.paintsRemoteCursor(id) {
		return cursorMark{}
	}
	// paneCursor is the same placement the real caret uses, with the same
	// refusals: a host that is not connected, a pane scrolled back into
	// history, a remote app that hid its cursor, an inline auth answer that
	// carries its own block, a cell outside the drawn area.
	x, y, ok := a.paneCursor(id, width, height)
	return cursorMark{x: x, y: y, on: ok}
}

// paintsRemoteCursor reports whether a pane is a broadcast target that has to
// draw its own cursor: in the set, and not the pane holding the frame's real
// caret.
func (a App) paintsRemoteCursor(id string) bool {
	if id == "" || a.cfg.Targets == nil {
		return false
	}
	if a.cfg.Targets.Mode() == broadcast.ModeSingle {
		// Single mode types to the focused pane alone, which has the real
		// caret. Nothing else is a target, so nothing else marks one.
		return false
	}
	if a.focus == AreaGrid && id == a.FocusedHost() {
		return false
	}
	_, ok := a.broadcastTargets()[id]
	return ok
}

// broadcastTargets is the target set as a lookup, memoized for the frame: the
// router answers with a fresh slice, and asking it once per pane per frame is
// O(fleet²) on a large run (issue #291).
func (a App) broadcastTargets() map[string]struct{} {
	if a.memo != nil && a.memo.targets != nil {
		return a.memo.targets
	}
	var ids []string
	if a.cfg.Targets != nil {
		ids = a.cfg.Targets.Targets()
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	if a.memo != nil {
		a.memo.targets = set
	}
	return set
}

// paintRemoteCursor draws the mark into a rendered pane body. Widths are
// preserved cut for cut, so the mark can never shift the layout by a cell; a
// cursor past the end of its line is padded to, which is where a prompt's
// cursor usually sits.
func (a App) paintRemoteCursor(body string, m cursorMark) string {
	if !m.on || body == "" {
		return body
	}
	lines := strings.Split(body, "\n")
	if m.y < 0 || m.y >= len(lines) {
		return body
	}
	lines[m.y] = markCell(lines[m.y], m.x, a.theme.RemoteCursor)
	return strings.Join(lines, "\n")
}

// markCell applies a style to the single cell at display column x of a
// rendered line, which may already carry the remote's own escape sequences —
// indexing the styled string by column would cut them in half.
func markCell(line string, x int, style lipgloss.Style) string {
	if x < 0 {
		return line
	}
	plain := ansi.Strip(line)
	w := ansi.StringWidth(plain)
	if x >= w {
		return line + strings.Repeat(" ", x-w) + style.Render(" ")
	}
	return ansi.Cut(line, 0, x) +
		style.Render(ansi.Cut(plain, x, x+1)) +
		ansi.Cut(line, x+1, w)
}
