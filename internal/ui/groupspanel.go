package ui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// A **group** is a persisted host list: the saved YAML file the store reads
// and writes. The Groups panel is its whole lifecycle - create with n, delete
// with d, open with enter, save the run with w - and opening one makes an open
// session out of it; see internal/ui/opensessions.go.

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

// GroupSavedMsg reports what the new-group dialog's write did. The write runs
// in a [tea.Cmd] - disk I/O never blocks Update (issue #225) - and this is how
// its outcome reaches the dialog, which stays open until it arrives.
type GroupSavedMsg struct {
	// Err is why the group was not written, or nil.
	Err error
}

// GroupRemovedMsg reports what deleting a group file did.
type GroupRemovedMsg struct {
	// Name is the group the removal was about.
	Name string
	// Err is why the file is still there, or nil.
	Err error
}

// SaveResultMsg reports what the save-as prompt's write did. The write runs in
// a [tea.Cmd] - disk I/O never blocks Update (issue #225) - and the prompt
// stays pending until this message says what happened.
type SaveResultMsg struct {
	// Name is the name the run was saved under.
	Name string
	// Err is why it was not, or nil. [sessions.ErrExists] means the overwrite
	// question must be asked.
	Err error
}

// groupsPanel is the Groups child model: the saved-group list, its cursor,
// and every dialog of the group lifecycle - create, delete, save the run.
// Disk writes leave through commands; what they did comes back as messages
// the root reduces into this panel.
type groupsPanel struct {
	ctx  panelContext
	keys KeyMap

	// store is the on-disk group store, nil when the run has none.
	store SessionStore
	// workingSet is the working set model the chunk keys page, nil without
	// one.
	workingSet WorkingSets

	// cursor is the selected row.
	cursor int
	// rows are the saved groups as last read; loadErr is why the directory
	// itself could not be listed.
	rows    []groupRow
	loadErr error

	// The new-group dialog: first the name, then the host patterns.
	nameInput  textinput.Model
	hostsInput textinput.Model
	stage      groupStage
	dialogErr  error
	// saving marks the dialog's disk write in flight: the dialog keeps the
	// keyboard, swallowing keys, until the result message lands (issue #225).
	saving bool

	// deletePending is the group the d key asked about; the panel shows the
	// question until y answers it or anything else withdraws it.
	deletePending string

	// The save-as prompt and its overwrite question.
	saveInput        textinput.Model
	saveErr          error
	confirmOverwrite bool
	// savePending marks the save's disk write in flight, same shape as
	// saving.
	savePending bool
}

// Title is the heading shown in the sidebar.
func (p *groupsPanel) Title() string { return PanelGroups.Title() }

// Number is the key that jumps to this panel.
func (p *groupsPanel) Number() int { return PanelGroups.Number() }

// Cursor is the position of the cursor.
func (p *groupsPanel) Cursor() int { return p.cursor }

// Selected is the group under the cursor.
func (p *groupsPanel) Selected() string {
	if len(p.rows) == 0 {
		return ""
	}
	return p.rows[clamp(p.cursor, 0, len(p.rows)-1)].Name
}

// DialogOpen reports whether the new-group dialog has the keyboard.
func (p *groupsPanel) DialogOpen() bool { return p.stage != groupStageNone }

// DeletePending is the group the delete question is about, empty when none is
// being asked.
func (p *groupsPanel) DeletePending() string { return p.deletePending }

// Saving reports whether the save-as prompt has the keyboard - including the
// moment a write is in flight, so no other binding can slip in between the
// enter and its result.
func (p *groupsPanel) Saving() bool {
	return p.saveInput.Focused() || p.confirmOverwrite || p.savePending
}

// SaveError is the last save failure, or nil.
func (p *groupsPanel) SaveError() error { return p.saveErr }

// MoveCursor nudges the cursor without committing - the wheel browses.
func (p *groupsPanel) MoveCursor(delta int) {
	p.cursor = clamp(p.cursor+delta, 0, max(0, len(p.rows)-1))
}

// SetCursorRow puts the cursor on a clicked visible body row.
func (p *groupsPanel) SetCursorRow(row int) {
	p.cursor = clamp(row, 0, max(0, len(p.rows)-1))
}

