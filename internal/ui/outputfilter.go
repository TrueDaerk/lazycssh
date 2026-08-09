package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// The output filter: `f` asks for a pattern and the grid then shows only the
// panes whose recent output holds it (issue #255). With forty panes on screen,
// "which of these said error" is a question the eye answers badly and a filter
// answers in one keystroke.
//
// It is a *view*, not a selection. Hiding a pane never changes who receives a
// keystroke: the broadcast limit is computed as if the filter were off (see
// [App.syncBroadcastLimit]), so a command typed under a filter reaches the same
// hosts it would have reached without one. The status bar says which filter is
// in force and how many hosts match, and the overflow footer says how many are
// hidden, because a filtered grid that reads as the whole run is exactly the
// misreading this tool exists to prevent.
//
// Matching is a **case-insensitive substring** - not a regexp. A filter is
// typed in a hurry to find the odd machine out; `error` should not have to be
// escaped, and a half-typed pattern should narrow the grid rather than fail to
// compile.
//
// The window matched is the output *since the last command-line send* - the
// same watermarks the Output diff panel compares from (see diffpanel.go) - so
// "which hosts failed that command" is not answered by an hour of older
// scrollback. A host the send never reached has no watermark, and the last
// [filterScanLines] lines are read instead.

// filterScanLines is how far back the filter reads for a host with no command
// watermark. Enough to cover a screenful of output several times over, small
// enough that re-evaluating forty hosts on every output message stays cheap.
const filterScanLines = 200

// OutputFilter is the active filter pattern, empty when the grid shows
// everything.
func (a App) OutputFilter() string { return a.outputFilter }

// FilterPromptOpen reports whether the filter prompt has the keyboard.
func (a App) FilterPromptOpen() bool { return a.filterInput.Focused() }

// FilterMatches reports whether a host's recent output matches the active
// filter. Without a filter every host matches: nothing is hidden.
func (a App) FilterMatches(id string) bool {
	if a.outputFilter == "" {
		return true
	}
	return a.filterMatch[id]
}

// beginOutputFilter opens the pattern prompt. It opens empty - enter on an
// empty prompt clears the filter, which is the promised way out - even while a
// filter is in force, so the fastest thing to do from here is always "show
// everything again".
func (a App) beginOutputFilter() App {
	a.filterInput.SetValue("")
	a.filterInput.Focus()
	return a
}

// handleFilterKey drives the pattern prompt, which owns the keyboard while it
// is open. enter applies what was typed - empty clears - and esc clears too:
// the filter hides panes, so the key that means "get me out of here" must not
// leave one in force.
func (a App) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.PromptSubmit):
		pattern := strings.TrimSpace(a.filterInput.Value())
		a.filterInput.SetValue("")
		a.filterInput.Blur()
		return a.applyOutputFilter(pattern)
	case key.Matches(msg, a.keys.PromptCancel):
		a.filterInput.SetValue("")
		a.filterInput.Blur()
		return a.applyOutputFilter("")
	}

	var cmd tea.Cmd
	a.filterInput, cmd = a.filterInput.Update(msg)
	return a, cmd
}

// applyOutputFilter sets the pattern - empty clears the filter - re-evaluates
// the matches and re-tiles for what is left.
//
// The focus is kept by identity and then clamped: a filter that hides the
// focused pane moves the focus to a pane the user can see, and clearing the
// filter puts it back on the host it was on. The focus must never sit on a
// hidden pane - that is the pane the next keystroke is aimed at.
func (a App) applyOutputFilter(pattern string) (App, tea.Cmd) {
	focused := a.FocusedHost()
	a.outputFilter = pattern
	a = a.syncFilterMatches()
	a.page = 0
	a = a.compactHoles().refocus(focused).resetGridSlots().followFocus().
		syncBroadcastLimit().syncFocusTarget()
	return a, gridChanged()
}

// syncFilterMatches re-reads every host's recent output and records what
// matches. It runs inside Update, never in a render helper: the match set is a
// model field for the same reason the fleet snapshot is one - two reads of a
// live scrollback inside one frame may disagree, and a host list that changes
// under an index is how a keystroke lands on the wrong machine (issues #135,
// #136).
func (a App) syncFilterMatches() App {
	if a.outputFilter == "" {
		a.filterMatch = nil
		return a
	}
	needle := strings.ToLower(a.outputFilter)
	ids := a.fleetIDs()
	match := make(map[string]bool, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		match[id] = strings.Contains(strings.ToLower(a.filterWindow(id)), needle)
	}
	a.filterMatch = match
	return a
}

// refreshFilter re-evaluates the matches against the output as it stands now
// and keeps the focus on the host it was on. It is what makes new output
// change the grid live: a host that starts printing the pattern joins the
// filtered view without a keypress, and one whose match scrolled out of the
// window leaves it. A no-op while no filter is in force, which is what keeps
// it affordable on the output message.
func (a App) refreshFilter() App {
	if a.outputFilter == "" {
		return a
	}
	focused := a.FocusedHost()
	return a.syncFilterMatches().refocus(focused).followFocus()
}

// filterWindow is the output the filter matches for one host: everything said
// since the last command-line send reached it, or the last [filterScanLines]
// lines when no send did.
func (a App) filterWindow(id string) string {
	if mark, ok := a.diffMarks[id]; ok {
		return a.diffTail(id, mark)
	}
	t := a.paneTerminal(id)
	if t == nil || !t.HasOutput() {
		return ""
	}
	lines := strings.Split(t.Text(), "\n")
	if len(lines) > filterScanLines {
		lines = lines[len(lines)-filterScanLines:]
	}
	return strings.Join(lines, "\n")
}

// outputFiltered drops the hosts whose output does not match, and the holes:
// a hole is a grid position, not a host, and a filtered view is an explicit
// "show me these" that has no room for the shape of what left.
func (a App) outputFiltered(ids []string) []string {
	if a.outputFilter == "" {
		return ids
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && a.filterMatch[id] {
			out = append(out, id)
		}
	}
	return out
}

// filterCounts is how many hosts match and how many the session holds.
func (a App) filterCounts() (matched, total int) {
	return len(a.outputFiltered(a.sessionHosts())), len(nonHoles(a.sessionHosts()))
}

// filterLabel is the status bar's word on the active filter, empty while none
// is in force.
func (a App) filterLabel() string {
	if a.outputFilter == "" {
		return ""
	}
	matched, total := a.filterCounts()
	return fmt.Sprintf("filter: %q (%d/%d)", a.outputFilter, matched, total)
}

// filterHidden is how many of the session's hosts the filter is holding back.
func (a App) filterHidden() int {
	if a.outputFilter == "" {
		return 0
	}
	matched, total := a.filterCounts()
	return max(0, total-matched)
}
