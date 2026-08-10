// Compact scrollback retention (issue #277). The vt scrollback stores one
// styled cell (~128 B) per rune, which put the 10k-line retention cap at
// ~75 MB per host. But a line that has scrolled off the screen is only ever
// read back as styled text — HistoryLine renders and discards — so keeping
// the cells is pure waste.
//
// The emulator therefore keeps only a small working depth of real cell lines
// in the vt scrollback: enough for the resize reflow and the height-shrink
// restore, which are the only readers that need cells. Everything older is
// drained into a bounded ring of pre-rendered styled strings — one string per
// line, the exact string HistoryLine used to build on every call. Draining
// happens on the write path (and on resize), in batches, so the vt scrollback
// never reaches its own cap and its O(lines) oldest-line eviction never runs.
//
// The compact lines are frozen at the width they scrolled off with: a later
// width reflow reflows the vt working depth only. The pane clips long lines
// at render time, so a frozen wide line cannot break the layout — this is the
// deliberate trade against re-materializing cells on resize, which would
// resurrect the cost the drain removed.
package term

import uv "github.com/charmbracelet/ultraviolet"

const (
	// compactWorkingDepth is how many scrolled-off lines stay in the vt
	// scrollback as full cell grids — the window that still reflows on a
	// width resize and feeds the height-shrink restore.
	compactWorkingDepth = 128

	// compactDrainBatch is the drain hysteresis: overflow accumulates to a
	// full batch before it moves, so each drain re-pushes the working depth
	// once per batch instead of once per line. The cap stays exact anyway:
	// trimLocked evicts compact lines — an O(1) string drop each — at every
	// write boundary.
	compactDrainBatch = 128

	// compactFeedChunk bounds the bytes handed to the vt parser between
	// drain checks. Worst case one byte scrolls one line, so it also caps
	// how many lines can pile into the vt scrollback inside a single write.
	compactFeedChunk = 4096
)

// stringRing is a bounded FIFO of rendered history lines: the compact
// retention. One styled string per line instead of one styled cell per rune.
type stringRing struct {
	buf  []string
	head int // index of the oldest line; 0 until the ring first fills
	n    int
	max  int
	// dropped counts every line ever evicted, monotonically: with the ring
	// active it is the absolute stream index of the oldest retained line,
	// which is what lets an incremental reader (the UI's search match cache,
	// issue #278) shift its state instead of rescanning.
	dropped uint64
}

func (r *stringRing) len() int { return r.n }

// at returns line i, 0 = oldest. The caller keeps i within range.
func (r *stringRing) at(i int) string {
	return r.buf[(r.head+i)%len(r.buf)]
}

// push appends a line, evicting the oldest once max is reached. A max of
// zero drops everything: the whole retention then fits the working depth.
func (r *stringRing) push(s string) {
	if r.max <= 0 {
		return
	}
	if r.n == r.max {
		r.evictOldest()
	}
	// The buffer grows on demand. Until it wraps, the lines sit contiguous
	// from head, so the tail is exactly len(buf) and append extends it.
	if r.head+r.n == len(r.buf) && len(r.buf) < r.max {
		r.buf = append(r.buf, s)
		r.n++
		return
	}
	r.buf[(r.head+r.n)%len(r.buf)] = s
	r.n++
}

// evictOldest drops the oldest line: one string reference, O(1) — which is
// what lets the cap be enforced exactly without extra cell copies.
func (r *stringRing) evictOldest() {
	if r.n == 0 {
		return
	}
	r.buf[r.head] = "" // release the string to the GC
	r.dropped++
	r.head++
	if r.head == len(r.buf) {
		r.head = 0
	}
	r.n--
}

// setMax rebounds the ring, dropping the oldest lines that no longer fit.
func (r *stringRing) setMax(m int) {
	if m < 0 {
		m = 0
	}
	if m == r.max {
		return
	}
	drop := max(r.n-m, 0)
	r.dropped += uint64(drop)
	buf := make([]string, 0, r.n-drop)
	for i := drop; i < r.n; i++ {
		buf = append(buf, r.at(i))
	}
	r.buf, r.head, r.n, r.max = buf, 0, len(buf), m
}

