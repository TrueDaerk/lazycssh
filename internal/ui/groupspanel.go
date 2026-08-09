package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// A **group** is a persisted host list: the saved YAML file the store reads
// and writes. The Groups panel is its whole lifecycle - create with n, delete
// with d, open with enter - and opening one makes an open session out of it;
// see internal/ui/opensessions.go.

// WorkingSets is what the interface needs from the working set model: the
// status bar describes it, and the chunk keys page it. It is the subset of
// [workingset.Manager] the interface uses, declared here so it can be faked.
type WorkingSets interface {
	// Describe renders the active set with its counts.
	Describe() string
	// Active is the selector in force.
	Active() workingset.Selector
	// ActiveName is the name of the active set, empty when it is ad hoc.
	ActiveName() string
	// Count is how many hosts are in the working set.
	Count() int
	// Total is how many hosts are in the run.
	Total() int
	// Next and Prev page a positional set by its own size.
	Next() bool
	Prev() bool
}

// groupRow is one line of the Groups panel: one saved group file.
type groupRow struct {
	// Name is the group name.
	Name string
	// Hosts is how many hosts it opens, or -1 when the file could not be read.
	Hosts int
	// Description is the group's free-text note.
	Description string
	// Patterns are the group's host patterns as typed, kept so the preview
	// can show the host list without re-reading the file inside View.
	Patterns []string
	// Err is why the row could not be read, if it could not.
	Err error
}

// Label renders the row.
func (r groupRow) Label() string {
	if r.Err != nil {
		return fmt.Sprintf("%s (unreadable)", r.Name)
	}
	label := fmt.Sprintf("%s (%d host%s)", r.Name, r.Hosts, plural(r.Hosts))
	if r.Description != "" {
		label += " — " + r.Description
	}
	return label
}

// groupStage is where the new-group dialog is: closed, asking for the name,
// or asking for the hosts.
type groupStage int

const (
	groupStageNone groupStage = iota
	groupStageName
	groupStageHosts
)

// loadGroups re-reads the group directory. One unreadable file becomes one
// unreadable row rather than an empty panel: the other groups are still
// usable, and hiding them would make a typo look like data loss.
func (a App) loadGroups() App {
	a.groupList = nil
	a.groupsErr = nil

	if a.cfg.Sessions == nil {
		return a
	}

	names, err := a.cfg.Sessions.List()
	if err != nil {
		a.groupsErr = err
		return a
	}

	for _, name := range names {
		sess, err := a.cfg.Sessions.Load(name)
		if err != nil {
			a.groupList = append(a.groupList, groupRow{Name: name, Hosts: -1, Err: err})
			continue
		}
		count, err := sess.HostCount()
		if err != nil {
			a.groupList = append(a.groupList, groupRow{Name: name, Hosts: -1, Err: err})
			continue
		}
		a.groupList = append(a.groupList, groupRow{
			Name: name, Hosts: count, Description: sess.Description,
			Patterns: sess.Patterns(),
		})
	}
	a.groupCursor = clamp(a.groupCursor, 0, max(0, len(a.groupList)-1))
	return a
}

// GroupCursor is the position of the cursor in the Groups panel.
func (a App) GroupCursor() int { return a.groupCursor }

// SelectedGroup is the group under the Groups panel cursor.
func (a App) SelectedGroup() string {
	if len(a.groupList) == 0 {
		return ""
	}
	return a.groupList[clamp(a.groupCursor, 0, len(a.groupList)-1)].Name
}

// GroupDialogOpen reports whether the new-group dialog has the keyboard.
func (a App) GroupDialogOpen() bool { return a.groupStage != groupStageNone }

// DeleteGroupPending is the group the delete question is about, empty when
// none is being asked.
func (a App) DeleteGroupPending() string { return a.deleteGroup }

// moveGroupCursor moves the cursor, stopping at the ends.
func (a App) moveGroupCursor(delta int) App {
	a.groupCursor = clamp(a.groupCursor+delta, 0, max(0, len(a.groupList)-1))
	return a
}

// openSelectedGroup asks the program to open the group under the cursor. The
// UI cannot dial: it says what the user chose, and the program resolves the
// patterns through ~/.ssh/config and connects.
func (a App) openSelectedGroup() (App, tea.Cmd) {
	name := a.SelectedGroup()
	if name == "" {
		return a, nil
	}
	return a, func() tea.Msg { return GroupOpenMsg{Name: name} }
}

// beginNewGroup opens the create dialog on its first question.
func (a App) beginNewGroup() App {
	a.groupErr = nil
	a.groupStage = groupStageName
	a.groupNameInput.SetValue("")
	a.groupNameInput.Focus()
	a.groupHostsInput.SetValue("")
	return a
}

// cancelNewGroup closes the dialog without writing anything.
func (a App) cancelNewGroup() App {
	a.groupStage = groupStageNone
	a.groupNameInput.SetValue("")
	a.groupNameInput.Blur()
	a.groupHostsInput.SetValue("")
	a.groupHostsInput.Blur()
	return a
}

