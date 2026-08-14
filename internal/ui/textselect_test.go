package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// selApp is a two-host grid-focused app with known output in the first pane.
func selApp(t *testing.T) (App, *fakeFleet, Rect) {
	t.Helper()
	a, fleet := typingApp(t, "web-01", "web-02")
	fleet.sessions["web-01"].Emit("alpha one\nbravo two\ncharlie three\n")
	fleet.sessions["web-02"].Emit("other pane text\n")
	body, ok := a.paneBodyRect(0)
	if !ok {
		t.Fatal("setup: pane 0 has no body rect")
	}
	return a, fleet, body
}

// mouse drives one mouse message through the model.
func mouse(t *testing.T, a App, msg tea.Msg) App {
	t.Helper()
	model, _ := a.Update(msg)
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	return next
}

// drag presses at (x1,y1), moves to (x2,y2) and releases there.
func drag(t *testing.T, a App, x1, y1, x2, y2 int) App {
	t.Helper()
	next, _ := dragCmd(t, a, x1, y1, x2, y2)
	return next
}

// dragCmd is drag, but also returns the release's command - copy-on-select
// (issue #302) means that release can itself carry a clipboard write.
func dragCmd(t *testing.T, a App, x1, y1, x2, y2 int) (App, tea.Cmd) {
	t.Helper()
	a = mouse(t, a, tea.MouseClickMsg{X: x1, Y: y1, Button: tea.MouseLeft})
	a = mouse(t, a, tea.MouseMotionMsg{X: x2, Y: y2, Button: tea.MouseLeft})
	model, cmd := a.Update(tea.MouseReleaseMsg{X: x2, Y: y2, Button: tea.MouseLeft})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	return next, cmd
}

// ctrlC presses ctrl+c and returns the model plus the rendered command
// message, which is the clipboard content for a copy.
func ctrlC(t *testing.T, a App) (App, string) {
	t.Helper()
	return pressCopyKey(t, a, tea.ModCtrl)
}

// superC presses super+c (cmd+c on macOS terminals that forward it) and
// returns the model plus the rendered clipboard content, like ctrlC.
func superC(t *testing.T, a App) (App, string) {
	t.Helper()
	return pressCopyKey(t, a, tea.ModSuper)
}

func pressCopyKey(t *testing.T, a App, mod tea.KeyMod) (App, string) {
	t.Helper()
	model, cmd := a.Update(tea.KeyPressMsg{Code: 'c', Mod: mod})
	next := model.(App)
	if cmd == nil {
		return next, ""
	}
	return next, fmt.Sprint(cmd())
}

// The acceptance criterion of issue #149: drag over a pane builds a
// selection, ctrl+c copies it (ANSI-free) and clears it, and no interrupt
// reaches any host.
func TestDragSelectsAndCtrlCCopies(t *testing.T) {
	a, fleet, body := selApp(t)

	a = drag(t, a, body.X, body.Y, body.X+4, body.Y+1)
	if !a.TextSelectionActive() {
		t.Fatal("the drag did not build a selection")
	}

	a, clip := ctrlC(t, a)
	if !strings.Contains(clip, "alpha one") || !strings.Contains(clip, "bravo") {
		t.Fatalf("clipboard = %q, want the two selected rows", clip)
	}
	if strings.Contains(clip, "charlie") {
		t.Fatalf("clipboard = %q reaches past the selection", clip)
	}
	if strings.Contains(clip, "\x1b[3") {
		t.Fatalf("clipboard carries ANSI styling: %q", clip)
	}
	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("web-01 received %q, want no interrupt", got)
	}
	if a.TextSelectionActive() {
		t.Fatal("the copy did not clear the selection")
	}
	if !strings.Contains(a.lastDelivery, "copied 2 lines from web-01") {
		t.Fatalf("lastDelivery = %q", a.lastDelivery)
	}
}

// Without a selection ctrl+c stays the interrupt for the focused host.
func TestCtrlCWithoutSelectionInterrupts(t *testing.T) {
	a, fleet, _ := selApp(t)

	a, _ = ctrlC(t, a)
	if got := fleet.sessions["web-01"].Written(); got != "\x03" {
		t.Fatalf("web-01 received %q, want the interrupt", got)
	}
	_ = a
}

