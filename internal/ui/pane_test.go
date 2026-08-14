package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// The pane shows the newest output: watching the tail is what the grid is for.
func TestPaneBodyFollowsTheTail(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 30; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}

	body := plain(a.paneBody("web-01", 40, 5))
	if !strings.Contains(body, "line-30") {
		t.Fatalf("the newest line is missing:\n%s", body)
	}
	if strings.Contains(body, "line-01") {
		t.Fatalf("the oldest line pushed the newest out:\n%s", body)
	}
	if got := len(strings.Split(body, "\n")); got > 5 {
		t.Fatalf("body is %d lines, want at most 5:\n%s", got, body)
	}
}

// The acceptance criterion: output from `ls --color` looks right - the colours
// are still there after sanitizing and wrapping.
func TestPaneBodyPreservesColors(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("\x1b[01;34mbin\x1b[0m  \x1b[01;36mlink\x1b[0m  file.txt\n")

	body := a.paneBody("web-01", 40, 5)
	// The emulator re-renders styles canonically, so the assertion is on the
	// colour surviving, not on the exact byte form the host sent.
	if !strings.Contains(body, "\x1b[34;1mbin") {
		t.Fatalf("the directory colour is gone:\n%q", body)
	}
	if !strings.Contains(plain(body), "bin") || !strings.Contains(plain(body), "file.txt") {
		t.Fatalf("the text is gone:\n%q", body)
	}
}

// The acceptance criterion: a remote program emitting cursor escapes cannot
// corrupt the surrounding layout. Every rendered line of the whole frame stays
// exactly as wide as the terminal.
func TestCursorEscapesCannotCorruptTheLayout(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	fleet.sessions["web-01"].Emit("\x1b[2J\x1b[H\x1b[10;20Hboom\x1b[5A\x1b]0;title\a\n")
	fleet.sessions["web-02"].Emit("calm\n")

	view := plain(a.View().Content)
	for i, line := range strings.Split(view, "\n") {
		if got := len([]rune(line)); got != 200 {
			t.Fatalf("line %d is %d columns wide, want 200:\n%s", i, got, view)
		}
	}
	if !strings.Contains(view, "boom") || !strings.Contains(view, "calm") {
		t.Fatalf("the output text is missing:\n%s", view)
	}
}

// A line longer than the pane wraps rather than widening the pane.
func TestPaneBodyWrapsLongLines(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Resize(20, 10)
	fleet.sessions["web-01"].Emit(strings.Repeat("abcdefghij", 5) + "\n")

	body := plain(a.paneBody("web-01", 20, 10))
	lines := strings.Split(body, "\n")
	if len(lines) < 3 {
		t.Fatalf("a 50-column line did not wrap at 20:\n%s", body)
	}
	for i, line := range lines {
		if len([]rune(line)) > 20 {
			t.Fatalf("wrapped line %d is %d columns wide: %q", i, len([]rune(line)), line)
		}
	}
	if got := strings.Join(lines, ""); got != strings.Repeat("abcdefghij", 5) {
		t.Fatalf("wrapping lost content: %q", got)
	}
}

// The acceptance criterion for the epic: truncated scrollback is visible, not
// silent. The marker sits where the missing output was.
func TestPaneBodyMarksDroppedOutput(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Resize(40, 5)
	fleet.sessions["web-01"].Terminal().SetHistorySize(5)
	for i := 1; i <= 15; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}

	// Scrolled all the way back, the marker sits where the missing output
	// was. Following the tail it is above the screen, like in any terminal.
	a.scroll = map[string]int{"web-01": 100}
	body := plain(a.paneBody("web-01", 40, 5))
	if !strings.Contains(body, "older output dropped") {
		t.Fatalf("no dropped marker:\n%s", body)
	}
	if !strings.HasPrefix(body, "~ older output dropped ~") {
		t.Fatalf("the marker is not where the missing output was:\n%s", body)
	}
}

// The pane renders through View as well: the grid shows the output, not just
// the host name.
func TestPaneOutputReachesTheFrame(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("$ uptime\n 17:02:11 up 42 days\n")

	view := plain(a.View().Content)
	if !strings.Contains(view, "up 42 days") {
		t.Fatalf("the pane does not show the output:\n%s", view)
	}
}

