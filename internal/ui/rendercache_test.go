package ui

import (
	"context"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// The cross-frame render cache (issue #293): a pane whose host said nothing
// since the last frame is served from its cached frame, not re-rendered. The
// hostRender.renders counter is the proof — it moves only when renderPane
// really rebuilt the pane.

// cacheApp builds a connected fleet with some scrollback per host, sized and
// rendered once so every pane's cache entry is primed.
func cacheApp(t *testing.T, names ...string) (App, *fakeFleet) {
	t.Helper()
	fleet := newFakeFleet(names...)
	for _, name := range names {
		s := fleet.sessions[name]
		if err := s.Start(context.Background()); err != nil {
			t.Fatalf("start %s: %v", name, err)
		}
		s.Emitf("hello from %s\r\n", name)
	}
	a := NewApp(Config{Hosts: names, Fleet: fleet, Theme: Options{Dark: true}})
	model, _ := a.Update(tea.WindowSizeMsg{Width: 220, Height: 60})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	_ = next.View()
	return next, fleet
}

// renderCount is how often a host's pane was really rendered so far.
func renderCount(a App, id string) int {
	return a.render.entry(id).renders
}

// A frame forced by one pane's output re-renders that pane and only that
// pane; the neighbours' content comes out of the cache.
func TestOutputOnOnePaneDoesNotReRenderTheOthers(t *testing.T) {
	a, fleet := cacheApp(t, "web-01", "web-02", "web-03")
	before01, before02, before03 := renderCount(a, "web-01"), renderCount(a, "web-02"), renderCount(a, "web-03")

	fleet.sessions["web-01"].Emit("fresh output\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "web-01"})
	a = model.(App)
	frame := a.View()

	if got := renderCount(a, "web-01"); got != before01+1 {
		t.Fatalf("the chatty pane rendered %d times, want %d", got, before01+1)
	}
	if got := renderCount(a, "web-02"); got != before02 {
		t.Fatalf("a quiet pane re-rendered: web-02 went %d -> %d", before02, got)
	}
	if got := renderCount(a, "web-03"); got != before03 {
		t.Fatalf("a quiet pane re-rendered: web-03 went %d -> %d", before03, got)
	}
	if !strings.Contains(frame.Content, "fresh output") {
		t.Fatal("the new output is not on the frame")
	}
}

// In full-screen mode, output for a pane that is not on screen must not make
// the visible pane pay for a re-render (issue #293's acceptance criterion).
func TestFullScreenHiddenOutputDoesNotReRenderTheVisiblePane(t *testing.T) {
	a, fleet := cacheApp(t, "web-01", "web-02")
	a = focusGrid(t, a)
	a = pressKey(t, a, "alt+z")
	if !a.FullScreen() {
		t.Fatal("alt+z did not enter full screen")
	}
	_ = a.View()
	before := renderCount(a, "web-01")

	fleet.sessions["web-02"].Emit("hidden output\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "web-02"})
	a = model.(App)
	frame := a.View()

	if got := renderCount(a, "web-01"); got != before {
		t.Fatalf("hidden output re-rendered the visible pane: %d -> %d", before, got)
	}
	if strings.Contains(frame.Content, "hidden output") {
		t.Fatal("the hidden pane's output leaked onto the full-screen frame")
	}
}

// An unchanged fleet costs no pane renders at all frame over frame — the
// whole grid is served from the cache.
func TestQuietFramesRenderNoPanes(t *testing.T) {
	a, _ := cacheApp(t, "web-01", "web-02")
	before01, before02 := renderCount(a, "web-01"), renderCount(a, "web-02")

	first := a.View().Content
	second := a.View().Content

	if got := renderCount(a, "web-01"); got != before01 {
		t.Fatalf("a quiet frame re-rendered web-01: %d -> %d", before01, got)
	}
	if got := renderCount(a, "web-02"); got != before02 {
		t.Fatalf("a quiet frame re-rendered web-02: %d -> %d", before02, got)
	}
	if first != second {
		t.Fatal("two quiet frames disagree")
	}
}

// The cache must never serve a stale frame: every model input renderPane
// reads is in the key, so changing one re-renders. Focus is the visible one —
// the border and header restyle — and a resize regeometries every pane.
func TestCacheInvalidatesOnFocusAndResize(t *testing.T) {
	a, _ := cacheApp(t, "web-01", "web-02")
	before := renderCount(a, "web-01")

	a = focusGrid(t, a)
	_ = a.View()
	if got := renderCount(a, "web-01"); got <= before {
		t.Fatalf("focusing the pane did not re-render it: %d -> %d", before, got)
	}

	before = renderCount(a, "web-02")
	model, _ := a.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	a = model.(App)
	_ = a.View()
	if got := renderCount(a, "web-02"); got <= before {
		t.Fatalf("resizing did not re-render the panes: %d -> %d", before, got)
	}
}

// Scrolling a pane changes what its window shows, so it re-renders — and the
// frame really shows the scrolled state, not the cached tail.
func TestCacheInvalidatesOnScroll(t *testing.T) {
	a, fleet := cacheApp(t, "web-01")
	for i := 0; i < 200; i++ {
		fleet.sessions["web-01"].Emitf("line %03d\r\n", i)
	}
	a = focusGrid(t, a)
	_ = a.View()
	before := renderCount(a, "web-01")

	a = pressKey(t, a, "shift+pgup")
	frame := a.View().Content

	if got := renderCount(a, "web-01"); got <= before {
		t.Fatalf("scrolling did not re-render the pane: %d -> %d", before, got)
	}
	if !strings.Contains(frame, "scrollback") {
		t.Fatal("the status bar does not announce the scrolled pane")
	}
}

// The cache changes cost, never content: the frame served from a primed
// cache is byte-identical to the one an uncached model renders.
func TestCachedFrameMatchesFreshRender(t *testing.T) {
	a, _ := cacheApp(t, "web-01", "web-02")
	cached := a.View().Content

	fresh := a
	fresh.render = nil
	if got := fresh.View().Content; got != cached {
		t.Fatal("the cached frame differs from a fresh render")
	}
}

// A host that leaves the fleet takes its cache entry with it.
func TestCachePrunesDepartedHosts(t *testing.T) {
	a, fleet := cacheApp(t, "web-01", "web-02")
	if _, ok := a.render.hosts["web-02"]; !ok {
		t.Fatal("the primed cache misses web-02")
	}

	fleet.ids = []string{"web-01"}
	delete(fleet.sessions, "web-02")
	model, _ := a.Update(HostsChangedMsg{Hosts: []string{"web-01"}})
	a = model.(App)

	if _, ok := a.render.hosts["web-02"]; ok {
		t.Fatal("the departed host's cache entry survived the fleet change")
	}
}

// The cache must stay invisible through a whole session of interactions, not
// only on a quiet frame: after every step below, the cached View and the
// honest one (render nil, the documented cache-off mode) are the same frame
// (issue #291).
func TestCachedViewMatchesFreshRenderThroughInteractions(t *testing.T) {
	fleet := newFakeFleet("web-01", "web-02", "web-03", "web-04")
	for _, name := range fleet.ids {
		fleet.sessions[name].EchoInput = true
		fleet.connect(t, name)
		fleet.sessions[name].Emit("hello from " + name + "\r\n")
	}
	a := resize(t, NewApp(Config{
		Hosts: fleet.ids, Fleet: fleet, Panes: fleet, Theme: Options{Dark: true},
	}), 220, 60)
	a = syncFleet(t, a)

	match := func(step string) {
		t.Helper()
		fresh := a
		fresh.render = nil
		if got, want := a.View().Content, fresh.View().Content; got != want {
			t.Fatalf("after %s the cached View differs from the fresh one:\ncached:\n%s\nfresh:\n%s",
				step, got, want)
		}
	}

	match("the first frame")

	fleet.sessions["web-02"].Emit("only web-02 said this\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "web-02"})
	a = model.(App)
	match("output on one host")

	a.focus = AreaGrid
	a = pressKey(t, a, "x") // typed into the focused pane, echoed by the fake
	match("a typed keystroke")

	a = pressKey(t, a, "alt+right")
	match("a focus move")

	a.searchTerm = "hello"
	match("a live search term")
	a.searchTerm = ""

	fleet.sessions["web-01"].Flood(200)
	model, _ = a.Update(SessionOutputMsg{ID: "web-01"})
	a = model.(App)
	a.scroll["web-01"] = 40
	match("a scrolled-back pane")
	delete(a.scroll, "web-01")

	fleet.sessions["web-03"].ReportExit(1)
	a = syncFleet(t, a)
	match("a failed command")

	fleet.sessions["web-04"].Disconnect(ssh.ErrDisconnected())
	a = syncFleet(t, a)
	match("a dropped host")

	model, _ = a.Update(tea.BackgroundColorMsg{})
	a = model.(App)
	match("a theme change")

	a = resize(t, a, 180, 50)
	match("a resize")

	a = pressKey(t, a, "alt+z")
	match("full screen")
	a = pressKey(t, a, "alt+z")
	match("leaving full screen")
}

// An open auth question echoes the typed answer into the pane body; the cache
// must not serve the frame from before a keystroke of it.
func TestCachedViewMatchesFreshRenderDuringAuth(t *testing.T) {
	a, _ := authTestApp(t)
	a = resize(t, a, 220, 60)
	a.focus = AreaGrid
	for _, step := range []string{"s", "3"} {
		a = pressKey(t, a, step)
		fresh := a
		fresh.render = nil
		if got, want := a.View().Content, fresh.View().Content; got != want {
			t.Fatalf("after typing %q into the auth prompt the cached View differs from the fresh one", step)
		}
	}
}