// A click without a drag is not a selection, and the next ctrl+c interrupts.
func TestClickWithoutDragIsNoSelection(t *testing.T) {
	a, fleet, body := selApp(t)

	a = mouse(t, a, tea.MouseClickMsg{X: body.X + 2, Y: body.Y, Button: tea.MouseLeft})
	a = mouse(t, a, tea.MouseReleaseMsg{X: body.X + 2, Y: body.Y, Button: tea.MouseLeft})
	if a.TextSelectionActive() {
		t.Fatal("a plain click built a selection")
	}
	a, _ = ctrlC(t, a)
	if got := fleet.sessions["web-01"].Written(); got != "\x03" {
		t.Fatalf("web-01 received %q, want the interrupt", got)
	}
}

// A drag that leaves the pane clamps to its body: nothing from a neighbour
// pane can end up in the selection.
func TestDragClampsToTheOwningPane(t *testing.T) {
	a, _, body := selApp(t)

	// Far beyond the pane in both directions.
	a = drag(t, a, body.X, body.Y, body.X+body.Width+40, body.Y+body.Height+20)
	if !a.TextSelectionActive() {
		t.Fatal("the drag did not build a selection")
	}
	_, clip := ctrlC(t, a)
	if strings.Contains(clip, "other pane") {
		t.Fatalf("clipboard = %q crossed into the neighbour pane", clip)
	}
	if !strings.Contains(clip, "alpha one") {
		t.Fatalf("clipboard = %q misses the owning pane's text", clip)
	}
}

// esc clears the selection; the ctrl+c after it is the interrupt again.
func TestEscClearsTheSelection(t *testing.T) {
	a, fleet, body := selApp(t)

	a = drag(t, a, body.X, body.Y, body.X+4, body.Y)
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEscape})
	if a.TextSelectionActive() {
		t.Fatal("esc did not clear the selection")
	}
	a, _ = ctrlC(t, a)
	if got := fleet.sessions["web-01"].Written(); !strings.HasSuffix(got, "\x03") {
		t.Fatalf("web-01 received %q, want the interrupt after esc", got)
	}
}

// Leaving the grid is a focus change and clears the selection.
func TestFocusChangeClearsTheSelection(t *testing.T) {
	a, _, body := selApp(t)

	a = drag(t, a, body.X, body.Y, body.X+4, body.Y)
	a = pressKey(t, a, "ctrl+]")
	if a.TextSelectionActive() {
		t.Fatal("leaving the grid kept the selection")
	}
}

// Scrolling the pane under a selection clears it - the decided behavior.
func TestScrollClearsTheSelection(t *testing.T) {
	a, fleet, body := selApp(t)
	fleet.sessions["web-01"].Flood(200) // enough scrollback to scroll into

	a = drag(t, a, body.X, body.Y, body.X+4, body.Y)
	if !a.TextSelectionActive() {
		t.Fatal("setup: no selection")
	}
	a = pressKey(t, a, "shift+pgup")
	if a.TextSelectionActive() {
		t.Fatal("scrolling kept the selection")
	}
}

// A grid change - full screen here - invalidates the selection.
func TestGridChangeClearsTheSelection(t *testing.T) {
	a, _, body := selApp(t)

	a = drag(t, a, body.X, body.Y, body.X+4, body.Y)
	a = pressKey(t, a, "alt+z")
	if a.TextSelectionActive() {
		t.Fatal("the zoom kept the selection")
	}
}

// The highlight must not shift the layout by a cell: every rendered line has
// the same width with and without a selection.
func TestHighlightDoesNotShiftTheLayout(t *testing.T) {
	a, _, body := selApp(t)

	before := strings.Split(plain(a.View().Content), "\n")
	a = drag(t, a, body.X+1, body.Y, body.X+5, body.Y+1)
	after := strings.Split(plain(a.View().Content), "\n")

	if len(before) != len(after) {
		t.Fatalf("line count changed: %d != %d", len(before), len(after))
	}
	for i := range before {
		if len([]rune(before[i])) != len([]rune(after[i])) {
			t.Fatalf("line %d changed width:\n%q\n%q", i, before[i], after[i])
		}
	}
	// The highlight itself is on screen: reverse video marks the selection.
	if !strings.Contains(a.View().Content, "\x1b[7m") {
		t.Fatal("no reverse-video highlight in the render")
	}
}

