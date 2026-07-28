package ui

import "fmt"

// The **window** is which hosts are on screen right now. It is not the working
// set, which is which hosts a command is about - see wiki/core/working-sets.md.
// Paging the window never changes who receives a keystroke; that is the whole
// reason the two are separate.

// Page is the page of panes currently on screen, counted from zero.
func (a App) Page() int { return a.clampedPage(a.grid()) }

// Pages is how many pages the run needs at the current size.
func (a App) Pages() int { return a.grid().Pages }

// WindowHosts returns the host identifiers on screen right now, in order.
func (a App) WindowHosts() []string {
	g := a.grid()
	if g.PerPage <= 0 {
		return nil
	}
	first := a.clampedPage(g) * g.PerPage
	last := min(first+g.PerPage, len(a.cfg.Hosts))
	if first >= last {
		return nil
	}
	return append([]string(nil), a.cfg.Hosts[first:last]...)
}

// clampedPage keeps the page inside the range the current size allows. The
// terminal can shrink under a paged window - fewer panes per page, more pages -
// and the last page can vanish when hosts are closed.
func (a App) clampedPage(g Grid) int {
	if g.Pages <= 0 {
		return 0
	}
	return clamp(a.page, 0, g.Pages-1)
}

// pageBy moves the window by whole pages and puts the pane focus on the first
// host of the new page, so the pane that receives a keystroke is one the user
// can see.
func (a App) pageBy(delta int) App {
	g := a.grid()
	if g.Pages <= 1 || g.PerPage <= 0 {
		return a
	}

	next := clamp(a.clampedPage(g)+delta, 0, g.Pages-1)
	if next == a.clampedPage(g) {
		return a
	}
	a.page = next
	a.paneIndex = clamp(next*g.PerPage, 0, len(a.cfg.Hosts)-1)
	return a
}

// followFocus moves the window to the page the focused pane is on. Moving the
// focus off the edge of a page is a page turn, not a jump to a pane the user
// cannot see.
func (a App) followFocus() App {
	g := a.grid()
	if g.PerPage <= 0 {
		return a
	}
	a.page = g.Page(a.paneIndex)
	return a
}

// windowLabel renders the page indicator for the status bar, empty when
// everything fits on one page.
func (a App) windowLabel() string {
	g := a.grid()
	if g.Pages <= 1 {
		return ""
	}
	return fmt.Sprintf("page %d/%d", a.clampedPage(g)+1, g.Pages)
}