// Output only asks for a redraw; there is nothing to store and no command to
// run.
func TestSessionOutputMsgRedraws(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	before := a.View().Content

	fleet.sessions["web-01"].Emit("fresh output\n")
	model, cmd := a.Update(SessionOutputMsg{ID: "web-01"})
	if cmd != nil {
		t.Fatal("SessionOutputMsg produced a command")
	}
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if next.View().Content == before {
		t.Fatal("new output changed nothing on screen")
	}
}

// Degenerate sizes and missing sessions render as nothing rather than
// panicking: the pane must never take the program down.
func TestPaneBodyDegenerateCases(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("text\n")

	if got := a.paneBody("web-01", 0, 5); got != "" {
		t.Fatalf("zero width rendered %q", got)
	}
	if got := a.paneBody("web-01", 20, 0); got != "" {
		t.Fatalf("zero height rendered %q", got)
	}
	if got := a.paneBody("web-99", 20, 5); got != "" {
		t.Fatalf("an unknown host rendered %q", got)
	}

	quiet := a.paneBody("web-01", 20, 5)
	if plain(quiet) != "text" {
		t.Fatalf("a quiet session renders %q", quiet)
	}

	noFleet := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	if got := noFleet.paneBody("h1", 20, 5); got != "" {
		t.Fatalf("a run without a fleet rendered %q", got)
	}
}

// The acceptance criterion of issue 131: `clear` on a host leaves an
// (apparently) empty pane instead of the full previous output.
func TestPaneBodyIsEmptyAfterClear(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 10; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}
	fleet.sessions["web-01"].Emit("\x1b[H\x1b[2J")

	if body := plain(a.paneBody("web-01", 40, 5)); body != "" {
		t.Fatalf("the pane still shows output after clear:\n%s", body)
	}
}

// After a clear, new output starts at the top of the pane, alone - the old
// lines do not bleed in from above while following the tail.
func TestPaneBodyShowsOnlyPostClearOutput(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 10; i++ {
		fleet.sessions["web-01"].Emitf("old-%02d\n", i)
	}
	fleet.sessions["web-01"].Emit("\x1b[2J$ uptime\n")

	body := plain(a.paneBody("web-01", 40, 5))
	if !strings.Contains(body, "$ uptime") {
		t.Fatalf("the post-clear output is missing:\n%s", body)
	}
	if strings.Contains(body, "old-") {
		t.Fatalf("pre-clear output bleeds into the cleared pane:\n%s", body)
	}
}

// Entering the alternate screen - `screen`, `vim` - shows a fresh panel too:
// the pane switches to the emulator's live grid, which is blank until the
// full-screen app draws.
func TestPaneBodyClearsOnAlternateScreen(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("$ screen\n")
	fleet.sessions["web-01"].Emit("\x1b[?1049h")

	if body := strings.TrimSpace(plain(a.paneBody("web-01", 40, 5))); body != "" {
		t.Fatalf("entering the alternate screen left old output on show:\n%s", body)
	}
}

// A pane whose remote app is on the alternate screen renders the emulator's
// live grid: what the app drew, where it drew it — not scrollback text.
func TestPaneBodyRendersAltScreenGrid(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("$ vim notes\n")
	fleet.sessions["web-01"].Resize(40, 5)
	fleet.sessions["web-01"].Emit("\x1b[?1049h\x1b[2;3HEDITOR")

	body := plain(a.paneBody("web-01", 40, 5))
	lines := strings.Split(body, "\n")
	if len(lines) < 2 || !strings.Contains(lines[1], "EDITOR") {
		t.Fatalf("grid content missing or misplaced:\n%s", body)
	}
	if strings.Contains(body, "$ vim") {
		t.Fatalf("scrollback text bleeds into the grid view:\n%s", body)
	}
}

// Leaving the alternate screen returns to the scrollback view. The tail shows
// the post-app screen (cleared, like a terminal after vim quits); the history
// from before the app stays reachable by scrolling, per the clear semantics.
func TestPaneBodyRestoresScrollbackAfterAltScreen(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 10; i++ {
		fleet.sessions["web-01"].Emitf("old-%02d\n", i)
	}
	fleet.sessions["web-01"].Emit("$ vim notes\n")
	fleet.sessions["web-01"].Emit("\x1b[?1049hEDITOR\x1b[?1049l")

	if a.paneAltScreen("web-01") {
		t.Fatal("pane still in grid view after the app quit")
	}
	a.scroll = map[string]int{"web-01": 5}
	body := plain(a.paneBody("web-01", 40, 5))
	if !strings.Contains(body, "old-") {
		t.Fatalf("scrollback history lost after the app quit:\n%s", body)
	}
}

