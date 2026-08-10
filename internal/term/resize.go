// Resize semantics on top of the vt emulator, ported from ike's integrated
// terminal. Upstream vt.Resize is a hard truncate: columns beyond the new
// width and rows beyond the new height are simply gone — no reflow, no
// scroll-out. A fleet grid resizes on every host join, leave and window
// change, so that would eat content constantly. Three mechanisms fix it:
//
//   - width change on the primary screen: the whole history is reconstructed
//     as logical lines and replayed through the parser at the new width, as
//     if the terminal had always been that size,
//   - height shrink: rows above the cursor scroll into the scrollback (what a
//     real terminal does) instead of the bottom rows being truncated,
//   - height grow: the rows a shrink pushed come back, and a reserve of the
//     fullest known row contents restores cells a plain truncate clipped.
//
// The alt screen never reflows: its apps repaint themselves on the window
// change the session forwards.

package term

import (
	"fmt"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Resize changes the emulator's screen size. It is called in lockstep with
// the pane geometry and the remote PTY, so the emulated app and the real one
// agree on the window.
func (e *Emulator) Resize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	e.gridMu.Lock()
	defer e.gridMu.Unlock()
	// A height shrink pushes rows into the retention; keep the cap exact.
	defer e.trimLocked()

	oldW, oldH := e.vt.Width(), e.vt.Height()
	if width == oldW && height == oldH {
		return
	}
	// A width reflow rewrites the vt working depth in place, and a height
	// change moves rows between screen and history; retained indices from
	// before this resize point at different lines after it.
	e.histGen++

	// Drain first: the reflow below is O(vt scrollback), and resize storms
	// (host joins on a busy fleet) must not pile cell lines past the working
	// depth. Compact lines stay frozen at the width they scrolled off with —
	// re-materializing cells here would resurrect the retention cost.
	e.drainLocked(false)

	if width != oldW && !e.vt.IsAltScreen() {
		lines, tail := e.logicalLinesLocked(oldW, oldH)
		e.vt.Resize(width, height)
		e.replayLocked(lines, tail, width)
		// The replay rewrote everything at the new width; stale reserve rows
		// would only poison later prefix matches.
		e.reserve, e.reserveW = nil, 0
		e.shrinkPushed, e.shrinkMark = 0, 0
		return
	}

	// The height-restore guard compares BEFORE the snapshot folds the current
	// screen into the reserve — afterwards the overlap matches trivially and
	// stale reserve rows beyond oldH would resurrect over newer content.
	heightMatch := e.reserveMatchesLocked(min(oldW, width), min(oldH, height))
	e.snapshotReserveLocked(oldW, oldH)
	if height < oldH {
		e.scrollShrinkLocked(oldW, oldH, height)
	}
	e.vt.Resize(width, height)
	if height > oldH {
		e.pullShrinkLocked(oldH, width, height)
	}
	e.restoreReserveLocked(oldW, oldH, width, height, heightMatch)
}

// snapshotReserveLocked folds the current screen into the reserve: a row
// whose visible cells still prefix-match its reserve row keeps the longer
// reserved content, anything else is replaced by what is on screen now.
// Rows beyond the current height are kept for a later height grow.
func (e *Emulator) snapshotReserveLocked(w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	if len(e.reserve) < h {
		e.reserve = append(e.reserve, make([]uv.Line, h-len(e.reserve))...)
	}
	for y := 0; y < h; y++ {
		row := e.screenRowLocked(w, y)
		if !rowPrefixEqual(e.reserve[y], row, w) {
			e.reserve[y] = row
		}
	}
	if w > e.reserveW {
		e.reserveW = w
	}
}

// reserveMatchesLocked reports whether every screen row up to h still
// prefix-matches its reserve row over the first w columns. Must run before
// snapshotReserveLocked in the same apply — the snapshot syncs the overlap,
// after which the comparison is vacuously true.
func (e *Emulator) reserveMatchesLocked(w, h int) bool {
	if len(e.reserve) == 0 {
		return false
	}
	for y := 0; y < h && y < len(e.reserve); y++ {
		if !rowPrefixEqual(e.reserve[y], e.screenRowLocked(w, y), w) {
			return false
		}
	}
	return true
}

