package ui

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

// The search match cache (issue #278). While a term is live, every frame's
// status bar asks for the focused pane's match lines, and every n/N press
// walks them again. Computing them from scratch materializes the pane's whole
// virtual line space — milliseconds and megabytes per frame at the retention
// cap, the last full-scrollback walk the #274 audit left on the render path.
//
// So the history's matches are found once per term and then only maintained.
// History changes in exactly two cheap-to-follow ways — new lines append, the
// cap drops from the front — and the emulator's [term.HistoryCursor] says how
// much of each happened, plus a generation that changes whenever retained
// lines were instead rewritten in place (resize reflow, retention change).
// The cache validates itself against that cursor on every read, so there is
// no invalidation hook to forget: whatever moved the history moves a cursor
// field, and a mismatch falls back to a rescan.
//
// Only the history portion is cached. The screen — at most a pane's height in
// rows — mutates in place with every output byte and is rescanned per call by
// [App.matchLines]; the dropped-output marker is a constant checked there too.
//
// The cache hangs off the model behind a pointer shared by every value copy:
// it is a memo of a pure question about the emulator's state, not model
// state, so filling it lazily wherever matchLines runs is safe — Update and
// View share one goroutine — and a copy observing a newer fill only skips
// work it would have redone. Match *cursor* state (matchAt, searchAnchor)
// stays in the model proper.
type searchCache struct {
	// term is the search term the cached matches belong to; any other term
	// starts the map over.
	term  string
	hosts map[string]*hostMatches
}

// hostMatches is one host's incremental scan state.
type hostMatches struct {
	// gen is the [term.HistoryCursor] generation the scan ran under.
	gen uint64
	// start mirrors the cursor's Start at the last read: hits below a newer
	// Start were dropped by the cap and go with it.
	start uint64
	// scanned is one past the last absolute history index scanned.
	scanned uint64
	// hits are the absolute history indices whose line contains the term.
	hits []uint64
}

// dropSearchCache empties the cache; clearing the term makes the next one
// start fresh instead of holding the old term's hits until they age out.
func (a App) dropSearchCache() App {
	if a.search != nil {
		a.search.term, a.search.hosts = "", nil
	}
	return a
}

// histMatches is the history portion of a host's search matches: the indices
// (0 = oldest retained line) whose text contains the live term, served from
// the cache when the emulator's history cursor vouches for it. histLen is the
// caller's measured history length — the coordinate space its virtual lines
// use — so no index at or past it is ever returned, even when the emulator
// has grown since the measure.
func (a App) histMatches(id string, histLen int) []int {
	t := a.paneTerminal(id)
	if t == nil || histLen <= 0 {
		return nil
	}
	cur := t.HistoryCursor()
	histLen = min(histLen, cur.Len)
	if histLen <= 0 {
		return nil
	}
	if a.search == nil || !cur.Exact {
		// No cache to keep, or drops the emulator cannot count (retention
		// within the compact working depth — a few dozen lines): scan.
		return scanHistory(t, a.searchTerm, 0, histLen)
	}
	if a.search.term != a.searchTerm {
		a.search.term = a.searchTerm
		a.search.hosts = make(map[string]*hostMatches)
	}
	h := a.search.hosts[id]
	if h == nil || h.gen != cur.Gen ||
		h.start > cur.Start || h.scanned < cur.Start ||
		h.scanned > cur.Start+uint64(cur.Len) {
		// Unknown host, rewritten history, or a cursor this state cannot be
		// shifted onto: start this host over.
		h = &hostMatches{gen: cur.Gen, start: cur.Start, scanned: cur.Start}
		a.search.hosts[id] = h
	}
	if h.start < cur.Start {
		// The cap dropped lines from the front; their hits go with them.
		keep := h.hits[:0]
		for _, abs := range h.hits {
			if abs >= cur.Start {
				keep = append(keep, abs)
			}
		}
		h.hits = keep
		h.start = cur.Start
	}
	if end := cur.Start + uint64(cur.Len); h.scanned < end {
		// Only what appended since the last look — the per-frame cost.
		for _, i := range scanHistory(t, a.searchTerm, int(h.scanned-cur.Start), cur.Len) {
			h.hits = append(h.hits, cur.Start+uint64(i))
		}
		h.scanned = end
	}
	out := make([]int, 0, len(h.hits))
	for _, abs := range h.hits {
		if i := int(abs - cur.Start); i < histLen {
			out = append(out, i)
		}
	}
	return out
}

// scanHistory finds the term in a host's retained history lines [from, to),
// returning their indices, oldest first.
func scanHistory(t *term.Emulator, needle string, from, to int) []int {
	var out []int
	for i := from; i < to; i++ {
		if containsFold(ansi.Strip(t.HistoryLine(i)), needle) {
			out = append(out, i)
		}
	}
	return out
}
