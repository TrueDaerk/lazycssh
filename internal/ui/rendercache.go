package ui

import (
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/term"
)

// The cross-frame pane render cache (issue #293). Every frame used to rebuild
// every visible pane from scratch — measure the emulator, materialize the
// window, style the header, draw the border — even though most panes had not
// changed since the last frame: one chatty host forces a redraw, and eleven
// quiet neighbours pay for it. Full screen is the worst case, where output for
// a pane that is not even on screen re-rendered the largest possible pane.
//
// The frame memo (framememo.go) cannot help across frames by design: it dies
// with each View call so nothing it caches can leak into Update. This cache is
// the other repo pattern, the one the search cache set (issue #278): a pointer
// shared by every value copy of the model, holding memoized answers to pure
// questions about the emulator's state, self-validating on every read. There
// is no dirty flag to set and none to forget: the emulator's [term.Emulator.Seq]
// counter moves with every write, resize and retention change, and a cached
// entry whose sequence still matches is by definition current. Everything else
// a pane's rendering depends on — geometry, focus, scroll, search, selection,
// theme — is captured in [paneKey]; any mismatch re-renders.
//
// Filling the cache from View is safe for the same reason filling the search
// cache is: Update and View run on one goroutine, and a model copy observing
// a newer fill only skips work it would have redone. View stays effectively
// pure over the model — the cache is derived state, not model state.
type renderCache struct {
	hosts map[string]*hostRender
}

// hostRender is one host's cached render state: the measured content snapshot
// the body and the cursor share, and the fully framed pane string.
type hostRender struct {
	// t is the emulator the snapshot was measured from and seq its
	// [term.Emulator.Seq] at the measure; any other emulator or a moved
	// sequence re-measures.
	t   *term.Emulator
	seq uint64
	// measured says c and ok hold a snapshot for (t, seq).
	measured bool
	c        paneContent
	ok       bool
	// pane is the framed pane string rendered under key; framed says it is
	// live. The key embeds seq, so a re-measured snapshot orphans it.
	key    paneKey
	pane   string
	framed bool
	// renders counts the real (non-cached) pane renders, which is how the
	// tests prove an unchanged pane was not re-rendered.
	renders int
}

// paneKey is everything a framed pane string depends on besides the emulator
// content itself. Comparable by design: a cached pane is served only against
// an identical key, so forgetting a dependency here is the only way to serve
// a stale frame — when adding one to the render path, add it to the key.
type paneKey struct {
	t           *term.Emulator
	seq         uint64
	cell        Rect
	host        int
	id          string
	focused     bool
	selected    bool
	state       ssh.State
	exitLabel   string
	failed      bool
	scroll      int
	searchTerm  string
	matchCursor int
	echo        string
	echoOK      bool
	selValid    bool
	sel         textSelection
	theme       *Theme
}

// entry returns the host's cache slot, creating it on first sight.
func (rc *renderCache) entry(id string) *hostRender {
	if rc.hosts == nil {
		rc.hosts = make(map[string]*hostRender)
	}
	e := rc.hosts[id]
	if e == nil {
		e = &hostRender{}
		rc.hosts[id] = e
	}
	return e
}

// prune drops the entries of hosts that left the fleet, so a long run cannot
// accumulate frames for panes that no longer exist.
func (rc *renderCache) prune(ids []string) {
	if rc == nil || len(rc.hosts) == 0 {
		return
	}
	keep := make(map[string]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	for id := range rc.hosts {
		if !keep[id] {
			delete(rc.hosts, id)
		}
	}
}

// cachedPaneContent is [App.measurePaneContent] behind the cross-frame cache:
// the snapshot is reused while the emulator's sequence vouches for it. The
// sequence is read before the measure, so a write racing the measure can only
// cost one spare re-measure on the next frame, never a stale hit.
func (a App) cachedPaneContent(id string) (paneContent, bool) {
	t := a.paneTerminal(id)
	if t == nil {
		return paneContent{}, false
	}
	if a.render == nil {
		return a.measurePaneContent(id)
	}
	e := a.render.entry(id)
	seq := t.Seq()
	if e.measured && e.t == t && e.seq == seq {
		return e.c, e.ok
	}
	c, ok := a.measurePaneContent(id)
	e.t, e.seq, e.c, e.ok, e.measured = t, seq, c, ok, true
	return c, ok
}

// paneKeyFor collects the model inputs a rendered pane depends on. The
// content snapshot must be pinned first — [App.paneContent] — so e.seq names
// the sequence the frame's body will actually be built from.
func (a App) paneKeyFor(e *hostRender, host int, id string, cell Rect, focused bool) paneKey {
	k := paneKey{
		t:           e.t,
		seq:         e.seq,
		cell:        cell,
		host:        host,
		id:          id,
		focused:     focused,
		selected:    host == a.paneIndex,
		state:       a.state(id),
		exitLabel:   a.exitLabel(id),
		failed:      a.commandFailed(id),
		scroll:      a.scrollOffset(id),
		searchTerm:  a.searchTerm,
		matchCursor: a.matchCursor(id),
		theme:       a.theme,
	}
	k.echo, k.echoOK = a.inlineAnswerEcho(id)
	if a.textSel.host == id {
		k.sel, k.selValid = a.textSel, a.textSelectionValid()
	}
	return k
}
