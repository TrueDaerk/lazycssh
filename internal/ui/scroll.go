package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/charmbracelet/x/ansi"
)

// Scrollback navigation and search. The buffer itself never notices any of
// this: scrolling is a render-time window into a snapshot, so a scrolled-back
// pane keeps buffering new output at full speed - the acceptance criterion of
// the navigation issue.

// scrollOffset is how far a pane is scrolled back, counted in virtual lines
// (history plus screen rows) from the bottom. Zero is the tail, which is
// where every pane starts and returns to.
func (a App) scrollOffset(id string) int {
	if a.scroll == nil {
		return 0
	}
	return a.scroll[id]
}

// FollowingTail reports whether a host's pane shows the newest output.
func (a App) FollowingTail(id string) bool { return a.scrollOffset(id) == 0 }

// scrollBy moves the focused pane's window by delta lines - positive
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
	if a.FullScreen() {
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
	_, h := a.paneExtent()
	return max(0, a.virtualLineCount(id)-h)
}

// scrollHostBy moves the pane at index by delta lines, whichever pane
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
	_, h := a.paneExtentAt(index)
	limit := max(0, a.virtualLineCount(id)-h)
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

// MatchPosition is where the focused pane's search cursor stands: the 1-based
// rank of the current match and the number of matches in that pane. Both are
// zero when nothing matches, which is what the status bar renders as "no
// match".
func (a App) MatchPosition() (position, total int) {
	id := a.FocusedHost()
	matches := a.matchLines(id)
	if len(matches) == 0 {
		return 0, 0
	}
	current, ok := a.matchAt[id]
	if !ok {
		return 0, len(matches)
	}
	for i, line := range matches {
		if line == current {
			return i + 1, len(matches)
		}
	}
	return 0, len(matches)
}

// CurrentMatch is the virtual line the focused pane's search cursor sits on, or
// -1 when the search has not landed on one.
func (a App) CurrentMatch() int { return a.matchCursor(a.FocusedHost()) }

// matchCursor is a host's current match line, or -1.
func (a App) matchCursor(id string) int {
	if id == "" || a.searchTerm == "" {
		return -1
	}
	line, ok := a.matchAt[id]
	if !ok {
		return -1
	}
	return line
}

// handleSearchKey drives the search input, which owns the keyboard while it is
// open, for the same reason the filter does: a term containing any letter must
// be typeable without acting on a pane.
func (a App) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.PromptSubmit):
		a.searchInput.Blur()
		term := strings.TrimSpace(a.searchInput.Value())
		if term == "" {
			return a.exitSearch(), nil
		}
		a.searchTerm = term
		// A new term starts a new hunt: the old cursor pointed into the old
		// term's matches.
		a.matchAt = nil
		// Land on the newest match: the reader is almost always hunting the
		// error that just happened. No match leaves every pane where it was;
		// the status bar says "no match".
		return a.gotoMatch(a.newestMatch(a.FocusedHost())), nil
	case key.Matches(msg, a.keys.PromptCancel):
		// esc here abandons the editing, not the search: whatever term was
		// live before the input opened is still live, with its highlight and
		// its scroll position. A second esc leaves the search itself.
		a.searchInput.Blur()
		a.searchInput.SetValue("")
		return a, nil
	}

	cmd := a.searchInput.Update(msg)
	return a, cmd
}

// handleSearchModeKey is the search's own key scope, live wherever plain
// letters are commands - the sidebar and the broadcast bar's view mode, never a
// focused pane's terminal.
//
// `/` opens the input for the focused pane; it does nothing without one, so the
// key is not swallowed on an empty run. n, N and esc are live only while a term
// is: outside a search `n` is still "connect a new host" and esc still belongs
// to whatever else answers it.
func (a App) handleSearchModeKey(msg tea.KeyPressMsg) (App, bool) {
	if key.Matches(msg, a.keys.SearchOpen) {
		if a.FocusedHost() == "" {
			return a, false
		}
		return a.openSearch(), true
	}
	if a.searchTerm == "" {
		return a, false
	}
	switch {
	case key.Matches(msg, a.keys.MatchOlder):
		return a.stepMatch(-1), true
	case key.Matches(msg, a.keys.MatchNewer):
		return a.stepMatch(+1), true
	case key.Matches(msg, a.keys.SearchLeave):
		return a.exitSearch(), true
	}
	return a, false
}

// clearSearch drops the term, the highlights and the match cursor. The scroll
// positions stay where the search put them: this is "stop highlighting", not
// "undo the search".
func (a App) clearSearch() App {
	a.searchTerm = ""
	a.searchInput.SetValue("")
	a.matchAt, a.searchAnchor = nil, nil
	return a.dropSearchCache()
}

