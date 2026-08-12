package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The frame's caret rules (issue #292): the focused live pane owns the
// terminal cursor, a dialog takes it while one is open, and nothing else in
// the interface has one - a frame with no owner reports no cursor at all, so
// bubbletea hides the caret instead of leaving it wherever it last was.

// cursorApp is a two-host app with both sessions connected and the grid
// focused, which is the state a user types in.
func cursorApp(t *testing.T) (App, *fakeFleet) {
	t.Helper()
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")
	a = syncFleet(t, a)
	return focusGrid(t, a), fleet
}

// textUnderCursor is the rendered text ending at the frame's cursor cell: what
// the user has just typed, read off the frame itself rather than recomputed
// from the layout. It is how these tests check the caret against the pixels
// instead of against the arithmetic that placed it.
func textUnderCursor(t *testing.T, v tea.View, n int) string {
	t.Helper()
	if v.Cursor == nil {
		t.Fatal("the frame has no cursor")
	}
	lines := strings.Split(plain(v.Content), "\n")
	if v.Cursor.Position.Y < 0 || v.Cursor.Position.Y >= len(lines) {
		t.Fatalf("cursor row %d is outside the %d-line frame", v.Cursor.Position.Y, len(lines))
	}
	row := []rune(lines[v.Cursor.Position.Y])
	x := v.Cursor.Position.X
	if x < n || x > len(row) {
		t.Fatalf("cursor column %d has no %d columns of text before it in %q", x, n, string(row))
	}
	return string(row[x-n : x])
}

// The acceptance criterion: the caret sits on the cell the host's emulator
// reports, which is the cell right after what the host has echoed.
func TestViewCursorSitsWhereTheHostSaysItIs(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("$ echo hi")

	if got := textUnderCursor(t, a.View(), len("$ echo hi")); got != "$ echo hi" {
		t.Fatalf("the text ending at the cursor is %q, want %q", got, "$ echo hi")
	}
}

// A cursor-movement sequence moves the caret with it: the host, not the pane's
// idea of "after the last character", decides where typing lands.
func TestViewCursorFollowsAMovementSequence(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("abcdef\b\b\b")

	if got := textUnderCursor(t, a.View(), 3); got != "abc" {
		t.Fatalf("the text ending at the cursor is %q, want %q", got, "abc")
	}

	// An absolute move is followed just the same: row 3, column 5, one-based.
	fleet.sessions["web-01"].Emit("\r\n\x1b[3;5Hxy\x1b[3;5H")
	v := a.View()
	if got := textUnderCursor(t, v, 4); got != "    " {
		t.Fatalf("the cursor is not on the moved-to column: %q", got)
	}
	row := strings.Split(plain(v.Content), "\n")[v.Cursor.Position.Y]
	if !strings.Contains(row, "xy") {
		t.Fatalf("the cursor is not on the row the host moved to: %q", row)
	}
}

// The caret moves to the pane the keyboard moves to, and only one pane ever
// has it.
func TestViewCursorFollowsTheFocusedPane(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("one$ ")
	fleet.sessions["web-02"].Emit("two$ ")

	first := a.View().Cursor
	if got := textUnderCursor(t, a.View(), 5); got != "one$ " {
		t.Fatalf("the cursor is not in the first pane: %q", got)
	}

	a = pressKey(t, a, "alt+shift+right")
	if a.FocusedHost() != "web-02" {
		t.Fatalf("FocusedHost() = %q", a.FocusedHost())
	}
	second := a.View().Cursor
	if got := textUnderCursor(t, a.View(), 5); got != "two$ " {
		t.Fatalf("the cursor did not move to the second pane: %q", got)
	}
	if first.Position == second.Position {
		t.Fatalf("both panes put the cursor at %+v", first.Position)
	}
}

// Nothing focused, no caret: the sidebar takes no typing, so a caret left in a
// pane would point at a pane the keyboard does not reach (issue #292).
func TestViewHasNoCursorWithoutAFocusedPane(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("$ ")

	a = pressKey(t, a, "ctrl+]") // leave the grid
	if a.Focus() == AreaGrid {
		t.Fatal("setup: the grid still has the focus")
	}
	if c := a.View().Cursor; c != nil {
		t.Fatalf("an unfocused grid carries a cursor at %+v", c.Position)
	}
}

// The same, one layer up: an unfocused pane's body carries no cursor block
// either, so a screen full of connected hosts shows exactly one caret.
func TestUnfocusedPanesDrawNoCursor(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("$ ")
	fleet.sessions["web-02"].Emit("$ ")

	if body := a.paneBody("web-02", 40, 5); body != plain(body) {
		t.Fatalf("the unfocused pane paints a cursor:\n%q", body)
	}
	if c := a.View().Cursor; c == nil {
		t.Fatal("the focused pane lost its cursor")
	}
}