// Update drives the panel's list: the arrows move the group cursor and enter
// or space opens that group as a session, which is the one keystroke the panel
// exists for. n and d are routed by the root before the global bindings could
// shadow them; see [App.handleAppKey].
func (p *groupsPanel) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, p.keys.Up):
		p.MoveCursor(-1)
		return nil
	case key.Matches(msg, p.keys.Down):
		p.MoveCursor(+1)
		return nil
	case key.Matches(msg, p.keys.Choose), key.Matches(msg, p.keys.Toggle):
		return p.openSelected()
	case key.Matches(msg, p.keys.SaveSet):
		p.beginSave()
		return nil
	case key.Matches(msg, p.keys.NextChunk):
		if p.workingSet != nil {
			p.workingSet.Next()
		}
		return nil
	case key.Matches(msg, p.keys.PrevChunk):
		if p.workingSet != nil {
			p.workingSet.Prev()
		}
		return nil
	}
	return nil
}

// openSelected asks the program to open the group under the cursor. The UI
// cannot dial: it says what the user chose, and the program resolves the
// patterns through ~/.ssh/config and connects.
func (p *groupsPanel) openSelected() tea.Cmd {
	name := p.Selected()
	if name == "" {
		return nil
	}
	return func() tea.Msg { return GroupOpenMsg{Name: name} }
}

// applyLoaded lands a directory re-read in the panel.
func (p *groupsPanel) applyLoaded(msg GroupsLoadedMsg) {
	p.rows = msg.Rows
	p.loadErr = msg.Err
	p.cursor = clamp(p.cursor, 0, max(0, len(p.rows)-1))
}

// beginNew opens the create dialog on its first question.
func (p *groupsPanel) beginNew() {
	p.dialogErr = nil
	p.stage = groupStageName
	p.nameInput.SetValue("")
	p.nameInput.Focus()
	p.hostsInput.SetValue("")
}

// cancelNew closes the dialog without writing anything.
func (p *groupsPanel) cancelNew() {
	p.stage = groupStageNone
	p.saving = false
	p.nameInput.SetValue("")
	p.nameInput.Blur()
	p.hostsInput.SetValue("")
	p.hostsInput.Blur()
}

// handleDialogKey drives the two questions of the create dialog: the name,
// then the host patterns. enter advances, esc abandons, and every error - a
// taken name, a malformed pattern - keeps the dialog open with what was typed,
// because the user's input must survive the telling.
func (p *groupsPanel) handleDialogKey(msg tea.KeyPressMsg) tea.Cmd {
	if p.saving {
		// The write is in flight; a second enter must not start a second one,
		// and the typed input must survive until the result says what happened.
		return nil
	}
	switch {
	case key.Matches(msg, p.keys.PromptCancel):
		p.cancelNew()
		return nil
	case key.Matches(msg, p.keys.PromptSubmit):
		if p.stage == groupStageName {
			name := strings.TrimSpace(p.nameInput.Value())
			if name == "" {
				return nil
			}
			if err := sessions.ValidateName(name); err != nil {
				p.dialogErr = err
				return nil
			}
			if p.store != nil && p.store.Exists(name) {
				p.dialogErr = fmt.Errorf("group %q already exists", name)
				return nil
			}
			p.dialogErr = nil
			p.stage = groupStageHosts
			p.nameInput.Blur()
			p.hostsInput.Focus()
			return nil
		}
		return p.commitNew()
	}

	var cmd tea.Cmd
	if p.stage == groupStageName {
		p.nameInput, cmd = p.nameInput.Update(msg)
	} else {
		p.hostsInput, cmd = p.hostsInput.Update(msg)
	}
	return cmd
}

// commitNew writes the group. The patterns are stored as typed - brace
// expansion stays readable - and are validated right here, synchronously, so a
// typo keeps the dialog open with what was typed; only the disk write itself
// leaves the Update loop.
func (p *groupsPanel) commitNew() tea.Cmd {
	patterns := strings.Fields(p.hostsInput.Value())
	if len(patterns) == 0 {
		p.dialogErr = errors.New("a group needs at least one host")
		return nil
	}
	if p.store == nil {
		p.dialogErr = errors.New("no group directory")
		return nil
	}

	name := strings.TrimSpace(p.nameInput.Value())
	run := sessions.Run{Name: name, Patterns: patterns}
	if _, err := sessions.FromRun(run); err != nil {
		// The same validation the store would apply, without its disk.
		p.dialogErr = err
		return nil
	}

	p.dialogErr = nil
	p.saving = true
	store := p.store
	return func() tea.Msg {
		_, err := store.SaveRun(run, false)
		return GroupSavedMsg{Err: err}
	}
}

