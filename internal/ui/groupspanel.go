package ui

import (
	"fmt"
	"strings"

	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// allHostsRow is the built-in entry that returns to every host. It is always
// first, because "undo my narrowing" has to be one keystroke away.
const allHostsRow = "all hosts"

// WorkingSets is what the Groups panel needs from the working set model. It is
// the subset of [workingset.Manager] the interface uses, declared here so the
// panel can be tested against a fake and so the UI cannot reach past it.
type WorkingSets interface {
	// Describe renders the active set with its counts.
	Describe() string
	// Active is the selector in force.
	Active() workingset.Selector
	// ActiveName is the name of the active set, empty when it is ad hoc.
	ActiveName() string
	// Named returns the saved sets in definition order.
	Named() []workingset.Set
	// Activate makes a named set the working set.
	Activate(name string) error
	// Reset makes every host the working set again.
	Reset()
	// Count is how many hosts are in the working set.
	Count() int
	// Total is how many hosts are in the run.
	Total() int
	// Next and Prev page a positional set by its own size.
	Next() bool
	Prev() bool
}

// groupRow is one line of the Groups panel.
type groupRow struct {
	// Name is what the row activates, empty for the built-in all-hosts row.
	Name string
	// Label is what the row shows.
	Label string
	// Active marks the row as the working set in force.
	Active bool
}

// groupRows returns the rows of the Groups panel: the built-in all-hosts entry,
// every named set, and - when the active set is neither - the ad hoc set the
// user is in right now, so the panel never hides where they are.
func (a App) groupRows() []groupRow {
	ws := a.cfg.WorkingSet
	if ws == nil {
		return nil
	}

	activeName := ws.ActiveName()
	everyHost := ws.Active() != nil && ws.Active().Kind() == workingset.KindAll

	rows := []groupRow{{Label: allHostsRow, Active: activeName == "" && everyHost}}
	for _, set := range ws.Named() {
		rows = append(rows, groupRow{
			Name:   set.Name,
			Label:  fmt.Sprintf("%s (%s)", set.Name, set.Selector.String()),
			Active: set.Name == activeName,
		})
	}

	if activeName == "" && !everyHost {
		rows = append(rows, groupRow{
			Label:  fmt.Sprintf("(unnamed) %s", ws.Active().String()),
			Active: true,
		})
	}
	return rows
}

// GroupCursor is the position of the cursor in the Groups panel.
func (a App) GroupCursor() int { return a.groupCursor }

// SelectedGroup is the row under the Groups panel cursor.
func (a App) SelectedGroup() string {
	rows := a.groupRows()
	if len(rows) == 0 {
		return ""
	}
	return rows[clamp(a.groupCursor, 0, len(rows)-1)].Label
}

// moveGroupCursor moves the cursor, stopping at the ends.
func (a App) moveGroupCursor(delta int) App {
	a.groupCursor = clamp(a.groupCursor+delta, 0, max(0, len(a.groupRows())-1))
	return a
}

// activateSelectedGroup makes the row under the cursor the working set. That is
// the panel's whole purpose: one keystroke from "which twenty am I working on"
// to "these twenty".
func (a App) activateSelectedGroup() (App, error) {
	ws := a.cfg.WorkingSet
	rows := a.groupRows()
	if ws == nil || len(rows) == 0 {
		return a, nil
	}

	row := rows[clamp(a.groupCursor, 0, len(rows)-1)]
	switch {
	case row.Name == "" && row.Label == allHostsRow:
		ws.Reset()
		return a, nil
	case row.Name == "":
		// The ad hoc row is where the user already is.
		return a, nil
	default:
		if err := ws.Activate(row.Name); err != nil {
			return a, err
		}
		return a, nil
	}
}

// groupsPanel renders the working sets: which exist, which is active, and where
// the window sits.
func (a App) groupsPanel(width, height int) string {
	if a.cfg.WorkingSet == nil {
		return a.theme.Muted.Render("no working sets yet")
	}

	rows := a.groupRows()
	cursor := clamp(a.groupCursor, 0, max(0, len(rows)-1))
	first, last := visibleRange(cursor, len(rows), max(1, height-2))

	var b strings.Builder
	for i := first; i < last; i++ {
		if i > first {
			b.WriteString("\n")
		}
		b.WriteString(a.groupLine(rows[i], i == cursor))
	}

	// The window position belongs here as much as the sets do: it is the other
	// half of "which hosts am I looking at", and the two are not the same
	// question - see wiki/core/working-sets.md.
	b.WriteString("\n")
	b.WriteString(a.theme.Muted.Render(a.cfg.WorkingSet.Describe()))
	if label := a.windowLabel(); label != "" {
		b.WriteString("\n")
		b.WriteString(a.theme.Muted.Render(label))
	}

	return a.theme.Base.Width(max(0, width)).Render(b.String())
}

// groupLine renders one row. The active set is marked with a character as well
// as a style, so it survives a terminal without colour.
func (a App) groupLine(row groupRow, underCursor bool) string {
	marker := "  "
	if row.Active {
		marker = "▸ "
	}

	label := marker + row.Label
	switch {
	case underCursor:
		return a.theme.Cursor.Render(label)
	case row.Active:
		return a.theme.Selected.Render(label)
	default:
		return a.theme.Base.Render(label)
	}
}
