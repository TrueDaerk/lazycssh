package ui

import (
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// The simulated remote cursor (issue #301): a frame has one terminal cursor
// and the focused pane owns it, so every other pane in the broadcast set marks
// its host's cursor position with a styled cell instead.

// broadcastCursorApp is [cursorApp] with the router, so a test can choose the
// broadcast mode the marks depend on.
func broadcastCursorApp(t *testing.T) (App, *fakeFleet, *broadcast.Router) {
	t.Helper()
	a, fleet, router, _ := statusApp(t, "web-01", "web-02")
	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")
	a = syncFleet(t, a)
	return focusGrid(t, a), fleet, router
}

// markedBody is a pane's body as the grid draws it: the text plus whatever
// mark the pane paints on it. paneBody itself stays unmarked - that is what
// the clipboard and the text selection read - so the mark is applied here the
// way renderPaneFresh applies it.
func markedBody(a App, id string, width, height int) string {
	return a.paintRemoteCursor(a.paneBody(id, width, height), a.remoteCursorMark(id, width, height))
}

// markColumn is the display column the pane's body paints its remote-cursor
// mark on, read off the rendered body rather than recomputed: the mark is the
// one styled cell there, and the text before it says where it sits.
func markColumn(t *testing.T, body string, row int) (int, bool) {
	t.Helper()
	lines := strings.Split(body, "\n")
	if row < 0 || row >= len(lines) {
		t.Fatalf("row %d is outside the %d-line body", row, len(lines))
	}
	line := lines[row]
	i := strings.Index(line, "\x1b[")
	if i < 0 {
		return 0, false
	}
	return len(plain(line[:i])), true
}

// The acceptance criterion for mode all: a connected pane that is not the
// focused one marks its cursor where its own host says it is, and the focused
// pane keeps the real terminal cursor rather than a mark.
func TestBroadcastTargetsMarkTheirCursor(t *testing.T) {
	a, fleet, _ := broadcastCursorApp(t)
	fleet.sessions["web-01"].Emit("$ ")
	fleet.sessions["web-02"].Emit("$ echo hi")

	body := markedBody(a, "web-02", 40, 5)
	col, ok := markColumn(t, body, 0)
	if !ok {
		t.Fatalf("the broadcast target paints no cursor mark:\n%q", body)
	}
	if want := len("$ echo hi"); col != want {
		t.Fatalf("the mark sits at column %d, want %d:\n%q", col, want, body)
	}

	if focused := markedBody(a, "web-01", 40, 5); focused != plain(focused) {
		t.Fatalf("the focused pane paints a mark as well as owning the caret:\n%q", focused)
	}
	if c := a.View().Cursor; c == nil {
		t.Fatal("the focused pane lost the real cursor")
	}
}

// The mark follows the host: a cursor moved back into the line is marked
// there, not at the end of the text.
func TestRemoteCursorMarkFollowsTheHost(t *testing.T) {
	a, fleet, _ := broadcastCursorApp(t)
	fleet.sessions["web-02"].Emit("$ abcdef")

	col, ok := markColumn(t, markedBody(a, "web-02", 40, 5), 0)
	if !ok || col != len("$ abcdef") {
		t.Fatalf("mark at column %d (%v), want %d", col, ok, len("$ abcdef"))
	}

	fleet.sessions["web-02"].Emit("\b\b\b")
	col, ok = markColumn(t, markedBody(a, "web-02", 40, 5), 0)
	if !ok || col != len("$ abc") {
		t.Fatalf("mark at column %d (%v) after three backspaces, want %d", col, ok, len("$ abc"))
	}
}

// Mode selected marks only the panes in the selection: the set is the promise
// about where a keystroke lands, and the marks are that promise drawn.
func TestRemoteCursorMarksOnlySelectedHosts(t *testing.T) {
	a, fleet, router := broadcastCursorApp(t)
	fleet.sessions["web-01"].Emit("$ ")
	fleet.sessions["web-02"].Emit("$ ")

	router.Select("web-02")
	if err := router.SetMode(broadcast.ModeSelected); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if body := markedBody(a, "web-02", 40, 5); body == plain(body) {
		t.Fatalf("the selected pane paints no mark:\n%q", body)
	}

	router.Deselect("web-02")
	if body := markedBody(a, "web-02", 40, 5); body != plain(body) {
		t.Fatalf("a deselected pane still paints a mark:\n%q", body)
	}
}

// A pane scrolled back shows history, and a disconnected one takes no input:
// a mark in either would claim a keystroke goes somewhere it does not - the
// same refusals the real caret makes.
func TestRemoteCursorNoMarkWhenScrolledOrDisconnected(t *testing.T) {
	a, fleet, _ := broadcastCursorApp(t)
	for i := range 200 {
		fleet.sessions["web-02"].Emitf("line-%03d\n", i)
	}
	if body := markedBody(a, "web-02", 40, 5); body == plain(body) {
		t.Fatalf("setup: the tail-following target paints no mark:\n%q", body)
	}

	a.paneIndex = 1 // scroll the second pane, which is the marked one
	a = a.scrollBy(5)
	if a.scrollOffset("web-02") == 0 {
		t.Fatal("the pane did not scroll; the test would assert nothing")
	}
	if body := markedBody(a, "web-02", 40, 5); body != plain(body) {
		t.Fatalf("a scrolled-back target paints a mark:\n%q", body)
	}

	a = a.scrollToBottom()
	fleet.sessions["web-02"].Disconnect(nil)
	a = syncFleet(t, a)
	if body := markedBody(a, "web-02", 40, 5); body != plain(body) {
		t.Fatalf("a disconnected target paints a mark:\n%q", body)
	}
}

// The render cache must not serve a pane whose mark moved: switching the
// broadcast mode changes nothing about the host's output, and the mark has to
// appear all the same (issue #293's cache, issue #301's mark).
func TestRemoteCursorMarkInvalidatesTheRenderCache(t *testing.T) {
	a, fleet, router := broadcastCursorApp(t)
	fleet.sessions["web-02"].Emit("$ ")
	if err := router.SetMode(broadcast.ModeSingle); err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	_ = a.View() // prime the cache with the unmarked pane
	before := renderCount(a, "web-02")
	if body := markedBody(a, "web-02", 40, 5); body != plain(body) {
		t.Fatalf("setup: single mode already marks the pane:\n%q", body)
	}

	if err := router.SetMode(broadcast.ModeAll); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	frame := a.View()
	if got := renderCount(a, "web-02"); got != before+1 {
		t.Fatalf("the mode switch left the cached, unmarked pane in place: renders %d -> %d", before, got)
	}
	if !strings.Contains(frame.Content, a.theme.RemoteCursor.Render(" ")) {
		t.Fatal("the re-rendered frame carries no cursor mark")
	}
}