// restoreReserveLocked writes reserved cells back after a grow. Width: each
// row that still prefix-matches its reserve row gets the clipped columns
// back. Height: the rows a shrink dropped come back only when the whole
// pre-snapshot overlap matched (heightMatch) — content that scrolled or was
// rewritten meanwhile shifts row indexes, and restoring then would resurrect
// stale lines over newer content.
func (e *Emulator) restoreReserveLocked(oldW, oldH, w, h int, heightMatch bool) {
	if len(e.reserve) == 0 {
		return
	}
	overlap := min(min(oldH, h), len(e.reserve))
	for y := 0; y < overlap; y++ {
		cur := e.screenRowLocked(min(oldW, w), y)
		if !rowPrefixEqual(e.reserve[y], cur, min(oldW, w)) {
			continue
		}
		if w > oldW { // width grow: fill the clipped columns
			for x := oldW; x < w && x < len(e.reserve[y]); x++ {
				c := e.reserve[y][x]
				e.vt.SetCell(x, y, &c)
			}
		}
	}
	if h > oldH && heightMatch { // height grow: bring the dropped rows back
		for y := oldH; y < h && y < len(e.reserve); y++ {
			for x := 0; x < w && x < len(e.reserve[y]); x++ {
				c := e.reserve[y][x]
				e.vt.SetCell(x, y, &c)
			}
		}
	}
}

// scrollShrinkLocked applies real-terminal height-shrink semantics before the
// emulator resize truncates: when the cursor would fall below the new height,
// the top rows scroll into the scrollback and the screen slides up so the
// cursor line (the prompt, the newest output) survives. Runs before
// vt.Resize; the subsequent Resize clamps the emulator cursor to h-1, which
// is exactly where the slide put its line. The alt screen has no scrollback
// and its apps redraw on the window change — skip.
func (e *Emulator) scrollShrinkLocked(w, oldH, h int) {
	if e.vt.IsAltScreen() {
		return
	}
	sb := e.vt.Scrollback()
	if sb == nil || sb.MaxLines() <= 0 {
		return
	}
	cy := e.vt.CursorPosition().Y
	shift := cy - (h - 1)
	if shift <= 0 {
		return
	}
	if shift > oldH {
		shift = oldH
	}
	for y := 0; y < shift; y++ {
		sb.Push(e.screenRowLocked(w, y))
	}
	for y := 0; y < oldH; y++ {
		src := y + shift
		for x := 0; x < w; x++ {
			c := uv.EmptyCell
			if src < oldH {
				if cell := e.vt.CellAt(x, src); cell != nil {
					c = *cell
				}
			}
			e.vt.SetCell(x, y, &c)
		}
	}
	e.shrinkPushed += shift
	e.shrinkMark = sb.Len()
}

// pullShrinkLocked reverses scrollShrinkLocked after a height grow: the rows
// the shrink pushed come back out of the scrollback onto the top of the
// screen, the on-screen content slides down and the cursor follows via an
// injected CUP. Only this emulator's own pushes are pulled, and only while
// they are still the newest scrollback lines — output that scrolled meanwhile
// buried them, so the pull is abandoned (the screen already reflects the
// newer content). Runs after vt.Resize.
func (e *Emulator) pullShrinkLocked(oldH, w, h int) {
	if e.shrinkPushed <= 0 || e.vt.IsAltScreen() {
		return
	}
	sb := e.vt.Scrollback()
	if sb == nil {
		return
	}
	if sb.Len() != e.shrinkMark {
		e.shrinkPushed = 0
		return
	}
	pull := min(e.shrinkPushed, h-oldH, sb.Len())
	if pull <= 0 {
		return
	}
	// Pop the newest pull lines: the scrollback API only pushes, so the kept
	// prefix is re-pushed after a clear. Line headers are copied first —
	// Push clones into the same backing array Clear truncated.
	all := sb.Lines()
	popped := make([]uv.Line, pull)
	copy(popped, all[len(all)-pull:])
	keep := make([]uv.Line, len(all)-pull)
	copy(keep, all[:len(all)-pull])
	sb.Clear()
	for _, l := range keep {
		sb.Push(l)
	}
	// Slide the screen down (bottom-up: every write lands above-read rows),
	// then lay the popped rows back on top, oldest first.
	for y := oldH - 1; y >= 0; y-- {
		row := e.screenRowLocked(w, y)
		for x := 0; x < w; x++ {
			c := row[x]
			e.vt.SetCell(x, y+pull, &c)
		}
	}
	for y := 0; y < pull; y++ {
		for x := 0; x < w; x++ {
			c := uv.EmptyCell
			if x < len(popped[y]) {
				c = popped[y][x]
			}
			e.vt.SetCell(x, y, &c)
		}
	}
	// The cursor rides the slide. Injected as CUP through the emulator's
	// input path — the only cursor mutator the safe wrapper exposes.
	pos := e.vt.CursorPosition()
	_, _ = e.vt.Write(fmt.Appendf(nil, "\x1b[%d;%dH", pos.Y+pull+1, pos.X+1))
	e.shrinkPushed -= pull
	e.shrinkMark = sb.Len()
}

