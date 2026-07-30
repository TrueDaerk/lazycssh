package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// Scrollback navigation and search. The buffer itself never notices any of
// this: scrolling is a render-time window into a snapshot, so a scrolled-back
// pane keeps buffering new output at full speed - the acceptance criterion of
// the navigation issue.

// scrollOffset is how far a pane is scrolled back, counted in wrapped lines
// from the bottom. Zero is the tail, which is where every pane starts and
// returns to.
func (a App) scrollOffset(id string) int {
	if a.scroll == nil {
		return 0
	}
	return a.scroll[id]
}

// FollowingTail reports whether a host's pane shows the newest output.
func (a App) FollowingTail(id string) bool { return a.scrollOffset(id) == 0 }

// scrollBy moves the focused pane's window by delta wrapped lines - positive
// is back in time. The offset is anchored at the bottom, so new output slides
// the window rather than the reader's position in it; the alternative, a
// top-anchored offset, drifts every time the bounded buffer drops a line.
func (a App) scrollBy(delta int) App {
	id := a.FocusedHost()
	if id == "" {
		return a
	}
	if a.scroll == nil {
		a.scroll = make(map[string]int)
	}
	a.scroll[id] = clamp(a.scroll[id]+delta, 0, a.maxScroll(id))
	return a
}

// scrollToTop shows the oldest retained output, where the dropped-lines marker
// lives.
func (a App) scrollToTop() App {
	id := a.FocusedHost()
	if id == "" {
		return a
	}
	if a.scroll == nil {
		a.scroll = make(map[string]int)
	}
	a.scroll[id] = a.maxScroll(id)
	return a
}

// scrollToBottom returns to the tail and thereby resumes following it.
func (a App) scrollToBottom() App {
	if a.scroll != nil {
		delete(a.scroll, a.FocusedHost())
	}
	return a
}

// scrollPage is how far ctrl+u / ctrl+d move: half the pane, like a pager, so
// two presses never skip a line the reader has not seen.
func (a App) scrollPage() int {
	_, h := a.paneExtent()
	return max(1, h/2)
}

// paneExtent is the inner text area of the focused pane: width, then height
// below the header.
func (a App) paneExtent() (width, height int) { return a.paneExtentAt(a.paneIndex) }

// paneExtentAt is the inner text area of the pane at index.
func (a App) paneExtentAt(index int) (width, height int) {
	if a.fullScreen {
		r := a.layout.Main
		return max(0, r.Width-2), max(0, r.Height-3)
	}
	cell, ok := a.grid().Cell(index)
	if !ok {
		return 0, 0
	}
	return max(0, cell.Width-2), max(0, cell.Height-3)
}

// maxScroll is the furthest back the focused pane can go at its current size.
func (a App) maxScroll(id string) int {
	w, h := a.paneExtent()
	return max(0, len(a.wrappedLines(id, w))-h)
}

// scrollHostBy moves the pane at index by delta wrapped lines, whichever pane
// has focus - the wheel scrolls what is under the pointer.
func (a App) scrollHostBy(index, delta int) App {
	id := a.hostIDAt(index)
	if id == "" {
		return a
	}
	if a.paneAltScreen(id) {
		// The remote app owns the screen; there is no scrollback view to move.
		return a
	}
	if a.scroll == nil {
		a.scroll = make(map[string]int)
	}
	w, h := a.paneExtentAt(index)
	limit := max(0, len(a.wrappedLines(id, w))-h)
	next := clamp(a.scroll[id]+delta, 0, limit)
	if next == 0 {
		delete(a.scroll, id)
		return a
	}
	a.scroll[id] = next
	return a
}

// Search. One term is shared by every pane: "which of my hosts printed this"
// is a question about the run, and a per-pane term would answer it forty
// times.

// SearchTerm is the active search, empty when none.
func (a App) SearchTerm() string { return a.searchTerm }

// Searching reports whether the search input has the keyboard.
func (a App) Searching() bool { return a.searchInput.Focused() }

// openSearch gives the keyboard to the search input.
func (a App) openSearch() App {
	a.searchInput.SetValue(a.searchTerm)
	a.searchInput.CursorEnd()
	a.searchInput.Focus()
	return a
}

// handleSearchKey drives the search input, which owns the keyboard while it is
// open, for the same reason the filter does: a term containing any letter must
// be typeable without acting on a pane.
func (a App) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		a.searchInput.Blur()
		a.searchTerm = strings.TrimSpace(a.searchInput.Value())
		if a.searchTerm == "" {
			return a.clearSearch(), nil
		}
		// Land on the newest match: the reader is almost always hunting the
		// error that just happened.
		return a.gotoMatch(a.newestMatch(a.FocusedHost())), nil
	case "esc":
		a.searchInput.Blur()
		a.searchInput.SetValue("")
		return a, nil
	}

	var cmd tea.Cmd
	a.searchInput, cmd = a.searchInput.Update(msg)
	return a, cmd
}

// clearSearch drops the term and the highlights.
func (a App) clearSearch() App {
	a.searchTerm = ""
	a.searchInput.SetValue("")
	return a
}

