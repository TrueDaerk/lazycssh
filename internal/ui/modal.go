package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Dialogs float. A confirm or a prompt is drawn as a centred box composited
// over the frame - lazygit's popups - instead of into the panel it belongs to:
// the question then appears in the same place every time, and the layout stays
// visible behind it, so "delete this group" is answered with the group list
// still on screen.
//
// Which dialog owns the keyboard is decided by the guard chain in
// [App.handleKey]; [App.activeModal] walks the same order, so what floats is
// what listens.
//
// Two prompts deliberately stay in the status bar. The command line and the
// scrollback search are aimed at the panes - the command line carries the
// broadcast scope it is about to hit, the search highlights matches in the
// output - and a box in the middle of the screen would cover the very thing
// being aimed at. The per-pane auth and host-key prompts stay in their panes
// for the reason given in authpane.go: they are per host, and several can be
// open at once.

const (
	// modalMinWidth is the narrowest a dialog is drawn, unless the frame
	// itself is narrower - a terminal that small has nothing wider to give.
	modalMinWidth = 34

	// modalFrameWidth is what the box costs a body line: two border columns
	// and the body's one column of padding on each side.
	modalFrameWidth = 4

	// noCursor marks a dialog that is answered rather than typed into.
	noCursor = -1
)

// confirmHint is the footer of every yes/no dialog: the keys that answer it,
// inside the box that asks.
const confirmHint = "enter/y confirms · esc cancels"

// modal is one dialog: a titled box with body lines, an optional error, and a
// footer naming the keys that answer it.
type modal struct {
	// Title is the box's border title.
	Title string
	// Lines are the body, already styled.
	Lines []string
	// Err is shown under the body while it is set, so a rejected name is
	// reported in the box the user is still typing into.
	Err error
	// Hint is the muted footer naming the keys that answer the dialog.
	Hint string
	// CursorLine and CursorCol place the terminal cursor inside the body,
	// counted from the first body line. CursorLine is [noCursor] when the
	// dialog takes no typing.
	CursorLine int
	CursorCol  int
}

// confirm builds a yes/no dialog: the question, then any notes that change
// what answering yes means.
func (a App) confirm(title, question string, notes ...string) modal {
	// The warning style is the status bar's, which pads itself for a bar it
	// is not in here; inside a box the padding only widens it.
	lines := []string{a.theme.StatusWarning.Padding(0).Render(question)}
	for _, note := range notes {
		lines = append(lines, a.theme.Muted.Render(note))
	}
	return modal{Title: title, Lines: lines, Hint: confirmHint, CursorLine: noCursor}
}

// prompt builds a dialog around one text input: any context lines, then the
// labelled input with the cursor in it.
func (a App) prompt(title, label string, ti textinput.Model, hint string, above ...string) modal {
	prefix := a.theme.Muted.Render(label + ": ")
	lines := append([]string{}, above...)
	lines = append(lines, prefix+a.theme.Base.Render(ti.Value()))
	return modal{
		Title:      title,
		Lines:      lines,
		Hint:       hint,
		CursorLine: len(lines) - 1,
		CursorCol:  lipgloss.Width(prefix) + typedWidth(ti.Value(), ti.Position()),
	}
}

// typedWidth is the screen width of the first pos runes of s - the column the
// cursor sits in once s has been typed into an input. It counts columns, not
// runes, so a wide character does not push the cursor out of step with the
// text under it.
func typedWidth(s string, pos int) int {
	runes := []rune(s)
	return ansi.StringWidth(string(runes[:clamp(pos, 0, len(runes))]))
}

