package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// paneTarget and otherTarget are two distinct wheel targets for the batch
// tests, which never touch a layout.
var (
	paneTarget    = wheelTarget{region: RegionMain, index: 0}
	otherPane     = wheelTarget{region: RegionMain, index: 1}
	sidebarTarget = wheelTarget{region: RegionSidebar, index: 2}
)

// A single notch on an idle batch applies unchanged and opens a window: slow
// scrolling must keep its old step, one notch at a time.
func TestWheelBatchSingleNotchAppliesImmediately(t *testing.T) {
	var b wheelBatch

	e := b.event(paneTarget, +1)
	if e.now != (wheelScroll{paneTarget, +1}) {
		t.Fatalf("now = %+v, want one notch on the pane", e.now)
	}
	if e.flush.notches != 0 {
		t.Fatalf("flush = %+v, want nothing held on an idle batch", e.flush)
	}
	if !e.openWindow {
		t.Fatal("the first notch did not open a coalescing window")
	}

	// An empty window ends the gesture, so the next notch is immediate again.
	held, reopen := b.flush()
	if held.notches != 0 || reopen {
		t.Fatalf("flush() = %+v, %v; want an empty window that ends the gesture", held, reopen)
	}
	if e := b.event(paneTarget, +1); e.now.notches != +1 || !e.openWindow {
		t.Fatalf("event after an empty window = %+v, want an immediate notch", e)
	}
}

// A flood of same-direction notches collapses into one jump at the flush,
// rather than one scroll - and one render - per notch.
func TestWheelBatchCoalescesAFlood(t *testing.T) {
	var b wheelBatch

	first := b.event(paneTarget, -1)
	if first.now.notches != -1 {
		t.Fatalf("first notch = %+v, want an immediate one", first.now)
	}
	for range 99 {
		if e := b.event(paneTarget, -1); e != (wheelEvent{}) {
			t.Fatalf("a notch inside the window scrolled on its own: %+v", e)
		}
	}

	held, reopen := b.flush()
	if held != (wheelScroll{paneTarget, -99}) {
		t.Fatalf("flush() = %+v, want the 99 held notches as one jump", held)
	}
	if !reopen {
		t.Fatal("a window that collected notches did not open the next one")
	}
}

// The point of the issue: a reversal in the middle of a flood takes effect on
// the spot and the stale, opposite notches behind it are dropped.
func TestWheelBatchReversalDropsHeldNotches(t *testing.T) {
	var b wheelBatch

	b.event(paneTarget, +1)
	for range 200 {
		b.event(paneTarget, +1)
	}

	e := b.event(paneTarget, -1)
	if e.now != (wheelScroll{paneTarget, -1}) {
		t.Fatalf("the reversal = %+v, want it applied immediately", e.now)
	}
	if e.flush.notches != 0 {
		t.Fatalf("the reversal emitted %+v; the stale notches must be dropped, not applied", e.flush)
	}
	if e.openWindow {
		t.Fatal("the reversal opened a second window while one was in flight")
	}

	held, reopen := b.flush()
	if held.notches != 0 || reopen {
		t.Fatalf("flush() after a reversal = %+v, %v; want nothing left of the flood", held, reopen)
	}

	// And the new direction coalesces from there, not from the old one: the
	// empty flush closed the gesture, so the first of these is immediate
	// again and three are held.
	for range 4 {
		b.event(paneTarget, -1)
	}
	if held, _ := b.flush(); held != (wheelScroll{paneTarget, -3}) {
		t.Fatalf("after the reversal flush() = %+v, want -3", held)
	}
}

// Moving the pointer to another target while notches are held applies them to
// the target they were aimed at, never to the new one.
func TestWheelBatchTargetChangeFlushesToTheOldTarget(t *testing.T) {
	var b wheelBatch

	b.event(paneTarget, +1) // immediate, opens the window
	b.event(paneTarget, +1)
	b.event(paneTarget, +1)
	b.event(paneTarget, +1)

	e := b.event(otherPane, +1)
	if e.flush != (wheelScroll{paneTarget, +3}) {
		t.Fatalf("flush = %+v, want the 3 held notches on the pane they were aimed at", e.flush)
	}
	if e.now != (wheelScroll{otherPane, +1}) {
		t.Fatalf("now = %+v, want the notch on the new target", e.now)
	}

	// The sidebar is a different target even at the same index arithmetic.
	e = b.event(sidebarTarget, +1)
	if e.flush.target != otherPane {
		t.Fatalf("flush target = %+v, want the pane the notches were held for", e.flush.target)
	}
}