// screenRowLocked reads the first n cells of screen row y. Every cell is
// copied: CellAt returns a pointer into the live buffer.
func (e *Emulator) screenRowLocked(n, y int) uv.Line {
	row := make(uv.Line, n)
	for x := 0; x < n; x++ {
		if c := e.vt.CellAt(x, y); c != nil {
			row[x] = *c
		} else {
			row[x] = uv.EmptyCell
		}
	}
	return row
}

// rowPrefixEqual reports whether the first n cells of a and b hold the same
// content. Style differences are ignored — the guard only needs to know the
// text is still the text the reserve captured.
func rowPrefixEqual(a, b uv.Line, n int) bool {
	if len(a) < n || len(b) < n {
		return false
	}
	for i := 0; i < n; i++ {
		if a[i].Content != b[i].Content {
			return false
		}
	}
	return true
}

// softWrappedLocked reports whether virtual line v — an index into
// [scrollback ++ screen] — continues into v+1 because the terminal ran out of
// columns, rather than because the program printed a newline. The emulator
// keeps no per-row wrap metadata, so this is the heuristic every terminal
// without shell integration uses: a row whose final column is occupied
// wrapped into the next one. Lines stored wider than the viewport, or screen
// rows still prefix-matching a wider resize reserve, are clips — never wraps.
func (e *Emulator) softWrappedLocked(v int) bool {
	sb := e.vt.ScrollbackLen()
	w := e.vt.Width()
	if w <= 0 || v < 0 || v >= sb+e.vt.Height()-1 {
		return false // the last virtual line has nothing to continue into
	}
	if v < sb {
		// Stored wider than the viewport: a clipped long line, not a wrap.
		if e.vt.ScrollbackCellAt(w, v) != nil {
			return false
		}
		return cellOccupied(e.vt.ScrollbackCellAt(w-1, v))
	}
	y := v - sb
	if !cellOccupied(e.vt.CellAt(w-1, y)) {
		return false
	}
	// Full row: a prefix-matching reserve row wider than the viewport means
	// the row was written on a wider grid and merely got cut (or ended
	// exactly at w) — not a wrap. Rows rewritten since fail the prefix match
	// and take the plain heuristic.
	if e.reserveW > w && y < len(e.reserve) && len(e.reserve[y]) > w {
		if rowPrefixEqual(e.reserve[y], e.screenRowLocked(w, y), w) {
			return false
		}
	}
	return true
}

// logicalLinesLocked reconstructs the logical lines of [scrollback ++ screen]
// at the current (pre-resize) width. The reflow cache is consulted first: it
// holds the logical lines the last replay wrote, so hard breaks in the
// matched prefix are known — the exact-width heuristic ambiguity cannot
// misjoin them, and repeated shrink/grow cycles stay lossless. Rows the cache
// does not cover (output since the last replay) fall back to the soft-wrap
// heuristic. Rows below both the cursor and the last content row are dropped
// — the cursor's own (possibly blank) row survives, so the replay puts the
// cursor back where the shell left it.
func (e *Emulator) logicalLinesLocked(w, h int) (lines, tail []uv.Line) {
	sb := e.vt.ScrollbackLen()
	last := e.vt.CursorPosition().Y
	for y := h - 1; y > last; y-- {
		if len(trimTrailingBlank(e.screenRowLocked(w, y))) > 0 {
			last = y
			break
		}
	}
	total := sb + last + 1

	// The tail is the last logical content line (and anything below it): the
	// shell's live edit line. It is NOT reflowed — its physical rows replay
	// verbatim (clipped on shrink) so an interactive shell's own redraw finds
	// the row geometry it remembers and repaints cleanly, instead of walking
	// up over relaid-out history rows. It anchors on the last content row,
	// NOT the cursor: a resize can catch the shell mid-redraw with the cursor
	// parked high in the grid, and trusting it would clip whole history rows
	// as "tail".
	tailStart := total - 1
	if tailStart < 0 {
		tailStart = 0
	}
	for tailStart > 0 && e.softWrappedLocked(tailStart-1) {
		tailStart--
	}

	rows := make([]uv.Line, 0, total)
	for v := 0; v < total; v++ {
		var row uv.Line
		if v < sb {
			for x := 0; ; x++ {
				c := e.vt.ScrollbackCellAt(x, v)
				if c == nil {
					break
				}
				row = append(row, *c)
			}
		} else {
			row = e.screenRowLocked(w, v-sb)
		}
		rows = append(rows, row)
	}

	var consumed int
	lines, consumed = reconcileCache(e.reflowCache, rows[:tailStart], w)

	var pending uv.Line
	for v := consumed; v < tailStart; v++ {
		wrapped := v < tailStart-1 && e.softWrappedLocked(v)
		if wrapped {
			// A wrapped row is full by definition; keep it verbatim so the
			// continuation glues seamlessly.
			pending = append(pending, rows[v]...)
			continue
		}
		pending = append(pending, trimTrailingBlank(rows[v])...)
		lines = append(lines, trimTrailingBlank(pending))
		pending = nil
	}
	for v := tailStart; v < total; v++ {
		tail = append(tail, trimTrailingBlank(rows[v]))
	}
	return lines, tail
}