// applySaved lands the new-group write's outcome. Success closes the dialog
// and re-reads the directory; failure keeps the dialog open with what was
// typed, because the user's input must survive the telling.
func (p *groupsPanel) applySaved(msg GroupSavedMsg) tea.Cmd {
	p.saving = false
	if msg.Err != nil {
		p.dialogErr = msg.Err
		return nil
	}
	p.cancelNew()
	return func() tea.Msg { return SessionsChangedMsg{} }
}

// beginDelete opens the delete question for the group under the cursor. A
// file is never removed without the user answering for it.
func (p *groupsPanel) beginDelete() {
	p.dialogErr = nil
	p.deletePending = p.Selected()
}

// handleDeleteKey answers the delete question: enter or y removes the file,
// esc or n withdraws the question, and anything else leaves it standing (see
// [readConfirm]). An open session of that group is untouched - deleting a
// definition must not tear down live connections.
func (p *groupsPanel) handleDeleteKey(msg tea.KeyPressMsg) tea.Cmd {
	answer := readConfirm(p.keys, msg)
	if answer == answerNone {
		return nil
	}

	name := p.deletePending
	p.deletePending = ""
	if answer == answerNo || p.store == nil {
		return nil
	}
	// The removal is disk I/O, so it runs in a Cmd (issue #225); the outcome
	// arrives as a [GroupRemovedMsg].
	store := p.store
	return func() tea.Msg {
		return GroupRemovedMsg{Name: name, Err: store.Remove(name)}
	}
}

// applyRemoved lands a removal's outcome: an error shows in the panel, and
// either way the directory is re-read - a failed removal may still mean a
// changed directory, and the rows must say what is actually on disk.
func (p *groupsPanel) applyRemoved(msg GroupRemovedMsg) tea.Cmd {
	if msg.Err != nil {
		p.dialogErr = msg.Err
	}
	return func() tea.Msg { return SessionsChangedMsg{} }
}

// beginSave opens the save-as prompt, pre-filled with the foreground session's
// name, because "save it again under the same name" is the common case.
func (p *groupsPanel) beginSave() {
	p.saveErr = nil
	name := p.ctx.activeName
	if name == adHocSessionName {
		name = ""
	}
	p.saveInput.SetValue(name)
	p.saveInput.CursorEnd()
	p.saveInput.Focus()
}

// cancelSave closes the prompt without writing anything.
func (p *groupsPanel) cancelSave() {
	p.saveInput.Blur()
	p.saveInput.SetValue("")
	p.confirmOverwrite = false
	p.savePending = false
}

// handleSaveKey drives the save-as prompt and the overwrite question. An
// existing session is never replaced without the user answering for it.
func (p *groupsPanel) handleSaveKey(msg tea.KeyPressMsg) tea.Cmd {
	if p.savePending {
		// The write is in flight; a second enter must not start a second one.
		// The result message reopens the keyboard.
		return nil
	}
	if p.confirmOverwrite {
		switch readConfirm(p.keys, msg) {
		case answerYes:
			p.confirmOverwrite = false
			return p.commitSave(true)
		case answerNo:
			p.cancelSave()
			return nil
		default:
			return nil
		}
	}

	switch {
	case key.Matches(msg, p.keys.PromptSubmit):
		return p.commitSave(false)
	case key.Matches(msg, p.keys.PromptCancel):
		p.cancelSave()
		return nil
	}

	var cmd tea.Cmd
	p.saveInput, cmd = p.saveInput.Update(msg)
	return cmd
}

