package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// filterPrompt is what the filter input shows while it is open.
const filterPrompt = "/"

// newHostPrompt is what the new-host input shows while it is open.
const newHostPrompt = "+"

// hostRows returns what the Hosts panel lists: every host of the run, then -
// below them - the ssh-config aliases not yet in the run, as candidates to
// connect to. The filter applies to both sections; the cursor walks both.
func (a App) hostRows() []hostRow {
	filter := strings.ToLower(strings.TrimSpace(a.filter.Value()))
	match := func(id string) bool {
		return filter == "" || strings.Contains(strings.ToLower(id), filter)
	}

	var rows []hostRow
	running := make(map[string]bool)
	for i, id := range a.hostIDs() {
		running[id] = true
		if match(id) {
			rows = append(rows, hostRow{Index: i, ID: id})
		}
	}
	for _, alias := range a.cfg.ConfigAliases {
		// A connected candidate is a host now; listing it twice would offer
		// the duplicate connect this panel exists to avoid.
		if !running[alias] && match(alias) {
			rows = append(rows, hostRow{ID: alias, Candidate: true})
		}
	}
	return rows
}

// hostRow is one line of the Hosts panel.
type hostRow struct {
	// Index is the host's position in the full list, which is its pane number
	// minus one. Filtering must not renumber panes. Meaningless for a
	// candidate, which has no pane yet.
	Index int
	// ID is the host identifier, or the ssh-config alias of a candidate.
	ID string
	// Candidate reports that this row is an ssh-config alias to connect to,
	// not a host of the run.
	Candidate bool
}

// HostCursor is the position of the cursor within the filtered rows.
func (a App) HostCursor() int { return a.hostCursor }

// selectedRow is the row under the Hosts panel cursor.
func (a App) selectedRow() (hostRow, bool) {
	rows := a.hostRows()
	if len(rows) == 0 {
		return hostRow{}, false
	}
	return rows[clamp(a.hostCursor, 0, len(rows)-1)], true
}

// SelectedHost is the host under the Hosts panel cursor, or the empty string
// when the filter matches nothing or the cursor is on a connect candidate.
func (a App) SelectedHost() string {
	row, ok := a.selectedRow()
	if !ok || row.Candidate {
		return ""
	}
	return row.ID
}

// SelectedCandidate is the connect candidate under the cursor, or the empty
// string when the cursor is on a host of the run.
func (a App) SelectedCandidate() string {
	row, ok := a.selectedRow()
	if !ok || !row.Candidate {
		return ""
	}
	return row.ID
}

// CandidateMarked reports whether space has marked a candidate for the next
// connect.
func (a App) CandidateMarked(alias string) bool { return a.candidateMarks[alias] }

// ConnectError is the last connect request's resolve error, empty when there
// is none. It clears when the fleet changes.
func (a App) ConnectError() string { return a.connectErr }

// Filtering reports whether the filter input has the keyboard.
func (a App) Filtering() bool { return a.filter.Focused() }

// Filter is the current filter text.
func (a App) Filter() string { return a.filter.Value() }

// moveHostCursor moves the Hosts panel cursor, stopping at the ends.
func (a App) moveHostCursor(delta int) App {
	a.hostCursor = clamp(a.hostCursor+delta, 0, max(0, len(a.hostRows())-1))
	return a
}

// toggleSelectedHost flips the selection of the host under the cursor, or the
// connect mark of the candidate under it.
//
// Host selection is held by the broadcast router and keyed by host identifier,
// so it survives a reconnect, a filter and a page turn: the pane moves, the
// host keeps its name. Candidates are not sessions yet, so their marks live on
// the model and mean only "connect this one too".
func (a App) toggleSelectedHost() App {
	row, ok := a.selectedRow()
	if !ok {
		return a
	}
	if row.Candidate {
		if a.candidateMarks[row.ID] {
			delete(a.candidateMarks, row.ID)
		} else {
			a.candidateMarks[row.ID] = true
		}
		return a
	}
	if a.cfg.Targets != nil {
		a.cfg.Targets.Toggle(row.ID)
	}
	return a
}

// focusSelectedHost moves the pane focus to the host under the cursor and hands
// focus to the grid, which is what enter on a host means.
func (a App) focusSelectedHost() App {
	id := a.SelectedHost()
	if id == "" {
		return a
	}
	for i, host := range a.hostIDs() {
		if host == id {
			a.paneIndex = i
			break
		}
	}
	a.focus = AreaGrid
	return a.followFocus()
}