// The grid never exceeds the pane body, even when the emulator is larger —
// a resize can lag one frame behind the layout.
func TestAltScreenGridIsClippedToThePane(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Resize(80, 24)
	fleet.sessions["web-01"].Emit("\x1b[?1049hwide")

	body := a.paneBody("web-01", 10, 3)
	lines := strings.Split(body, "\n")
	if len(lines) > 3 {
		t.Fatalf("grid is %d lines high, want at most 3", len(lines))
	}
	for i, line := range lines {
		if w := len([]rune(plain(line))); w > 10 {
			t.Fatalf("grid line %d is %d columns wide, want at most 10:\n%s", i, w, body)
		}
	}
}

// The grid is the remote app's screen and nothing else: the cursor rides on
// the frame, so a pane nobody is typing into - here one that never even
// connected - paints no caret over the app's own output (issue #292).
func TestAltScreenGridPaintsNoCursor(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Resize(40, 5)
	fleet.sessions["web-01"].Emit("\x1b[?1049h\x1b[1;1Hab")

	body := a.paneBody("web-01", 40, 5)
	if body != plain(body) {
		t.Fatalf("the grid styles a cursor cell:\n%q", body)
	}
	fleet.sessions["web-01"].Emit("\x1b[?25l")
	if hidden := a.paneBody("web-01", 40, 5); hidden != body {
		t.Fatalf("hiding the remote cursor changed the grid:\n%q\n%q", body, hidden)
	}
	if x, y, ok := a.paneCursor("web-01", 40, 5); ok {
		t.Fatalf("a pane that never connected reports a cursor at (%d,%d)", x, y)
	}
}

// Scrolling is a no-op while a full-screen app owns the pane: there is no
// scrollback view to move, and the offset must not jump when the app exits.
func TestScrollIsNoOpOnAltScreen(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 30; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}
	fleet.sessions["web-01"].Emit("\x1b[?1049h")

	a = a.scrollHostBy(0, 10)
	if got := a.scrollOffset("web-01"); got != 0 {
		t.Fatalf("scroll offset = %d on the alternate screen, want 0", got)
	}
}

// The status bar names the alt-screen exclusion: a broadcast keystroke never
// silently skips a vim host.
func TestStatusBarNamesTheAltScreenSkip(t *testing.T) {
	a, fleet, router, _ := statusApp(t, "web-01", "web-02")
	router.Attach(fleet)
	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")
	fleet.sessions["web-02"].Emit("\x1b[?1049h")

	view := plain(a.View().Content)
	if !strings.Contains(view, "1 alt-screen skipped") {
		t.Fatalf("status bar does not name the excluded vim host:\n%s", view)
	}
}

// The history is preserved, not wiped: a clear pushes the visible lines into
// the retention (the xterm behaviour), so scrolling up reaches the pre-clear
// output — and the ESC[3J some terminfos append is filtered out before it
// could erase it.
func TestClearedHistoryStaysScrollable(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Resize(40, 5)
	for i := 1; i <= 10; i++ {
		fleet.sessions["web-01"].Emitf("old-%02d\n", i)
	}
	fleet.sessions["web-01"].Emit("\x1b[H\x1b[2J\x1b[3J$ ")

	a.scroll = map[string]int{"web-01": 100}
	body := plain(a.paneBody("web-01", 40, 5))
	if !strings.Contains(body, "old-") {
		t.Fatalf("scrolling up does not reach the pre-clear history:\n%s", body)
	}
}

// A failed pane says why (issue #167): the dial or session error renders at
// the bottom of the body, after whatever output preceded the failure.
func TestFailedPaneShowsTheError(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("last words\n")
	fleet.sessions["web-01"].Disconnect(errors.New("connect to web-01: connection refused"))
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	body := plain(a.paneBody("web-01", 120, 6))
	if !strings.Contains(body, "connection refused") {
		t.Fatalf("the failure reason is missing:\n%s", body)
	}
	if !strings.Contains(body, "last words") {
		t.Fatalf("the error pushed the output away:\n%s", body)
	}
	if got := len(strings.Split(body, "\n")); got > 6 {
		t.Fatalf("body is %d lines, want at most 6:\n%s", got, body)
	}
}