// reconcileCache consumes grid rows from the top while they still are the
// rewrap of the cached logical lines at width w, returning those lines
// verbatim — their hard breaks are authoritative. The first mismatching line
// (rewritten or new content) stops the walk; when even the first cached line
// no longer matches (e.g. the scrollback cap trimmed it), whole leading cache
// lines are skipped until one aligns with row 0 again.
func reconcileCache(cache []uv.Line, rows []uv.Line, w int) (lines []uv.Line, consumed int) {
	for skip := 0; skip < len(cache); skip++ {
		r := 0
		for _, cl := range cache[skip:] {
			seg := rewrapLine(cl, w)
			if r+len(seg) > len(rows) || !segMatches(seg, rows[r:r+len(seg)]) {
				break
			}
			lines = append(lines, cl)
			r += len(seg)
		}
		if r > 0 {
			return lines, r
		}
	}
	return nil, 0
}

// rewrapLine chunks a logical line into the physical rows the emulator would
// produce at width w: display cells accumulate up to w, zero-width
// continuation cells stay with their head, and a wide cell that would
// straddle the edge moves to the next row whole. An empty line is one empty
// row.
func rewrapLine(l uv.Line, w int) []uv.Line {
	if w <= 0 || len(l) == 0 {
		return []uv.Line{nil}
	}
	var segs []uv.Line
	var cur uv.Line
	used := 0
	for i := 0; i < len(l); i++ {
		cw := l[i].Width
		if cw > 0 && used+cw > w {
			segs = append(segs, cur)
			cur, used = nil, 0
		}
		cur = append(cur, l[i])
		used += cw
	}
	return append(segs, cur)
}

// segMatches reports whether the expected rewrap rows equal the actual grid
// rows by trimmed cell content (styles may drift through render/re-parse and
// do not affect wrap structure).
func segMatches(expected, actual []uv.Line) bool {
	for i := range expected {
		e, a := trimTrailingBlank(expected[i]), trimTrailingBlank(actual[i])
		if len(e) != len(a) {
			return false
		}
		for x := range e {
			if e[x].Content != a[x].Content {
				return false
			}
		}
	}
	return true
}

// replayLocked rewrites the emulator's content from scratch: clear screen
// (2J first — it pushes the stale rows into the scrollback — then 3J to wipe
// that scrollback wholesale), home, then every logical line hard-newline
// separated with no trailing newline (SGR preserved via uv.Line.Render). The
// emulator re-wraps each line at the current width itself, so wrap state,
// cursor and scrollback come out exactly as if the terminal had always been
// this size. The written lines become the next reflow cache.
func (e *Emulator) replayLocked(lines, tail []uv.Line, w int) {
	var b strings.Builder
	b.WriteString("\x1b[0m\x1b[2J\x1b[3J\x1b[H")
	for i, l := range lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		b.WriteString(l.Render())
	}
	// The tail (the shell's live edit line) keeps its physical rows verbatim,
	// clipped to the new width: an interactive shell repaints it on the
	// window change and must find the geometry it remembers.
	for i, row := range tail {
		if i > 0 || len(lines) > 0 {
			b.WriteString("\r\n")
		}
		if segs := rewrapLine(row, w); len(segs) > 0 {
			b.WriteString(uv.Line(segs[0]).Render())
		}
	}
	_, _ = e.vt.Write([]byte(b.String()))
	// Only the reflowed prefix is cacheable — the tail belongs to the shell.
	e.reflowCache = lines
}

// trimTrailingBlank drops the trailing run of blank cells (the same
// definition the scrollback push uses), keeping styled spaces.
func trimTrailingBlank(l uv.Line) uv.Line {
	end := len(l)
	for end > 0 {
		c := &l[end-1]
		if !c.IsZero() && !c.Equal(&uv.EmptyCell) {
			break
		}
		end--
	}
	return l[:end]
}

// cellOccupied reports whether a cell holds visible content — the soft-wrap
// heuristic's "did the line reach the final column" test. Width-0
// continuation cells count: a wide rune reaches the edge.
func cellOccupied(c *uv.Cell) bool {
	if c == nil {
		return false
	}
	if c.Width == 0 {
		return true
	}
	return c.Content != "" && c.Content != " "
}