// exitSearch is what esc means once a term is live: the highlight goes, the
// cursor goes, and every pane the search scrolled goes back to where its window
// was before it did (issue #250). A pane the user scrolled themselves after the
// jump is not restored - the anchor is recorded once, at the first jump, and
// dropped here.
func (a App) exitSearch() App {
	for id, offset := range a.searchAnchor {
		if a.scroll == nil {
			break
		}
		if offset == 0 {
			delete(a.scroll, id)
			continue
		}
		a.scroll[id] = offset
	}
	return a.clearSearch()
}

// matchLines returns the virtual-line indices in a host's pane that contain
// the term, oldest first. It runs once per rendered frame (the status bar's
// counter) and on every n/N step, so it must not materialize the virtual line
// space: the history portion comes from the incremental match cache
// (searchcache.go, issue #278), and only the marker and the screen rows — a
// pane's height at most — are checked in place.
func (a App) matchLines(id string) []int {
	if a.searchTerm == "" {
		return nil
	}
	c, ok := a.paneContent(id)
	if !ok {
		return nil
	}
	hist := a.histMatches(id, c.histLen)
	out := make([]int, 0, len(hist))
	off := 0
	if c.marker {
		if containsFold(droppedMarkerText, a.searchTerm) {
			out = append(out, 0)
		}
		off = 1
	}
	for _, i := range hist {
		out = append(out, off+i)
	}
	for i, line := range c.screen {
		if containsFold(ansi.Strip(line), a.searchTerm) {
			out = append(out, c.screenTop+i)
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

// gotoMatch scrolls the focused pane so a virtual line is on screen, roughly
// centred, and records the line as the pane's current match. A negative line -
// no match - changes nothing, so a search that finds nothing leaves the
// viewport exactly where it was.
func (a App) gotoMatch(line int) App {
	id := a.FocusedHost()
	if id == "" || line < 0 {
		return a
	}
	_, h := a.paneExtent()
	total := a.virtualLineCount(id)
	if a.scroll == nil {
		a.scroll = make(map[string]int)
	}
	if a.searchAnchor == nil {
		a.searchAnchor = make(map[string]int)
	}
	if _, recorded := a.searchAnchor[id]; !recorded {
		// Where the window sat before the search moved it, so esc can put it
		// back. Recorded once per pane per search.
		a.searchAnchor[id] = a.scroll[id]
	}
	a.scroll[id] = clamp(total-line-1-h/2, 0, max(0, total-h))
	if a.matchAt == nil {
		a.matchAt = make(map[string]int)
	}
	a.matchAt[id] = line
	return a
}

// stepMatch moves the search cursor in a direction: negative is older, up the
// buffer; positive is newer. The step does not wrap - running out of matches in
// a direction stays put, exactly like pane movement, and the counter in the
// status bar shows which end the cursor is at.
//
// Without a cursor yet - `n` pressed on a term set by /find, which highlights
// without jumping - the first step lands on the newest match, the same place
// enter lands.
func (a App) stepMatch(direction int) App {
	id := a.FocusedHost()
	matches := a.matchLines(id)
	if id == "" || len(matches) == 0 {
		return a
	}

	current := a.matchCursor(id)
	if current < 0 {
		return a.gotoMatch(matches[len(matches)-1])
	}
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

// searchReport says which hosts matched, for the cross-pane question "which
// of my 40 hosts printed this error". Run from the command line as
// /find <text>.
func (a App) searchReport() string {
	if a.searchTerm == "" {
		return ""
	}

	var matched []string
	for _, id := range nonHoles(a.hostIDs()) {
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
		a.searchTerm, len(matched), len(nonHoles(a.hostIDs())), names)
}

// matchAnywhere searches a host's whole retained content — history and
// screen — so the answer does not depend on which page the pane happens to be
// on.
func (a App) matchAnywhere(id string) []int {
	if a.searchTerm == "" {
		return nil
	}
	t := a.paneTerminal(id)
	if t == nil {
		return nil
	}
	var out []int
	for i, line := range strings.Split(t.Text(), "\n") {
		if containsFold(line, a.searchTerm) {
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
		a = a.exitSearch()
		return a, "search cleared", true
	}
	a.searchTerm = term
	// The report answers "which hosts", so nothing is scrolled; the cursor
	// starts unset and the first n/N lands on the newest match.
	a.matchAt = nil
	return a, a.searchReport(), true
}

// searchLabel is the status bar's word on the active search: the term and where
// the focused pane's cursor stands in its matches, "no match" when that pane
// holds none. Empty when no term is live.
func (a App) searchLabel() string {
	if a.searchTerm == "" {
		return ""
	}
	position, total := a.MatchPosition()
	switch {
	case total == 0:
		return fmt.Sprintf("search %q no match", a.searchTerm)
	case position == 0:
		// A term set by /find, not yet stepped into on this pane.
		return fmt.Sprintf("search %q %d matches", a.searchTerm, total)
	default:
		return fmt.Sprintf("search %q %d/%d", a.searchTerm, position, total)
	}
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