// A pane that never produced output still shows the reason it failed.
func TestFailedPaneWithoutOutputShowsTheError(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Disconnect(errors.New("dial tcp: lookup web-01: no such host"))
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	body := plain(a.paneBody("web-01", 120, 6))
	if !strings.Contains(body, "no such host") {
		t.Fatalf("the failure reason is missing:\n%s", body)
	}
}

// The error is part of the scrollback, like a terminal prints it (issue #180):
// a long one scrolls the earlier output up, and scrolling back still reaches
// it - nothing floats over the history, nothing is capped away.
func TestFailedPaneErrorScrollsLikeOutput(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("kept\n")
	fleet.sessions["web-01"].Disconnect(errors.New(strings.Repeat("very long failure text ", 30)))
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	body := plain(a.paneBody("web-01", 20, 8))
	if got := len(strings.Split(body, "\n")); got > 8 {
		t.Fatalf("body is %d lines, want at most 8:\n%s", got, body)
	}
	if !strings.Contains(body, "failure text") {
		t.Fatalf("the failure is not in the output:\n%s", body)
	}
	if history := plain(strings.Join(a.virtualLines("web-01"), "\n")); !strings.Contains(history, "kept") {
		t.Fatalf("the earlier output is gone from the history:\n%s", history)
	}
}

// A host that closed cleanly carries no failure line, whatever error the
// session last recorded.
func TestClosedPaneShowsNoError(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	fleet.sessions["web-01"].ExitWithStatus(0)
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if body := plain(a.paneBody("web-01", 60, 6)); strings.Contains(body, "✗") {
		t.Fatalf("a closed pane renders a failure mark:\n%s", body)
	}
}

// The issue-190 behavior, now reported rather than painted (issue #292): a
// connected pane following the tail puts the cursor where the scrollback's
// line discipline says it is.
func TestPaneCursorAtTheEndOfThePrompt(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	fleet.sessions["web-01"].Emit("$ ")

	x, y, ok := a.paneCursor("web-01", 40, 5)
	if !ok || x != 2 || y != 0 {
		t.Fatalf("paneCursor() = (%d,%d,%v), want (2,0,true)", x, y, ok)
	}
}

// A pane outside the broadcast set carries no cursor of its own: the caret is
// the terminal's and only the focused pane gets it, so a pane no keystroke
// reaches paints nothing (issue #292). Single mode is exactly that set — the
// focused pane and no other; the broadcast-target case is in remotecursor_test.go.
func TestPaneBodyPaintsNoCursor(t *testing.T) {
	a, fleet, router, _ := statusApp(t, "web-01")
	if err := router.SetMode(broadcast.ModeSingle); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	fleet.sessions["web-01"].Emit("$ ")

	if body := markedBody(a, "web-01", 40, 5); body != plain(body) {
		t.Fatalf("the pane body styles a cursor block:\n%q", body)
	}
}

// Right after a line feed the cursor sits on the empty row below the output,
// exactly like a terminal between the command's output and the next prompt.
func TestPaneCursorOnTheEmptyRowAfterALineFeed(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	fleet.sessions["web-01"].Emit("done\n")

	body := a.paneBody("web-01", 40, 5)
	lines := strings.Split(body, "\n")
	if len(lines) != 2 || plain(lines[0]) != "done" {
		t.Fatalf("expected the output plus a cursor row:\n%q", body)
	}
	x, y, ok := a.paneCursor("web-01", 40, 5)
	if !ok || x != 0 || y != 1 {
		t.Fatalf("paneCursor() = (%d,%d,%v), want (0,1,true)", x, y, ok)
	}
}

// A cursor moved into the line - readline editing - is reported there, not at
// the end.
func TestPaneCursorFollowsBackspace(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	fleet.sessions["web-01"].Emit("$ abc\b\b")

	x, y, ok := a.paneCursor("web-01", 40, 5)
	if !ok || x != 3 || y != 0 {
		t.Fatalf("paneCursor() = (%d,%d,%v), want (3,0,true)", x, y, ok)
	}
}