// A dialog owns the keyboard, so it owns the caret: the pane behind it gives
// the cursor up while the box is open and takes it back when it closes.
func TestViewCursorGoesToAnOpenDialog(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("$ ")

	pane := a.View().Cursor
	if pane == nil {
		t.Fatal("setup: the focused pane has no cursor")
	}

	a = a.beginSplit()
	m, ok := a.activeModal()
	if !ok {
		t.Fatal("the split prompt is not the active dialog")
	}
	_, x, y, want := a.renderModal(m)
	got := a.View().Cursor
	if got == nil {
		t.Fatal("the open dialog has no cursor")
	}
	if got.Position != want.Position {
		t.Fatalf("cursor at %+v, want the dialog's %+v", got.Position, want.Position)
	}
	if got.Position.X <= x || got.Position.Y <= y {
		t.Fatalf("cursor at %+v is not inside the box drawn at %d,%d", got.Position, x, y)
	}

	a = pressKey(t, a, "esc")
	if back := a.View().Cursor; back == nil || back.Position != pane.Position {
		t.Fatalf("the pane did not take its cursor back: %+v", back)
	}
}

// The status bar's prompts own the keyboard wherever the focus is, so they own
// the caret: it sits on the column the next character lands in, in the line
// that is drawn, and the pane behind them keeps none.
func TestViewCursorInTheStatusBarPrompts(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("$ ")
	pane := a.View().Cursor
	if pane == nil {
		t.Fatal("setup: the focused pane has no cursor")
	}

	// The command line, opened from the app level: a focused pane types a
	// literal colon to its host.
	cmd := pressKey(t, pressKey(t, a, "ctrl+]"), ":")
	if !cmd.cmdInput.Focused() {
		t.Fatal("the command line did not open")
	}
	cmd = typeInto(t, cmd, "uptime")
	c := cmd.View().Cursor
	if c == nil {
		t.Fatal("the command line has no cursor")
	}
	if got := textUnderCursor(t, cmd.View(), len(":uptime")); got != ":uptime" {
		t.Fatalf("the text ending at the cursor is %q, want %q", got, ":uptime")
	}
	if c.Position.Y != a.layout.StatusBar.Y {
		t.Fatalf("cursor row = %d, want the status bar's %d", c.Position.Y, a.layout.StatusBar.Y)
	}
	// Moving the caret inside the line moves the cursor with it.
	if moved := pressKey(t, cmd, "left").View().Cursor; moved.Position.X != c.Position.X-1 {
		t.Fatalf("cursor X = %d after left, want %d", moved.Position.X, c.Position.X-1)
	}

	// The scrollback search, which a focused pane opens with alt+/.
	search := typeInto(t, pressKey(t, a, "alt+/"), "err")
	if !search.searchInput.Focused() {
		t.Fatal("the search prompt did not open")
	}
	if got := textUnderCursor(t, search.View(), len("/err")); got != "/err" {
		t.Fatalf("the text ending at the cursor is %q, want %q", got, "/err")
	}
}

// The help overlay covers the frame, so nothing under it may keep a caret.
func TestViewHasNoCursorUnderTheHelpOverlay(t *testing.T) {
	a, fleet := cursorApp(t)
	fleet.sessions["web-01"].Emit("$ ")

	// Opened on the model rather than with "?": inside a focused pane every
	// key belongs to the host, and the pane keeping the focus is the point.
	a.showHelp = true
	if c := a.View().Cursor; c != nil {
		t.Fatalf("the help overlay left a cursor at %+v", c.Position)
	}
}

// A focused pane whose host is not connected takes no input, so it shows no
// caret - the dead-pane half of the rogue-caret defect.
func TestViewHasNoCursorForADeadPane(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("$ ")
	a = focusGrid(t, syncFleet(t, a))

	if c := a.View().Cursor; c != nil {
		t.Fatalf("a pending pane carries a cursor at %+v", c.Position)
	}

	fleet.fail(t, "web-01")
	a = syncFleet(t, a)
	if c := a.View().Cursor; c != nil {
		t.Fatalf("a failed pane carries a cursor at %+v", c.Position)
	}
}

// Scrolled back, the pane shows history: a caret there would claim input goes
// somewhere it does not.
func TestViewHasNoCursorWhileScrolledBack(t *testing.T) {
	a, fleet := cursorApp(t)
	for i := range 200 {
		fleet.sessions["web-01"].Emitf("line-%03d\n", i)
	}
	if a.View().Cursor == nil {
		t.Fatal("setup: the pane following the tail has no cursor")
	}

	a = a.scrollBy(5)
	if a.scrollOffset("web-01") == 0 {
		t.Fatal("the pane did not scroll; the test would assert nothing")
	}
	if c := a.View().Cursor; c != nil {
		t.Fatalf("a scrolled-back pane carries a cursor at %+v", c.Position)
	}
}

// A pane on another page is not on screen, so its cursor is not either.
func TestViewHasNoCursorForAPaneOffThePage(t *testing.T) {
	a := fleetApp(t, 4)
	a = resize(t, a, 100, 24) // one pane fits; the rest page
	g := a.Grid()
	if g.Pages < 2 {
		t.Fatalf("setup: the grid has %d pages", g.Pages)
	}
	if _, ok := a.focusedPaneRect(); !ok {
		t.Fatal("the focused pane on the shown page has no rect")
	}

	a.paneIndex = len(a.hostIDs()) - 1 // a host on the last page, page still 0
	if rect, ok := a.focusedPaneRect(); ok {
		t.Fatalf("a pane on another page reports the rect %+v", rect)
	}
	if c := a.View().Cursor; c != nil {
		t.Fatalf("a pane on another page carries a cursor at %+v", c.Position)
	}
}
