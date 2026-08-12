package ui

import (
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/term"
)

// The pane frame cache (issue #291). The frame memo (framememo.go) lives one
// View call, so every frame used to re-render every visible pane — measure,
// style, border, pad — even though a keystroke into one host changes exactly
// one pane. At fleet scale that invariant work was the whole keystroke-to-echo
// latency: ~two thirds of a frame's cost and most of its megabyte of garbage
// went to panes whose content had not moved.
//
// So a pane's finished frame — the [App.renderPane] string, border and all —
// is kept across frames and reused while everything that fed it is provably
// unchanged. The key holds every input the render reads: the cell, the focus
// and selection styling, the theme, the snapshot state the header shows, the
// scroll offset, the live search, and the emulator's [term.ChangeMark], which
// moves on every write, resize and retention change. There is no invalidation
// hook to forget: whatever changes a render input changes the key, and a
// mismatch re-renders.
//
// Like the search cache, it hangs off the model behind a pointer shared by
// every value copy: it is a memo of a pure question — "what does this pane
// look like" — not model state, and Update and View share one goroutine. A
// nil cache is legal and means every pane renders honestly, which is what the
// equivalence test relies on.
type paneFrameCache struct {
	frames map[string]paneFrame
}

// paneFrame is one cached render and the key it was rendered under.
type paneFrame struct {
	key   paneFrameKey
	frame string
}

// paneFrameKey is every model input [App.renderPane] reads for one pane. Two
// equal keys render two equal frames; the fields are comparable so the check
// is one ==.
type paneFrameKey struct {
	// number is the pane's position in the host list, which the header shows
	// and which moves when hosts join, leave or are filtered.
	number int
	// cell is where and how big the pane is drawn.
	cell Rect
	// focused and selected pick the header and border styles.
	focused  bool
	selected bool
	// theme is the palette by identity; a background change replaces it.
	theme *Theme
	// state, exit, exitSeq, cmdMark and cmdMarked feed the header's state and
	// exit labels and the border's failure colour, all from the fleet
	// snapshot and the last send's marks.
	state     ssh.State
	exit      int
	exitSeq   uint64
	cmdMark   uint64
	cmdMarked bool
	// change is the emulator's content mark: writes, resizes and retention
	// changes all move it.
	change term.ChangeMark
	// scroll is the pane's scrollback offset.
	scroll int
	// searchTerm and matchLine are the live search as this pane renders it.
	searchTerm string
	matchLine  int
}

// paneFrameKey builds the cache key for one pane, and reports whether the
// pane may be cached at all. Two states opt out because their render reads
// input the key cannot cheaply carry: an open auth question (its typed answer
// echoes into the body) and a live text selection on this pane (the drag
// coordinates restyle the body per mouse event). Both are rare, transient and
// user-driven — rendering them honestly costs one pane, not the fleet.
func (a App) paneFrameKey(host int, id string, cell Rect, gridFocused bool) (paneFrameKey, bool) {
	if a.paneFrames == nil || a.paneFrames.frames == nil {
		return paneFrameKey{}, false
	}
	if a.authFor(id) != nil {
		return paneFrameKey{}, false
	}
	if a.textSel.active && a.textSel.host == id {
		return paneFrameKey{}, false
	}
	key := paneFrameKey{
		number:     host,
		cell:       cell,
		focused:    gridFocused && host == a.paneIndex,
		selected:   host == a.paneIndex,
		theme:      a.theme,
		scroll:     a.scrollOffset(id),
		searchTerm: a.searchTerm,
		matchLine:  a.matchCursor(id),
	}
	st, ok := a.hostStates[id]
	if ok {
		key.state, key.exit, key.exitSeq = st.state, st.exit, st.exitSeq
	} else {
		key.state = ssh.StatePending
	}
	key.cmdMark, key.cmdMarked = a.cmdExitMarks[id]
	if t := a.paneTerminal(id); t != nil {
		// The mark is taken before the render: a write landing in between
		// makes the next frame's mark differ, so staleness lasts one redraw
		// hint, never forever.
		key.change = t.Change()
	}
	return key, true
}

// prunePaneFrames drops cached frames for hosts that left the run, so the
// cache is bounded by the fleet. It runs where the host list changes — the
// fleet snapshot — not per frame.
func (a App) prunePaneFrames() App {
	if a.paneFrames == nil || len(a.paneFrames.frames) == 0 {
		return a
	}
	keep := make(map[string]bool, len(a.fleetHosts))
	for _, id := range a.fleetHosts {
		keep[id] = true
	}
	for id := range a.paneFrames.frames {
		if !keep[id] {
			delete(a.paneFrames.frames, id)
		}
	}
	return a
}