// commitSave writes the run as a group. An existing name is not replaced until
// the user has said so: the first attempt asks, the second overwrites.
func (p *groupsPanel) commitSave(overwrite bool) tea.Cmd {
	name := strings.TrimSpace(p.saveInput.Value())
	if name == "" || p.store == nil {
		p.cancelSave()
		return nil
	}
	if err := sessions.ValidateName(name); err != nil {
		// A bad name is known without the disk; report it now rather than
		// after the round trip through the Cmd.
		p.saveErr = err
		p.cancelSave()
		return nil
	}

	run := sessions.Run{
		Name:      name,
		Patterns:  p.ctx.runPatterns,
		Defaults:  p.ctx.runDefaults,
		Broadcast: p.ctx.bcastMode,
	}
	if p.workingSet != nil {
		run.WorkingSet = p.workingSet.Active()
	}
	if len(run.Patterns) == 0 {
		// Nothing was recorded about how the run was started, so the hosts as
		// they are now is the honest thing to write.
		run.Patterns = p.ctx.fleetIDs
	}
	if len(run.Patterns) == 0 {
		// An empty run cannot be saved, but the typed name must survive the
		// telling: the user may connect a host and press enter again.
		p.saveErr = errors.New("nothing to save: no hosts in the run")
		return nil
	}

	// The prompt keeps the keyboard while the write is in flight - Saving()
	// covers savePending - so a second enter cannot start a second write and
	// the typed name survives a failure.
	p.savePending = true
	p.saveInput.Blur()
	store := p.store
	return func() tea.Msg {
		_, err := store.SaveRun(run, overwrite)
		return SaveResultMsg{Name: name, Err: err}
	}
}

// applySaveResult lands the save-as write's outcome: success closes the prompt
// and re-reads the group directory, a taken name opens the overwrite question,
// and any other failure reports in the panel with the prompt closed. saved
// reports success, so the root can adopt the name as the run's.
func (p *groupsPanel) applySaveResult(msg SaveResultMsg) (cmd tea.Cmd, saved bool) {
	p.savePending = false
	if msg.Err == nil {
		p.cancelSave()
		return func() tea.Msg { return SessionsChangedMsg{} }, true
	}
	if errors.Is(msg.Err, sessions.ErrExists) {
		p.confirmOverwrite = true
		p.saveInput.Blur()
		return nil, false
	}
	p.saveErr = msg.Err
	p.cancelSave()
	return nil, false
}

// modal is the dialog of this panel that owns the keyboard, or false when none
// does. The order matches the guard chain in [App.handleKey]: the dialog
// listening is the dialog drawn.
func (p *groupsPanel) modal() (modal, bool) {
	switch {
	case p.confirmOverwrite:
		m := confirmModal(p.ctx.theme, p.keys, "Overwrite group",
			fmt.Sprintf("overwrite %q?", strings.TrimSpace(p.saveInput.Value())))
		m.Err = p.saveErr
		return m, true

	case p.saveInput.Focused():
		m := promptModal(p.ctx.theme, "Save group as", "name", p.saveInput,
			promptHint(does(p.keys.PromptSubmit, "saves"), does(p.keys.PromptCancel, "cancels")))
		m.Err = p.saveErr
		return m, true

	case p.stage == groupStageName:
		m := promptModal(p.ctx.theme, "New group", "name", p.nameInput,
			promptHint(does(p.keys.PromptSubmit, "continues"), does(p.keys.PromptCancel, "cancels")))
		m.Err = p.dialogErr
		return m, true

	case p.stage == groupStageHosts:
		m := promptModal(p.ctx.theme, "New group", "hosts", p.hostsInput,
			promptHint(note("space-separated patterns"),
				does(p.keys.PromptSubmit, "creates"), does(p.keys.PromptCancel, "cancels")),
			field(p.ctx.theme, "name", strings.TrimSpace(p.nameInput.Value())))
		m.Err = p.dialogErr
		return m, true

	case p.deletePending != "":
		return confirmModal(p.ctx.theme, p.keys, "Delete group",
			fmt.Sprintf("delete %q?", p.deletePending),
			"the group file is removed; open sessions of it are untouched"), true
	}

	return modal{}, false
}