// matchLines returns the wrapped-line indices in a host's pane that contain
// the term, oldest first, at the pane's current width.
func (a App) matchLines(id string) []int {
	if a.searchTerm == "" {
		return nil
	}
	w, _ := a.paneExtent()
	var out []int
	for i, line := range a.wrappedLines(id, w) {
		if containsFold(ansi.Strip(line), a.searchTerm) {
			out = append(out, i)
		}
	}
	return out
}

// newestMatch is the last matching line, or -1.
func (a App) newestMatch(id string) int {
	m := a.matchLines(id)
	if len(m) == 0 {
		return -1
	}
	return m[len(m)-1]
}

// gotoMatch scrolls the focused pane so a wrapped line is on screen, roughly
// centred. A negative line - no match - changes nothing.
func (a App) gotoMatch(line int) App {
	id := a.FocusedHost()
	if id == "" || line < 0 {
		return a
	}
	w, h := a.paneExtent()
	total := len(a.wrappedLines(id, w))
	if a.scroll == nil {
		a.scroll = make(map[string]int)
	}
	a.scroll[id] = clamp(total-line-1-h/2, 0, max(0, total-h))
	return a
}

// stepMatch moves to the next match in a direction: negative is older, up the
// buffer; positive is newer. The step does not wrap - running out of matches
// in a direction stays put, exactly like pane movement.
func (a App) stepMatch(direction int) App {
	id := a.FocusedHost()
	matches := a.matchLines(id)
	if len(matches) == 0 {
		return a
	}

	current := a.currentLine(id)
	if direction < 0 {
		for i := len(matches) - 1; i >= 0; i-- {
			if matches[i] < current {
				return a.gotoMatch(matches[i])
			}
		}
		return a
	}
	for _, m := range matches {
		if m > current {
			return a.gotoMatch(m)
		}
	}
	return a
}

// currentLine is the wrapped line the focused pane's window is anchored on:
// the centre of what is visible.
func (a App) currentLine(id string) int {
	w, h := a.paneExtent()
	total := len(a.wrappedLines(id, w))
	return clamp(total-1-a.scrollOffset(id)-h/2, 0, max(0, total-1))
}

// searchReport says which hosts matched, for the cross-pane question "which
// of my 40 hosts printed this error". Run from the command line as
// /find <text>.
func (a App) searchReport() string {
	if a.searchTerm == "" {
		return ""
	}

	var matched []string
	for _, id := range a.hostIDs() {
		if len(a.matchAnywhere(id)) > 0 {
			matched = append(matched, id)
		}
	}
	if len(matched) == 0 {
		return fmt.Sprintf("%q matches no host", a.searchTerm)
	}

	names := strings.Join(matched, ", ")
	const most = 5
	if len(matched) > most {
		names = strings.Join(matched[:most], ", ") +
			fmt.Sprintf(" and %d more", len(matched)-most)
	}
	return fmt.Sprintf("%q found on %d/%d hosts: %s",
		a.searchTerm, len(matched), len(a.hostIDs()), names)
}

// matchAnywhere searches a host's raw scrollback rather than its wrapped
// pane, so the answer does not depend on which page the pane happens to be on
// or how wide it is.
func (a App) matchAnywhere(id string) []int {
	if a.searchTerm == "" || a.cfg.Fleet == nil {
		return nil
	}
	session, ok := a.cfg.Fleet.Session(id)
	if !ok {
		return nil
	}
	var out []int
	for i, line := range session.Scrollback().Lines() {
		if containsFold(sanitizeLine(line), a.searchTerm) {
			out = append(out, i)
		}
	}
	return out
}

// applyFind handles the /find meta command: it sets the shared term - every
// pane highlights it - and reports which hosts matched. Like the selection
// commands, nothing is sent to a host.
func (a App) applyFind(line string) (App, string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(line), metaPrefix+"find")
	if !ok || (rest != "" && !strings.HasPrefix(rest, " ")) {
		return a, "", false
	}

	term := strings.TrimSpace(rest)
	if term == "" {
		a = a.clearSearch()
		return a, "search cleared", true
	}
	a.searchTerm = term
	return a, a.searchReport(), true
}

// renderSearchLine draws the search prompt in place of the status bar while
// the term is being typed, naming the pane it will search first.
func (a App) renderSearchLine() string {
	label := a.FocusedHost()
	if label == "" {
		label = "no pane"
	}
	line := a.theme.Base.Render("/"+a.searchInput.Value()) + " " +
		a.theme.Muted.Render("→ search "+label+", highlight everywhere")
	return a.theme.StatusBar.
		Width(a.layout.Width).
		MaxHeight(StatusBarHeight).
		Render(line)
}

// scrollLabel is the status bar's word on a scrolled-back focused pane, empty
// while it follows the tail.
func (a App) scrollLabel() string {
	id := a.FocusedHost()
	if id == "" || a.scrollOffset(id) == 0 {
		return ""
	}
	return fmt.Sprintf("scrollback +%d", a.scrollOffset(id))
}

// containsFold is a case-insensitive substring test.
func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