// handleGroupDialogKey drives the two questions of the create dialog: the
// name, then the host patterns. enter advances, esc abandons, and every error
// - a taken name, a malformed pattern - keeps the dialog open with what was
// typed, because the user's input must survive the telling.
func (a App) handleGroupDialogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return a.cancelNewGroup(), nil
	case "enter":
		if a.groupStage == groupStageName {
			name := strings.TrimSpace(a.groupNameInput.Value())
			if name == "" {
				return a, nil
			}
			if err := sessions.ValidateName(name); err != nil {
				a.groupErr = err
				return a, nil
			}
			if a.cfg.Sessions != nil && a.cfg.Sessions.Exists(name) {
				a.groupErr = fmt.Errorf("group %q already exists", name)
				return a, nil
			}
			a.groupErr = nil
			a.groupStage = groupStageHosts
			a.groupNameInput.Blur()
			a.groupHostsInput.Focus()
			return a, nil
		}
		return a.commitNewGroup()
	}

	var cmd tea.Cmd
	if a.groupStage == groupStageName {
		a.groupNameInput, cmd = a.groupNameInput.Update(msg)
	} else {
		a.groupHostsInput, cmd = a.groupHostsInput.Update(msg)
	}
	return a, cmd
}

// commitNewGroup writes the group. The patterns are stored as typed - brace
// expansion stays readable - and are validated by the store before anything
// lands on disk.
func (a App) commitNewGroup() (App, tea.Cmd) {
	patterns := strings.Fields(a.groupHostsInput.Value())
	if len(patterns) == 0 {
		a.groupErr = errors.New("a group needs at least one host")
		return a, nil
	}
	if a.cfg.Sessions == nil {
		a.groupErr = errors.New("no group directory")
		return a, nil
	}

	name := strings.TrimSpace(a.groupNameInput.Value())
	run := sessions.Run{Name: name, Patterns: patterns}
	if _, err := a.cfg.Sessions.SaveRun(run, false); err != nil {
		a.groupErr = err
		return a, nil
	}

	next := a.cancelNewGroup()
	return next, func() tea.Msg { return SessionsChangedMsg{} }
}

// beginDeleteGroup opens the delete question for the group under the cursor.
// A file is never removed without the user answering for it.
func (a App) beginDeleteGroup() App {
	a.groupErr = nil
	a.deleteGroup = a.SelectedGroup()
	return a
}

// handleGroupDeleteKey answers the delete question: enter or y removes the
// file, esc or n withdraws the question, and anything else leaves it standing
// (see [readConfirm]). An open session of that group is untouched - deleting a
// definition must not tear down live connections.
func (a App) handleGroupDeleteKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	answer := readConfirm(msg)
	if answer == answerNone {
		return a, nil
	}

	name := a.deleteGroup
	a.deleteGroup = ""
	if answer == answerNo || a.cfg.Sessions == nil {
		return a, nil
	}
	if err := a.cfg.Sessions.Remove(name); err != nil {
		a.groupErr = err
		return a, nil
	}
	return a, func() tea.Msg { return SessionsChangedMsg{} }
}

// groupsPanel renders the saved groups. The dialogs this panel drives - new
// group, delete, save-as - float over the frame instead of being drawn into
// it; see modal.go.
func (a App) groupsPanel(width, height int) string {
	var b strings.Builder

	// Their errors travel with the dialogs, so the panel reports one only
	// once the box that would have shown it has closed - a save that failed
	// on an empty run keeps its prompt open, and the reason belongs there.
	if a.groupErr != nil && !a.GroupDialogOpen() {
		b.WriteString(a.theme.Failure.Render(a.groupErr.Error()))
		b.WriteString("\n")
	}
	if a.saveErr != nil && !a.Saving() {
		b.WriteString(a.theme.Failure.Render(a.saveErr.Error()))
		b.WriteString("\n")
	}

	switch {
	case a.cfg.Sessions == nil:
		b.WriteString(a.theme.Muted.Render("no group directory"))
	case a.groupsErr != nil:
		b.WriteString(a.theme.Failure.Render(a.groupsErr.Error()))
	case len(a.groupList) == 0:
		b.WriteString(a.theme.Muted.Render("no groups — n creates one"))
	default:
		openNames := make(map[string]bool, len(a.open))
		for _, s := range a.open {
			openNames[s.Name] = true
		}
		cursor := clamp(a.groupCursor, 0, len(a.groupList)-1)
		first, last := visibleRange(cursor, len(a.groupList), max(1, height-1))
		for i := first; i < last; i++ {
			if i > first {
				b.WriteString("\n")
			}
			b.WriteString(a.groupLine(a.groupList[i], i == cursor, openNames[a.groupList[i].Name]))
		}
		if hidden := len(a.groupList) - last; hidden > 0 {
			b.WriteString("\n")
			b.WriteString(a.theme.Muted.Render(fmt.Sprintf("+%d more", hidden)))
		}
	}

	return a.theme.Base.Width(max(0, width)).Render(b.String())
}

// groupLine renders one saved group. A group with an open session is marked
// with a character as well as a style, so it survives a terminal without
// colour.
func (a App) groupLine(row groupRow, underCursor, open bool) string {
	marker := "  "
	if open {
		marker = "▸ "
	}
	label := marker + row.Label()
	switch {
	case underCursor:
		return a.theme.Cursor.Render(label)
	case row.Err != nil:
		return a.theme.Failure.Render(label)
	case open:
		return a.theme.Selected.Render(label)
	default:
		return a.theme.Base.Render(label)
	}
}