// setRetentionLocked applies a total retention cap of n lines: a working
// depth of cell lines in the vt scrollback, the rest as compact strings. The
// vt scrollback gets headroom above the working depth — one drain batch plus
// the worst-case lines of one feed chunk — so it never evicts on its own;
// eviction order is the ring's job, oldest first. Caps at or below the
// working depth disable the ring entirely and behave like the plain vt
// scrollback. The caller holds gridMu.
func (e *Emulator) setRetentionLocked(n int) {
	// A retention change can move lines between the vt scrollback and the
	// ring and drop across both; incremental readers start over.
	e.histGen++
	e.capTotal = n
	e.vtDepth = min(compactWorkingDepth, n)
	e.hist.setMax(n - e.vtDepth)
	e.drainLocked(true)
	vtMax := n
	if n > e.vtDepth {
		vtMax = e.vtDepth + compactDrainBatch + compactFeedChunk
	}
	e.vt.SetScrollbackSize(vtMax)
}

// feedLocked hands bytes to the vt parser in bounded chunks, draining the
// scrollback overflow between chunks so the cell-grid retention stays near
// the working depth even inside one huge write. The parser is a streaming
// state machine; chunk boundaries can fall anywhere. The caller holds gridMu.
func (e *Emulator) feedLocked(p []byte) {
	for len(p) > 0 {
		n := min(len(p), compactFeedChunk)
		_, _ = e.vt.Write(p[:n])
		p = p[n:]
		e.drainLocked(false)
	}
}

// drainLocked moves scrolled-off lines beyond the working depth out of the
// vt scrollback into the compact ring, oldest first, each rendered once to
// the styled string HistoryLine would have produced. force skips the batch
// hysteresis; SetHistorySize uses it to make the new cap hold immediately.
// The caller holds gridMu.
func (e *Emulator) drainLocked(force bool) {
	sb := e.vt.Scrollback()
	over := sb.Len() - e.vtDepth
	// The batch shrinks with a tight cap so the vt working overflow alone can
	// never exceed the whole retention cap between drains.
	batch := min(compactDrainBatch, e.capTotal-e.vtDepth)
	if over <= 0 || (!force && over < batch) {
		return
	}
	lines := sb.Lines()
	for _, l := range lines[:over] {
		e.hist.push(l.Render())
	}
	// The scrollback API only pushes, so the kept suffix goes back via
	// Clear + Push. Line headers are copied first — Push clones into the
	// same backing array Clear truncated.
	keep := make([]uv.Line, len(lines)-over)
	copy(keep, lines[over:])
	sb.Clear()
	for _, l := range keep {
		sb.Push(l)
	}
	// The drain moved or buried anything a height shrink pushed; a later
	// height grow must not pull rows that are no longer the newest.
	e.shrinkPushed = 0
}

// trimLocked evicts the oldest compact lines until the whole retention —
// compact ring plus vt scrollback — fits the cap again. It runs at every
// write and resize boundary; eviction is an O(1) string drop, so exactness
// costs nothing. The vt overflow between drains stays below the cap by the
// batch bound above, so evicting ring lines alone always suffices.
func (e *Emulator) trimLocked() {
	sbLen := e.vt.ScrollbackLen()
	for e.hist.len() > 0 && e.hist.len()+sbLen > e.capTotal {
		e.hist.evictOldest()
	}
}

// histLenLocked is the retained-history line count: compact lines plus what
// still sits in the vt scrollback. The caller holds gridMu.
func (e *Emulator) histLenLocked() int {
	return e.hist.len() + e.vt.ScrollbackLen()
}

// histLineLocked renders retained history line i (0 = oldest) as styled
// text; out-of-range indexes return the empty string. Compact lines are
// returned as stored — rendering happened once, at drain time. The caller
// holds gridMu.
func (e *Emulator) histLineLocked(i int) string {
	if i < 0 {
		return ""
	}
	if i < e.hist.len() {
		return e.hist.at(i)
	}
	line := e.vt.Scrollback().Line(i - e.hist.len())
	if line == nil {
		return ""
	}
	return line.Render()
}
