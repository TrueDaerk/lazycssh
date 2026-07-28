package ui

import (
	"fmt"
	"strings"
)

// filterPrompt is what the filter input shows while it is open.
const filterPrompt = "/"

// hostRows returns the hosts the Hosts panel lists: every host of the run,
// minus the ones the filter excludes, each with the position it has in the full
// list so the pane number stays the pane number.
func (a App) hostRows() []hostRow {
	filter := strings.ToLower(strings.TrimSpace(a.filter.Value()))

	var rows []hostRow
	for i, id := range a.hostIDs() {
		if filter != "" && !strings.Contains(strings.ToLower(id), filter) {
			continue
		}
		rows = append(rows, hostRow{Index: i, ID: id})
	}
	return rows
}

// hostRow is one line of the Hosts panel.
type hostRow struct {
	// Index is the host's position in the full list, which is its pane number
	// minus one. Filtering must not renumber panes.
	Index int
	// ID is the host identifier.
	ID string
}

// HostCursor is the position of the cursor within the filtered rows.
func (a App) HostCursor() int { return a.hostCursor }

// SelectedHost is the host under the Hosts panel cursor, or the empty string
// when the filter matches nothing.
func (a App) SelectedHost() string {
	rows := a.hostRows()
	if len(rows) == 0 {
		return ""
	}
	return rows[clamp(a.hostCursor, 0, len(rows)-1)].ID
}

// Filtering reports whether the filter input has the keyboard.
func (a App) Filtering() bool { return a.filter.Focused() }

// Filter is the current filter text.
func (a App) Filter() string { return a.filter.Value() }

// moveHostCursor moves the Hosts panel cursor, stopping at the ends.
func (a App) moveHostCursor(delta int) App {
	a.hostCursor = clamp(a.hostCursor+delta, 0, max(0, len(a.hostRows())-1))
	return a
}

// toggleSelectedHost flips the selection of the host under the cursor.
//
// Selection is held by the broadcast router and keyed by host identifier, so it
// survives a reconnect, a filter and a page turn: the pane moves, the host keeps
// its name.
func (a App) toggleSelectedHost() App {
	if a.cfg.Targets == nil {
		return a
	}
	if id := a.SelectedHost(); id != "" {
		a.cfg.Targets.Toggle(id)
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

// hostsPanel renders the host list: connection state, pane number, selection
// marker, and the filter when it is open.
//
// Only the visible rows are rendered. With two hundred hosts the panel shows the
// dozen that fit, so the cost of a redraw is the size of the panel rather than
// the size of the fleet.
func (a App) hostsPanel(width, height int) string {
	rows := a.hostRows()

	var b strings.Builder
	if a.filter.Focused() || a.filter.Value() != "" {
		b.WriteString(a.theme.Base.Render(filterPrompt + a.filter.Value()))
		b.WriteString("\n")
		height--
	}

	if len(rows) == 0 {
		if a.filter.Value() != "" {
			b.WriteString(a.theme.Muted.Render("no host matches"))
		} else {
			b.WriteString(a.theme.Muted.Render("no hosts"))
		}
		return a.theme.Base.Width(max(0, width)).Render(b.String())
	}

	cursor := clamp(a.hostCursor, 0, len(rows)-1)
	first, last := visibleRange(cursor, len(rows), height)

	for i := first; i < last; i++ {
		if i > first {
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

// hostLine renders one host: pane number, selection marker, name and state.
func (a App) hostLine(row hostRow, underCursor bool) string {
	marker := ""
	if a.cfg.Targets != nil && a.cfg.Targets.IsSelected(row.ID) {
		marker = "*"
	}

	state := a.state(row.ID)
	name := fmt.Sprintf("%d%s %s", row.Index+1, marker, row.ID)

	line := a.theme.Base.Render(name) + " " + a.theme.State(state).Render(state.String())
	if underCursor {
		// The cursor is a style, not a character, so a host name is never
		// shifted sideways by where the cursor happens to be.
		return a.theme.Cursor.Render(name + " " + state.String())
	}
	if marker == "*" {
		return a.theme.Selected.Render(name) + " " + a.theme.State(state).Render(state.String())
	}
	return line
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