// New output arriving under a tail-following selection redraws beneath it
// without corrupting the render or dropping the selection.
func TestNewOutputUnderASelection(t *testing.T) {
	a, fleet, body := selApp(t)

	a = drag(t, a, body.X, body.Y, body.X+4, body.Y+1)
	fleet.sessions["web-01"].Emit("late arrival\n")
	if !a.TextSelectionActive() {
		t.Fatal("new output dropped the selection")
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, "late arrival") {
		t.Fatalf("the new output is not rendered:\n%s", view)
	}
}

// Copy-on-select (issue #302): releasing a drag copies the selection right
// away, so cmd+v works without cmd+c ever having to reach the app - and the
// selection stays live and highlighted, exactly like the existing ctrl+c path
// leaves it before it is copied.
func TestReleaseAfterDragCopiesImmediately(t *testing.T) {
	a, fleet, body := selApp(t)

	a, cmd := dragCmd(t, a, body.X, body.Y, body.X+4, body.Y+1)
	if cmd == nil {
		t.Fatal("release after a drag did not emit a clipboard command")
	}
	clip := fmt.Sprint(cmd())
	if !strings.Contains(clip, "alpha one") || !strings.Contains(clip, "bravo") {
		t.Fatalf("clipboard = %q, want the two selected rows", clip)
	}
	if strings.Contains(clip, "\x1b[3") {
		t.Fatalf("clipboard carries ANSI styling: %q", clip)
	}
	if !a.TextSelectionActive() {
		t.Fatal("copy-on-select cleared the selection; it should stay live")
	}
	if !strings.Contains(a.lastDelivery, "copied 2 lines from web-01") {
		t.Fatalf("lastDelivery = %q", a.lastDelivery)
	}
	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("web-01 received %q, want no interrupt", got)
	}
}

// A release without a drag is a plain click: no selection, so no copy.
func TestReleaseWithoutDragDoesNotCopy(t *testing.T) {
	a, _, body := selApp(t)

	a = mouse(t, a, tea.MouseClickMsg{X: body.X + 2, Y: body.Y, Button: tea.MouseLeft})
	model, cmd := a.Update(tea.MouseReleaseMsg{X: body.X + 2, Y: body.Y, Button: tea.MouseLeft})
	if cmd != nil {
		t.Fatalf("a plain click emitted a clipboard command: %v", fmt.Sprint(cmd()))
	}
	next := model.(App)
	if next.TextSelectionActive() {
		t.Fatal("a plain click built a selection")
	}
}

// super+c copies a live selection exactly like ctrl+c (issue #302).
func TestSuperCCopiesTheSelectionLikeCtrlC(t *testing.T) {
	a, fleet, body := selApp(t)

	a = drag(t, a, body.X, body.Y, body.X+4, body.Y+1)
	a, clip := superC(t, a)
	if !strings.Contains(clip, "alpha one") || !strings.Contains(clip, "bravo") {
		t.Fatalf("clipboard = %q, want the two selected rows", clip)
	}
	if a.TextSelectionActive() {
		t.Fatal("super+c did not clear the selection")
	}
	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("web-01 received %q, want no interrupt", got)
	}
}

// Without a selection, super+c must stay inert: unlike ctrl+c it has no
// interrupt fallback, so it must not reach the host as a keystroke either.
func TestSuperCWithoutSelectionHasNoSideEffect(t *testing.T) {
	a, fleet, _ := selApp(t)

	a, clip := superC(t, a)
	if clip != "" {
		t.Fatalf("clipboard = %q, want nothing copied", clip)
	}
	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("web-01 received %q, want no interrupt and no keystroke", got)
	}
}