// A stray flush - the window already closed, or the tick outlived it - is a
// no-op rather than a phantom scroll.
func TestWheelBatchStrayFlushIsANoop(t *testing.T) {
	var b wheelBatch
	held, reopen := b.flush()
	if held.notches != 0 || reopen {
		t.Fatalf("flush() on an idle batch = %+v, %v", held, reopen)
	}
}

// wheelFlush drives the coalescing window's end through Update, the way the
// scheduled tick does.
func wheelFlush(t *testing.T, a App) App {
	t.Helper()
	model, _ := a.Update(wheelFlushMsg{})
	return model.(App)
}

// End to end through Update: a flood of notches scrolls once, at the flush,
// and by the whole amount.
func TestWheelFloodScrollsOnceAtTheFlush(t *testing.T) {
	a, _ := scrollApp(t, 500)
	a = pressKey(t, a, "ctrl+]") // focus away from the grid
	cell, _ := a.Grid().Cell(0)
	x, y := cell.X+2, cell.Y+3

	a = wheel(t, a, x, y, true)
	after := a.scrollOffset("web-01")
	if after != wheelStep {
		t.Fatalf("one notch scrolled %d lines, want %d", after, wheelStep)
	}

	for range 9 {
		a = wheel(t, a, x, y, true)
	}
	if got := a.scrollOffset("web-01"); got != after {
		t.Fatalf("notches inside the window scrolled to %d, want them held at %d", got, after)
	}

	a = wheelFlush(t, a)
	if got := a.scrollOffset("web-01"); got != after+9*wheelStep {
		t.Fatalf("the flush scrolled to %d, want %d", got, after+9*wheelStep)
	}
}

// End to end: reversing after a long flood moves the pane the other way on
// the reversing notch, not after the backlog.
func TestWheelReversalTakesEffectImmediately(t *testing.T) {
	a, _ := scrollApp(t, 2000)
	a = pressKey(t, a, "ctrl+]")
	cell, _ := a.Grid().Cell(0)
	x, y := cell.X+2, cell.Y+3

	for range 300 {
		a = wheel(t, a, x, y, true)
	}
	a = wheelFlush(t, a)
	back := a.scrollOffset("web-01")
	if back == 0 {
		t.Fatal("the flood did not scroll the pane back at all")
	}

	// One notch the other way, with the flood still notionally queued.
	a = wheel(t, a, x, y, false)
	if got := a.scrollOffset("web-01"); got != back-wheelStep {
		t.Fatalf("after the reversal the offset is %d, want %d - the reversal was queued behind the flood",
			got, back-wheelStep)
	}
}

// The sidebar's wheel coalesces the same way: a flood moves the cursor once,
// by the held rows.
func TestWheelCoalescesTheSidebarCursor(t *testing.T) {
	a, _ := groupsStoreApp(t,
		savedGroup("a", "h1"), savedGroup("b", "h2"), savedGroup("c", "h3"),
		savedGroup("d", "h4"), savedGroup("e", "h5"))

	heights := SidebarHeights(a.Layout().Sidebar.Height, len(Panels()), int(PanelGroups))
	y := heights[0] + 2 // inside the Groups box

	a = wheel(t, a, 2, y, false)
	if a.GroupCursor() != 1 {
		t.Fatalf("GroupCursor() = %d after one notch, want 1", a.GroupCursor())
	}
	a = wheel(t, a, 2, y, false)
	a = wheel(t, a, 2, y, false)
	if a.GroupCursor() != 1 {
		t.Fatalf("GroupCursor() = %d; notches inside the window must be held", a.GroupCursor())
	}
	a = wheelFlush(t, a)
	if a.GroupCursor() != 3 {
		t.Fatalf("GroupCursor() = %d after the flush, want 3", a.GroupCursor())
	}
}

// The first notch of a gesture asks for a window; a notch folded into an open
// one must not schedule a second tick.
func TestWheelSchedulesOneWindowPerGesture(t *testing.T) {
	a, _ := scrollApp(t, 500)
	cell, _ := a.Grid().Cell(0)
	x, y := cell.X+2, cell.Y+3

	model, cmd := a.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if cmd == nil {
		t.Fatal("the first notch did not schedule a flush")
	}
	a = model.(App)

	model, cmd = a.Update(tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp})
	if cmd != nil {
		t.Fatal("a notch inside the window scheduled a second flush")
	}
	a = model.(App)

	// The flush that finds something held keeps the coalescing going; the one
	// after it, with an idle wheel, does not.
	model, cmd = a.Update(wheelFlushMsg{})
	if cmd == nil {
		t.Fatal("a flush that collected notches did not open the next window")
	}
	if _, cmd = model.(App).Update(wheelFlushMsg{}); cmd != nil {
		t.Fatal("an empty flush kept the coalescing running")
	}
}