// activeModal is the dialog that owns the keyboard, or false when none does.
// The order matches the guard chain in [App.handleKey]: the dialog listening
// is the dialog drawn.
func (a App) activeModal() (modal, bool) {
	switch {
	case a.confirmOverwrite:
		m := a.confirm("Overwrite group",
			fmt.Sprintf("overwrite %q?", strings.TrimSpace(a.saveInput.Value())))
		m.Err = a.saveErr
		return m, true

	case a.saveInput.Focused():
		m := a.prompt("Save group as", "name", a.saveInput, "enter saves · esc cancels")
		m.Err = a.saveErr
		return m, true

	case a.groupStage == groupStageName:
		m := a.prompt("New group", "name", a.groupNameInput, "enter continues · esc cancels")
		m.Err = a.groupErr
		return m, true

	case a.groupStage == groupStageHosts:
		m := a.prompt("New group", "hosts", a.groupHostsInput,
			"space-separated patterns · enter creates · esc cancels",
			a.field("name", strings.TrimSpace(a.groupNameInput.Value())))
		m.Err = a.groupErr
		return m, true

	case a.deleteGroup != "":
		return a.confirm("Delete group", fmt.Sprintf("delete %q?", a.deleteGroup),
			"the group file is removed; open sessions of it are untouched"), true

	case a.endSession != "":
		return a.confirm("End session", fmt.Sprintf("end %q?", a.endSession),
			"ctrl+c and ctrl+d go to its hosts"), true

	case a.hostInput.Focused():
		return a.connectModal(), true

	case a.splitInput.Focused():
		return a.prompt("Split view", "panes per view", a.splitInput,
			"empty or 0 shows all · enter applies · esc keeps"), true
	}

	return modal{}, false
}

// renderModal lays a dialog out as a centred box and reports where it landed
// and where its cursor goes, so [App.View] can composite it over the frame. An
// empty box means the frame has no room for one.
func (a App) renderModal(m modal) (box string, x, y int, cursor *tea.Cursor) {
	lines := append([]string{}, m.Lines...)
	if m.Err != nil {
		lines = append(lines, a.theme.Failure.Render(m.Err.Error()))
	}
	if m.Hint != "" {
		lines = append(lines, "", a.theme.Muted.Render(m.Hint))
	}

	content := 0
	for _, line := range lines {
		content = max(content, lipgloss.Width(line))
	}
	// The box grows to its content and stops at the frame, never past it: a
	// dialog wider than the terminal is a dialog with its answer off screen.
	width := clamp(content+modalFrameWidth,
		min(modalMinWidth, a.layout.Width), max(0, a.layout.Width-2))
	height := min(max(0, a.layout.Height-1), len(lines)+2)
	if width <= 0 || height <= 0 {
		return "", 0, 0, nil
	}

	// Clipped, not wrapped: a long host pattern shortens on screen rather
	// than reflowing the keys that answer the dialog off the bottom of it.
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, max(0, width-modalFrameWidth), "…")
	}

	box = titledBox(a.theme, true, width, height, m.Title, strings.Join(lines, "\n"))
	x = max(0, (a.layout.Width-width)/2)
	y = max(0, (a.layout.Height-height)/2)

	if m.CursorLine >= 0 {
		// The body starts one row under the hand-drawn title line, one column
		// inside the border and one more inside the body's padding.
		cx := x + 2 + m.CursorCol
		cy := y + 1 + m.CursorLine
		if cx < x+width-1 && cy < y+height-1 {
			cursor = tea.NewCursor(cx, cy)
		}
	}
	return box, x, y, cursor
}

// composite draws an overlay box over a rendered frame at x,y, leaving the
// frame visible around it.
func composite(base, box string, x, y int) string {
	return lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	).Render()
}

// confirmAnswer is what a key press means to a yes/no dialog.
type confirmAnswer int

const (
	// answerNone is a key that is neither yes nor no: the question stays open
	// rather than being decided by a keystroke aimed at something else.
	answerNone confirmAnswer = iota
	answerYes
	answerNo
)

// readConfirm reads a key as an answer to a confirm dialog. enter and y
// confirm, esc and n withdraw the question, and everything else is ignored -
// these dialogs guard a file delete and a fleet-wide ctrl+c, and a stray
// keystroke must not be able to answer either one.
func readConfirm(msg tea.KeyPressMsg) confirmAnswer {
	switch msg.String() {
	case "enter", "y", "Y":
		return answerYes
	case "esc", "n", "N":
		return answerNo
	}
	return answerNone
}