// connectSelected asks the program to connect the marked candidates, or - when
// none are marked - the candidate under the cursor. The marks clear with the
// request: what enter meant has been said.
func (a App) connectSelected() (App, tea.Cmd) {
	var patterns []string
	for _, row := range a.hostRows() {
		if row.Candidate && a.candidateMarks[row.ID] {
			patterns = append(patterns, row.ID)
		}
	}
	if len(patterns) == 0 {
		alias := a.SelectedCandidate()
		if alias == "" {
			return a, nil
		}
		patterns = []string{alias}
	}

	a.candidateMarks = make(map[string]bool)
	a.connectErr = ""
	return a, func() tea.Msg { return HostConnectMsg{Patterns: patterns} }
}

// hostsPanel renders the host list: connection state, pane number, selection
// marker, and the filter when it is open.
//
// Only the visible rows are rendered. With two hundred hosts the panel shows the
// dozen that fit, so the cost of a redraw is the size of the panel rather than
// the size of the fleet.
func (a App) hostsPanel(width, height int) string {
	rows := a.hostRows()

	var b strings.Builder
	if a.hostInput.Focused() {
		b.WriteString(a.theme.Base.Render(newHostPrompt + a.hostInput.Value()))
		b.WriteString("\n")
		height--
	}
	if a.filter.Focused() || a.filter.Value() != "" {
		b.WriteString(a.theme.Base.Render(filterPrompt + a.filter.Value()))
		b.WriteString("\n")
		height--
	}
	if a.connectErr != "" {
		// A connect that failed to resolve reports where it was asked for.
		b.WriteString(a.theme.Failure.Render(a.connectErr))
		b.WriteString("\n")
		height--
	}

	if len(rows) == 0 {
		if a.filter.Value() != "" {
			b.WriteString(a.theme.Muted.Render("no host matches"))
		} else {
			b.WriteString(a.theme.Muted.Render("no hosts — n to type one"))
		}
		return a.theme.Base.Width(max(0, width)).Render(b.String())
	}

	cursor := clamp(a.hostCursor, 0, len(rows)-1)
	first, last := visibleRange(cursor, len(rows), height)

	for i := first; i < last; i++ {
		if i > first {
			b.WriteString("\n")
		}
		// The candidates read as their own section: the first one visible
		// carries the divider naming where they come from.
		if rows[i].Candidate && (i == 0 || !rows[i-1].Candidate || i == first) {
			b.WriteString(a.theme.Muted.Render("─ ssh config ─"))
			b.WriteString("\n")
		}
		b.WriteString(a.hostLine(rows[i], i == cursor))
	}

	if hidden := len(rows) - last; hidden > 0 {
		b.WriteString("\n")
		b.WriteString(a.theme.Muted.Render(fmt.Sprintf("+%d more", hidden)))
	}

	return a.theme.Base.Width(max(0, width)).Render(b.String())
}

// hostLine renders one host: pane number, selection marker, name, state and -
// when the last command failed - its exit code, so a failing host is findable
// in the list as well as in the grid.
func (a App) hostLine(row hostRow, underCursor bool) string {
	if row.Candidate {
		// A candidate has no pane number and no state: it is an offer, not a
		// host. The mark is a character as well as a style, so it survives a
		// terminal without colour.
		marker := " "
		if a.candidateMarks[row.ID] {
			marker = "+"
		}
		line := marker + " " + row.ID
		switch {
		case underCursor:
			return a.theme.Cursor.Render(line)
		case marker == "+":
			return a.theme.Selected.Render(line)
		default:
			return a.theme.Muted.Render(line)
		}
	}

	marker := ""
	if a.cfg.Targets != nil && a.cfg.Targets.IsSelected(row.ID) {
		marker = "*"
	}

	state := a.state(row.ID)
	name := fmt.Sprintf("%d%s %s", row.Index+1, marker, row.ID)

	// Only a failure is worth a column in the list; "ok" on two hundred rows
	// would bury the three that matter.
	exit := ""
	if code, ok := a.lastExit(row.ID); ok && code != 0 {
		exit = fmt.Sprintf(" exit %d", code)
	}

	if underCursor {
		// The cursor is a style, not a character, so a host name is never
		// shifted sideways by where the cursor happens to be.
		return a.theme.Cursor.Render(name + " " + state.String() + exit)
	}

	styledExit := ""
	if exit != "" {
		styledExit = " " + a.theme.Failure.Render(exit[1:])
	}
	if marker == "*" {
		return a.theme.Selected.Render(name) + " " + a.theme.State(state).Render(state.String()) + styledExit
	}
	return a.theme.Base.Render(name) + " " + a.theme.State(state).Render(state.String()) + styledExit
}

// visibleRange returns the half-open range of rows to draw so that the cursor is
// on screen, scrolling by whole rows.
func visibleRange(cursor, total, height int) (first, last int) {
	if height < 1 {
		height = 1
	}
	if total <= height {
		return 0, total
	}

	first = cursor - height/2
	first = clamp(first, 0, total-height)
	return first, first + height
}