// View renders the saved groups. The dialogs this panel drives - new group,
// delete, save-as - float over the frame instead of being drawn into it; see
// modal.go. focused reports whether this panel is the one that would actually
// receive a keystroke right now; it decides how the cursor row is drawn
// (issue #222).
func (p *groupsPanel) View(focused bool, width, height int) string {
	theme := p.ctx.theme
	var b strings.Builder

	// Their errors travel with the dialogs, so the panel reports one only
	// once the box that would have shown it has closed - a save that failed
	// on an empty run keeps its prompt open, and the reason belongs there.
	if p.dialogErr != nil && !p.DialogOpen() {
		b.WriteString(theme.Failure.Render(p.dialogErr.Error()))
		b.WriteString("\n")
	}
	if p.saveErr != nil && !p.Saving() {
		b.WriteString(theme.Failure.Render(p.saveErr.Error()))
		b.WriteString("\n")
	}

	switch {
	case p.store == nil:
		b.WriteString(theme.Muted.Render("no group directory"))
	case p.loadErr != nil:
		b.WriteString(theme.Failure.Render(p.loadErr.Error()))
	case len(p.rows) == 0:
		b.WriteString(theme.Muted.Render("no groups — n creates one"))
	default:
		openNames := make(map[string]bool, len(p.ctx.open))
		for _, s := range p.ctx.open {
			openNames[s.Name] = true
		}
		cursor := clamp(p.cursor, 0, len(p.rows)-1)
		first, last := visibleRange(cursor, len(p.rows), max(1, height-1))
		for i := first; i < last; i++ {
			if i > first {
				b.WriteString("\n")
			}
			b.WriteString(p.line(p.rows[i], i == cursor, openNames[p.rows[i].Name], focused))
		}
		if hidden := len(p.rows) - last; hidden > 0 {
			b.WriteString("\n")
			b.WriteString(theme.Muted.Render(fmt.Sprintf("+%d more", hidden)))
		}
	}

	return theme.Base.Width(max(0, width)).Render(b.String())
}

// line renders one saved group. A group with an open session is marked with a
// character as well as a style, so it survives a terminal without colour.
// focused decides whether the cursor row gets the strong highlight or the
// muted one; see [Theme.ListCursor].
func (p *groupsPanel) line(row groupRow, underCursor, open, focused bool) string {
	theme := p.ctx.theme
	marker := "  "
	if open {
		marker = "▸ "
	}
	label := marker + row.Label()
	switch {
	case underCursor:
		return theme.ListCursor(focused).Render(label)
	case row.Err != nil:
		return theme.Failure.Render(label)
	case open:
		return theme.Selected.Render(label)
	default:
		return theme.Base.Render(label)
	}
}

// Preview describes the saved group under the cursor: what it is for, and
// which hosts opening it would dial (issue #218).
func (p *groupsPanel) Preview(width, height int) (string, string, bool) {
	theme := p.ctx.theme
	if len(p.rows) == 0 {
		return "Group", fitLines(theme, width, height,
			[]string{theme.Muted.Render("no group selected")}), true
	}
	row := p.rows[clamp(p.cursor, 0, len(p.rows)-1)]

	title := "Group — " + row.Name
	if row.Err != nil {
		// An unreadable file says why here rather than only "(unreadable)" in
		// the list: this is the space the reason fits in.
		return title, fitLines(theme, width, height,
			[]string{theme.Failure.Render(row.Err.Error())}), true
	}

	lines := []string{field(theme, "hosts", fmt.Sprintf("%d", row.Hosts))}
	if row.Description != "" {
		lines = append(lines, field(theme, "description", row.Description))
	}
	for _, s := range p.ctx.open {
		if s.Name == row.Name {
			lines = append(lines, theme.Selected.Render("open as a session"))
			break
		}
	}
	lines = append(lines, "")
	if len(row.Patterns) == 0 {
		lines = append(lines, theme.Muted.Render("no host patterns"))
	}
	for _, pat := range row.Patterns {
		lines = append(lines, theme.Base.Render("  "+pat))
	}
	return title, fitLines(theme, width, height, lines), true
}

// The root's accessors: what the rest of the model and the tests ask the
// Groups panel about.

// GroupCursor is the position of the cursor in the Groups panel.
func (a App) GroupCursor() int { return a.panels.groups.Cursor() }

// SelectedGroup is the group under the Groups panel cursor.
func (a App) SelectedGroup() string { return a.panels.groups.Selected() }

// GroupDialogOpen reports whether the new-group dialog has the keyboard.
func (a App) GroupDialogOpen() bool { return a.panels.groups.DialogOpen() }

// DeleteGroupPending is the group the delete question is about, empty when
// none is being asked.
func (a App) DeleteGroupPending() string { return a.panels.groups.DeletePending() }

// Saving reports whether the save-as prompt has the keyboard.
func (a App) Saving() bool { return a.panels.groups.Saving() }

// SaveError is the last save failure, or nil.
func (a App) SaveError() error { return a.panels.groups.SaveError() }
