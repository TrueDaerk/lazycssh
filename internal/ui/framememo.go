package ui

// frameMemo caches the two derived values everything in one frame asks for
// again and again: the visible host list and the grid tiled for it. Both are
// pure functions of the model, but rebuilding them per pane and per header
// dominated a frame's allocations under load — tens of megabytes per frame on
// a dozen panes (issue #274).
//
// The memo lives exactly one View call. View works on a value copy of the
// model, plants a fresh memo on that copy, and throws the copy away with the
// returned frame — so nothing the memo caches can survive into a later Update,
// and Update-side callers (memo nil) compute the honest way. That is also why
// the field must never be set anywhere but View.
type frameMemo struct {
	hostIDs    []string
	hostIDsSet bool
	grid       *Grid
	// content is each pane's measured content, so the body and the frame's
	// cursor are placed from one snapshot of a screen the session's reader
	// goroutine keeps changing (issue #292).
	content map[string]memoPaneContent
}

// memoPaneContent is one memoized [App.paneContent] result, ok and all: a host
// that has said nothing yet must not be re-measured on every lookup either.
type memoPaneContent struct {
	c  paneContent
	ok bool
}

// memoHostIDs is hostIDs through the frame's memo when one is live.
func (a App) memoHostIDs() []string {
	if a.memo == nil {
		return a.visibleHosts()
	}
	if !a.memo.hostIDsSet {
		a.memo.hostIDs = a.visibleHosts()
		a.memo.hostIDsSet = true
	}
	return a.memo.hostIDs
}

// memoGrid is grid through the frame's memo when one is live.
func (a App) memoGrid() Grid {
	if a.memo == nil {
		return a.tileGrid()
	}
	if a.memo.grid == nil {
		g := a.tileGrid()
		a.memo.grid = &g
	}
	return *a.memo.grid
}
