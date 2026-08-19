package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Wheel coalescing. A trackpad or a free-spinning wheel emits notches far
// faster than a frame takes to draw, so bubbletea - which delivers messages
// one at a time - hands Update a queue that is seconds deep. Worked off
// notch by notch, a reversal only takes effect once every stale notch ahead
// of it has been scrolled, which is exactly the backpressure rule the output
// side already follows: a flood must not stall the reaction to what the user
// is doing now (issue #313).
//
// The shape here is a one-shot window. The first notch of a gesture applies
// immediately - a slow reader clicking once still gets exactly one
// [wheelStep] - and opens a window; notches arriving inside it are added up
// and applied as a single jump when it closes. A reversal inside the window
// drops everything held, because all of it points the other way, and applies
// on the spot.

// wheelCoalesceWindow is how long notches are collected before the held jump
// is applied. Short enough to be invisible on a single notch, long enough
// that a flood collapses into a few renders instead of hundreds.
const wheelCoalesceWindow = 20 * time.Millisecond

// wheelFlushMsg closes a coalescing window.
type wheelFlushMsg struct{}

// wheelFlushCmd schedules the end of a coalescing window.
func wheelFlushCmd() tea.Cmd {
	return tea.Tick(wheelCoalesceWindow, func(time.Time) tea.Msg { return wheelFlushMsg{} })
}

// wheelTarget is what the pointer stood over: a pane index inside the grid,
// or a panel index inside the sidebar. It is compared as a whole, so notches
// held for one pane are never applied to another.
type wheelTarget struct {
	region Region
	index  int
}

// wheelScroll is a resolved amount of scrolling: signed notches - positive is
// back in time - for one target. The zero value scrolls nothing.
type wheelScroll struct {
	target  wheelTarget
	notches int
}

// wheelBatch is the coalescing state: which target the open window belongs
// to, which way the gesture is going, and how many notches are held.
type wheelBatch struct {
	target  wheelTarget
	dir     int // +1 or -1, the direction the last applied notch went; 0 when idle
	notches int // held, same-direction notches waiting for the flush
	open    bool
}

// wheelEvent is what one wheel notch resolves to: up to two scrolls that
// apply right now - a held batch a target change forces out early, and the
// notch's own immediate effect - plus whether a flush window must be opened.
// A notch folded into an open window produces the zero value.
type wheelEvent struct {
	flush      wheelScroll
	now        wheelScroll
	openWindow bool
}

// event folds one notch into the batch. delta is +1 or -1.
func (b *wheelBatch) event(target wheelTarget, delta int) wheelEvent {
	switch {
	case !b.open:
		// Idle: nothing is queued behind this notch, so it is the whole
		// gesture and applies unchanged. The window it opens is what catches
		// a flood, if one follows.
		*b = wheelBatch{target: target, dir: delta, open: true}
		return wheelEvent{now: wheelScroll{target, delta}, openWindow: true}

	case target != b.target:
		// The pointer moved to another pane or panel. What is held was aimed
		// at the old one and goes out before the new target starts its own
		// run.
		held := wheelScroll{b.target, b.notches}
		*b = wheelBatch{target: target, dir: delta, open: true}
		return wheelEvent{flush: held, now: wheelScroll{target, delta}}

	case delta != b.dir:
		// The reversal. Everything held points the way the user has just
		// stopped going, so applying it would scroll away from where they
		// are heading; it is dropped and the new direction takes effect on
		// this notch rather than after the backlog.
		b.dir, b.notches = delta, 0
		return wheelEvent{now: wheelScroll{target, delta}}

	default:
		// Same target, same direction: fold it in, render nothing.
		b.notches += delta
		return wheelEvent{}
	}
}

// flush closes the window. It returns what was held and whether another
// window must be opened: a window that collected something is a flood still
// running, so the next one keeps coalescing, while an empty one ends the
// gesture and hands the next notch back its immediate response.
func (b *wheelBatch) flush() (wheelScroll, bool) {
	if !b.open || b.notches == 0 {
		*b = wheelBatch{}
		return wheelScroll{}, false
	}
	held := wheelScroll{b.target, b.notches}
	b.notches = 0
	return held, true
}

// wheelTargetAt resolves a pointer position to what the wheel would scroll
// there, or reports that it scrolls nothing.
func (a App) wheelTargetAt(x, y int) (wheelTarget, bool) {
	switch a.layout.regionAt(x, y) {
	case RegionMain:
		if _, showing := a.mainPreview(); showing {
			// A preview is not a pane's scrollback; the wheel over it scrolls
			// nothing rather than a host the user cannot see.
			return wheelTarget{}, false
		}
		if index, ok := a.paneUnder(x, y); ok {
			return wheelTarget{region: RegionMain, index: index}, true
		}
	case RegionSidebar:
		heights := a.sidebarHeights()
		if panel, _, ok := sidebarPanelAt(heights, y-a.layout.Sidebar.Y); ok {
			return wheelTarget{region: RegionSidebar, index: panel}, true
		}
	}
	return wheelTarget{}, false
}

// applyWheelScroll performs a resolved scroll. Panes move by [wheelStep] per
// notch; a sidebar list moves its cursor one row per notch, in the opposite
// sign because scrolling down walks the list forward.
func (a App) applyWheelScroll(s wheelScroll) App {
	if s.notches == 0 {
		return a
	}
	switch s.target.region {
	case RegionMain:
		return a.scrollHostBy(s.target.index, s.notches*wheelStep)
	case RegionSidebar:
		panels := Panels()
		if s.target.index < 0 || s.target.index >= len(panels) {
			// The sidebar was rebuilt under a held batch; there is no panel
			// left to move.
			return a
		}
		return a.movePanelCursor(panels[s.target.index], -s.notches)
	}
	return a
}

// handleWheelFlush closes a coalescing window and applies what it collected.
func (a App) handleWheelFlush() (App, tea.Cmd) {
	held, reopen := a.wheel.flush()
	a = a.applyWheelScroll(held)
	if reopen {
		return a, wheelFlushCmd()
	}
	return a, nil
}
