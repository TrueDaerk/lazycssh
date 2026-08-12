package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// The pane frame cache (issue #291) must be invisible: a cached View and an
// honest one are the same frame, whatever changed in between. The cache is
// disabled by clearing the pointer, which is the documented nil-cache mode.

// cacheApp builds a connected, echoing fleet with output on every pane.
func cacheApp(t *testing.T, names ...string) (App, *fakeFleet) {
	t.Helper()
	fleet := newFakeFleet(names...)
	for _, name := range names {
		fleet.sessions[name].EchoInput = true
		fleet.connect(t, name)
		fleet.sessions[name].Emit("hello from " + name + "\r\n")
	}
	a := resize(t, NewApp(Config{
		Hosts: names, Fleet: fleet, Panes: fleet, Theme: Options{Dark: true},
	}), 220, 60)
	return syncFleet(t, a), fleet
}

// honestView renders the same model with the cache off.
func honestView(a App) string {
	a.paneFrames = nil
	return a.View().Content
}

// assertViewMatches fails when the cached frame differs from the honest one.
func assertViewMatches(t *testing.T, a App, step string) {
	t.Helper()
	got := a.View().Content
	want := honestView(a)
	if got != want {
		t.Fatalf("after %s the cached View differs from the uncached one:\ncached:\n%s\nuncached:\n%s",
			step, got, want)
	}
}

func TestCachedViewMatchesUncachedRender(t *testing.T) {
	a, fleet := cacheApp(t, "web-01", "web-02", "web-03", "web-04")
	assertViewMatches(t, a, "the first frame")

	// One host speaks: exactly one pane changes, three come from the cache.
	fleet.sessions["web-02"].Emit("only web-02 said this\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "web-02"})
	a = model.(App)
	assertViewMatches(t, a, "output on one host")

	// Entering the grid and typing: focus styling and the echoed keystroke.
	a.focus = AreaGrid
	a = pressKey(t, a, "x")
	assertViewMatches(t, a, "a typed keystroke")

	// Focus moves: the styles of two panes change, none of the content.
	a = pressKey(t, a, "alt+right")
	assertViewMatches(t, a, "a focus move")

	// A live search restyles the matching lines.
	a.searchTerm = "hello"
	assertViewMatches(t, a, "a live search term")
	a.searchTerm = ""

	// Scrolling back changes one pane's window.
	fleet.sessions["web-01"].Flood(200)
	model, _ = a.Update(SessionOutputMsg{ID: "web-01"})
	a = model.(App)
	a.scroll["web-01"] = 40
	assertViewMatches(t, a, "a scrolled-back pane")
	delete(a.scroll, "web-01")

	// A command exit changes the header's label and the border's colour.
	fleet.sessions["web-03"].ReportExit(1)
	a = syncFleet(t, a)
	assertViewMatches(t, a, "a failed command")

	// A dropped host changes its header state.
	fleet.sessions["web-04"].Disconnect(ssh.ErrDisconnected())
	a = syncFleet(t, a)
	assertViewMatches(t, a, "a dropped host")

	// The theme is rebuilt when the terminal reports its background.
	model, _ = a.Update(tea.BackgroundColorMsg{})
	a = model.(App)
	assertViewMatches(t, a, "a theme change")

	// A resize moves every cell.
	a = resize(t, a, 180, 50)
	assertViewMatches(t, a, "a resize")

	// Full screen draws one pane into the whole main area.
	a = pressKey(t, a, "alt+z")
	assertViewMatches(t, a, "full screen")
	a = pressKey(t, a, "alt+z")
	assertViewMatches(t, a, "leaving full screen")
}

func TestCachedViewMatchesUncachedRenderDuringAuth(t *testing.T) {
	a, fleet := authTestApp(t)
	a = resize(t, a, 220, 60)
	assertViewMatches(t, a, "an open auth question")

	// The typed answer echoes into the pane, which is one of the two states
	// the cache refuses to hold.
	a.focus = AreaGrid
	a = pressKey(t, a, "s")
	a = pressKey(t, a, "3")
	assertViewMatches(t, a, "typing an auth answer")
	_ = fleet
}

// An unchanged pane is served from the cache: a sentinel planted into its
// cached frame surfaces in the next View, while the changed pane re-renders.
func TestUnchangedPaneIsServedFromCache(t *testing.T) {
	a, fleet := cacheApp(t, "web-01", "web-02")
	_ = a.View()

	hit, ok := a.paneFrames.frames["web-02"]
	if !ok {
		t.Fatal("no cached frame for web-02 after a View")
	}
	// Same length, so the sentinel cannot shear the frame's geometry.
	hit.frame = strings.Replace(hit.frame, "web-02", "WEB-XX", 1)
	a.paneFrames.frames["web-02"] = hit

	fleet.sessions["web-01"].Emit("fresh words\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "web-01"})
	a = model.(App)

	content := a.View().Content
	if !strings.Contains(content, "WEB-XX") {
		t.Fatal("the unchanged pane was re-rendered instead of served from the cache")
	}
	if !strings.Contains(content, "fresh words") {
		t.Fatal("the changed pane was served stale instead of re-rendered")
	}
}

// Hosts that leave the run leave the cache with their panes.
func TestPaneFramesArePruned(t *testing.T) {
	a, _ := cacheApp(t, "web-01", "web-02")
	_ = a.View()
	if len(a.paneFrames.frames) != 2 {
		t.Fatalf("cached %d frames, want 2", len(a.paneFrames.frames))
	}

	model, _ := a.Update(HostsChangedMsg{Hosts: []string{"web-01"}})
	a = model.(App)
	// The fake fleet still lists both; the prune follows the snapshot, so
	// shrink the fleet the way a removal does before snapshotting again.
	fleet := a.cfg.Fleet.(*fakeFleet)
	fleet.ids = []string{"web-01"}
	a = syncFleet(t, a)

	if _, ok := a.paneFrames.frames["web-02"]; ok {
		t.Fatal("a removed host's frame is still cached")
	}
}

// The redraw-hint fast path in Update must not skip the one panel whose
// content is the output: a selected Output diff panel regroups per hint.
func TestOutputHintStillRegroupsSelectedDiffPanel(t *testing.T) {
	a, fleet := diffApp(t, "uptime", "web-01", "web-02")
	a = selectDiffPanel(t, a)

	fleet.sessions["web-01"].Emit("answer A\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "web-01"})
	a = model.(App)

	body := plain(a.panelBody(PanelDiff, 60, 10, true))
	if !strings.Contains(body, "answer A") {
		t.Fatalf("the diff panel missed output that arrived while selected:\n%s", body)
	}
}

// zipJoinRow is JoinHorizontal for the grid's exact-rectangle frames, and
// falls back to the honest join when the shape it relies on is broken.
func TestZipJoinRowMatchesLipgloss(t *testing.T) {
	a, _ := cacheApp(t, "web-01", "web-02", "web-03")
	g := a.Grid()
	var cells []string
	for i := range g.Cells {
		cells = append(cells, a.renderPane(i, g.Cells[i], false))
	}
	if got, want := zipJoinRow(cells), lipgloss.JoinHorizontal(lipgloss.Top, cells...); got != want {
		t.Fatalf("zipJoinRow differs from JoinHorizontal:\n%q\n%q", got, want)
	}

	ragged := []string{"a\nb\nc", "x\ny"}
	if got, want := zipJoinRow(ragged), lipgloss.JoinHorizontal(lipgloss.Top, ragged...); got != want {
		t.Fatalf("ragged fallback differs from JoinHorizontal:\n%q\n%q", got, want)
	}
}