// A cursor-movement sequence is followed wherever it lands, which is what a
// remote full-screen editor does between keystrokes.
func TestPaneCursorFollowsAMovementSequence(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	if err := fleet.sessions["web-01"].Resize(20, 5); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	// Three lines of output, then CUP to row 2, column 4 - one-based on the
	// wire, zero-based in the pane.
	fleet.sessions["web-01"].Emit("one\ntwo\nthree\n\x1b[2;4H")

	x, y, ok := a.paneCursor("web-01", 20, 5)
	if !ok || x != 3 || y != 1 {
		t.Fatalf("paneCursor() = (%d,%d,%v), want (3,1,true)", x, y, ok)
	}
}

// A pending line wrapped over several rows places the cursor on its last
// wrapped row, mapped through the terminal width.
func TestPaneCursorOnAWrappedPendingLine(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	if err := fleet.sessions["web-01"].Resize(20, 10); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	fleet.sessions["web-01"].Emit(strings.Repeat("x", 50))

	body := a.paneBody("web-01", 20, 10)
	if lines := strings.Split(body, "\n"); len(lines) != 3 {
		t.Fatalf("a 50-cell pending line should wrap to 3 rows:\n%q", body)
	}
	x, y, ok := a.paneCursor("web-01", 20, 10)
	if !ok || x != 10 || y != 2 {
		t.Fatalf("paneCursor() = (%d,%d,%v), want (10,2,true)", x, y, ok)
	}
}

// No cursor on a pane that is not connected: a dead pane must not pretend to
// take input.
func TestPaneCursorNeedsAConnection(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("$ ")

	if x, y, ok := a.paneCursor("web-01", 40, 5); ok {
		t.Fatalf("a disconnected pane grew a cursor at (%d,%d)", x, y)
	}
	// The trailing space is a blank cell like any other; the emulator does
	// not retain it.
	if body := a.paneBody("web-01", 40, 5); body != "$" {
		t.Fatalf("paneBody() = %q", body)
	}
}

// No cursor while scrolled back: the window shows history, and a cursor in
// history would claim input goes somewhere it does not.
func TestPaneCursorHidesWhileScrolledBack(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	for i := 0; i < 200; i++ {
		fleet.sessions["web-01"].Emitf("line-%03d\n", i)
	}
	a.paneIndex = 0 // scrolling targets the focused pane
	a = a.scrollBy(5)
	if a.scrollOffset("web-01") == 0 {
		t.Fatal("the pane did not scroll; the test would assert nothing")
	}

	if x, y, ok := a.paneCursor("web-01", 40, 5); ok {
		t.Fatalf("a scrolled-back pane reports a cursor at (%d,%d)", x, y)
	}
}

// A remote app that hides its cursor (CSI ?25l) is taken at its word: htop
// repainting must not blink a caret in the pane.
func TestPaneCursorHonoursTheHiddenCursorMode(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	fleet.sessions["web-01"].Emit("$ \x1b[?25l")

	if x, y, ok := a.paneCursor("web-01", 40, 5); ok {
		t.Fatalf("a hidden remote cursor was reported at (%d,%d)", x, y)
	}

	fleet.sessions["web-01"].Emit("\x1b[?25h")
	if _, _, ok := a.paneCursor("web-01", 40, 5); !ok {
		t.Fatal("the cursor did not come back when the remote showed it again")
	}
}

// A full-screen app owns the grid, so its cursor is the grid's own cell.
func TestPaneCursorOnTheAlternateScreen(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	if err := fleet.sessions["web-01"].Resize(20, 5); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	fleet.sessions["web-01"].Emit("\x1b[?1049h\x1b[3;6H")

	if !a.paneAltScreen("web-01") {
		t.Fatal("setup: the session is not on the alternate screen")
	}
	x, y, ok := a.paneCursor("web-01", 20, 5)
	if !ok || x != 5 || y != 2 {
		t.Fatalf("paneCursor() = (%d,%d,%v), want (5,2,true)", x, y, ok)
	}
}

// A cursor outside the drawn area is no cursor: a pane narrower than the
// terminal the host believes it has must not place a caret off its own body.
func TestPaneCursorOutsideTheBodyIsDropped(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	if err := fleet.sessions["web-01"].Resize(20, 5); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	fleet.sessions["web-01"].Emit(strings.Repeat("x", 15))

	if x, y, ok := a.paneCursor("web-01", 10, 5); ok {
		t.Fatalf("a cursor past the pane's last column was reported at (%d,%d)", x, y)
	}
}
