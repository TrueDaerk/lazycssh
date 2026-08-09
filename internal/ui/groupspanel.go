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

// GroupsLoadedMsg carries a re-read of the group directory back into Update.
// The reading happens in a [tea.Cmd] - it is disk I/O, and Update never blocks
// (issue #225); this message is how the result lands in the model.
type GroupsLoadedMsg struct {
	// Rows are the readable and unreadable groups, in lexical order.
	Rows []groupRow
	// Err is why the directory itself could not be listed, or nil.
	Err error
}

// loadGroupsCmd re-reads the group directory off the Update loop. The result
// arrives as a [GroupsLoadedMsg].
func (a App) loadGroupsCmd() tea.Cmd {
	store := a.cfg.Sessions
	if store == nil {
		return nil
	}
	return func() tea.Msg { return readGroups(store) }
}

// readGroups builds the panel's rows from the store. One unreadable file
// becomes one unreadable row rather than an empty panel: the other groups are
// still usable, and hiding them would make a typo look like data loss.
func readGroups(store SessionStore) GroupsLoadedMsg {
	names, err := store.List()
	if err != nil {
		return GroupsLoadedMsg{Err: err}
	}

	var rows []groupRow
	for _, name := range names {
		sess, err := store.Load(name)
		if err != nil {
			rows = append(rows, groupRow{Name: name, Hosts: -1, Err: err})
			continue
		}
		count, err := sess.HostCount()
		if err != nil {
			rows = append(rows, groupRow{Name: name, Hosts: -1, Err: err})
			continue
		}
		rows = append(rows, groupRow{
			Name: name, Hosts: count, Description: sess.Description,
			Patterns: sess.Patterns(),
		})
	}
	return GroupsLoadedMsg{Rows: rows}
}

// applyGroupsLoaded lands a directory re-read in the model.
func (a App) applyGroupsLoaded(msg GroupsLoadedMsg) App {
	a.groupList = msg.Rows
	a.groupsErr = msg.Err
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
	a.groupSaving = false
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
	if a.groupSaving {
		// The write is in flight; a second enter must not start a second one,
		// and the typed input must survive until the result says what happened.
		return a, nil
	}
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

// GroupSavedMsg reports what the new-group dialog's write did. The write runs
// in a [tea.Cmd] - disk I/O never blocks Update (issue #225) - and this is how
// its outcome reaches the dialog, which stays open until it arrives.
type GroupSavedMsg struct {
	// Err is why the group was not written, or nil.
	Err error
}

// commitNewGroup writes the group. The patterns are stored as typed - brace
// expansion stays readable - and are validated right here, synchronously, so a
// typo keeps the dialog open with what was typed; only the disk write itself
// leaves the Update loop.
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
	if _, err := sessions.FromRun(run); err != nil {
		// The same validation the store would apply, without its disk.
		a.groupErr = err
		return a, nil
	}

	a.groupErr = nil
	a.groupSaving = true
	store := a.cfg.Sessions
	return a, func() tea.Msg {
		_, err := store.SaveRun(run, false)
		return GroupSavedMsg{Err: err}
	}
}

// applyGroupSaved lands the new-group write's outcome. Success closes the
// dialog and re-reads the directory; failure keeps the dialog open with what
// was typed, because the user's input must survive the telling.
func (a App) applyGroupSaved(msg GroupSavedMsg) (App, tea.Cmd) {
	a.groupSaving = false
	if msg.Err != nil {
		a.groupErr = msg.Err
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
	// The removal is disk I/O, so it runs in a Cmd (issue #225); the outcome
	// arrives as a [GroupRemovedMsg].
	store := a.cfg.Sessions
	return a, func() tea.Msg {
		return GroupRemovedMsg{Name: name, Err: store.Remove(name)}
	}
}

// GroupRemovedMsg reports what deleting a group file did.
type GroupRemovedMsg struct {
	// Name is the group the removal was about.
	Name string
	// Err is why the file is still there, or nil.
	Err error
}

// applyGroupRemoved lands a removal's outcome: an error shows in the panel,
// and either way the directory is re-read - a failed removal may still mean a
// changed directory, and the rows must say what is actually on disk.
func (a App) applyGroupRemoved(msg GroupRemovedMsg) (App, tea.Cmd) {
	if msg.Err != nil {
		a.groupErr = msg.Err
	}
	return a, func() tea.Msg { return SessionsChangedMsg{} }
}

// groupsPanel renders the saved groups. The dialogs this panel drives - new
// group, delete, save-as - float over the frame instead of being drawn into
// it; see modal.go. focused reports whether this panel is the one that would
// actually receive a keystroke right now; it decides how the cursor row is
// drawn (issue #222).
func (a App) groupsPanel(width, height int, focused bool) string {
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
			b.WriteString(a.groupLine(a.groupList[i], i == cursor, openNames[a.groupList[i].Name], focused))
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
// colour. focused decides whether the cursor row gets the strong highlight or
// the muted one; see [Theme.ListCursor].
func (a App) groupLine(row groupRow, underCursor, open, focused bool) string {
	marker := "  "
	if open {
		marker = "▸ "
	}
	label := marker + row.Label()
	switch {
	case underCursor:
		return a.theme.ListCursor(focused).Render(label)
	case row.Err != nil:
		return a.theme.Failure.Render(label)
	case open:
		return a.theme.Selected.Render(label)
	default:
		return a.theme.Base.Render(label)
	}
}
