package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/lipgloss/v2"
)

// The acceptance criterion: a dialog is a box in the middle of the frame, and
// the layout is still there behind it.
func TestModalFloatsOverTheLayout(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "h1"))
	a = pressKey(t, a, "d")

	view := plain(a.View().Content)
	if !strings.Contains(view, "Delete group") || !strings.Contains(view, `delete "prod"?`) {
		t.Fatalf("the dialog is not on screen:\n%s", view)
	}
	if !strings.Contains(view, confirmHint(a.keys)) {
		t.Fatalf("the dialog does not name the keys that answer it:\n%s", view)
	}
	// The panels the dialog is asked from are still drawn around it.
	for _, want := range []string{"Status [1]", "Groups [2]", "Sessions [3]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the layout behind the dialog lost %q:\n%s", want, view)
		}
	}
	// And the dialog is centred rather than pinned to a panel: the row it
	// sits on starts with the layout, not with the box.
	for _, line := range strings.Split(view, "\n") {
		if i := strings.Index(line, "╭ Delete group"); i > 0 {
			return
		}
	}
	t.Fatalf("the dialog is not inset from the left edge:\n%s", view)
}

// A dialog never renders wider or taller than the terminal, so its answer
// cannot end up off screen.
func TestModalClampsToTheTerminal(t *testing.T) {
	for _, size := range []struct{ w, h int }{{30, 8}, {24, 4}, {40, 6}, {120, 40}} {
		a, _ := groupsStoreApp(t, savedGroup("a-group-with-a-fairly-long-name", "h1"))
		a = resize(t, a, size.w, size.h)
		a = pressKey(t, a, "d")

		m, ok := a.activeModal()
		if !ok {
			t.Fatalf("%dx%d: no dialog is open", size.w, size.h)
		}
		box, x, y, _ := a.renderModal(m)
		if box == "" {
			continue // no room for a box at all, which is honest
		}
		if w := lipgloss.Width(box); x+w > size.w {
			t.Fatalf("%dx%d: the box runs off the right edge (x=%d w=%d)", size.w, size.h, x, w)
		}
		if hgt := lipgloss.Height(box); y+hgt > size.h {
			t.Fatalf("%dx%d: the box runs off the bottom (y=%d h=%d)", size.w, size.h, y, hgt)
		}
	}
}

// The too-small guard wins: below the minimum the frame is the apology, and no
// dialog is composited over it.
func TestModalDoesNotSurviveTheTooSmallGuard(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "h1"))
	a = pressKey(t, a, "d")
	a = resize(t, a, 10, 2)

	view := a.View()
	// The apology is clipped to the terminal it apologises for, so only its
	// start is on screen - and none of the dialog is.
	got := plain(view.Content)
	if !strings.HasPrefix(got, "terminal t") {
		t.Fatalf("the too-small guard did not draw:\n%s", got)
	}
	if strings.Contains(got, "Delete group") {
		t.Fatalf("the dialog drew over the apology:\n%s", got)
	}
	if view.Cursor != nil {
		t.Fatal("the apology carries a cursor")
	}
}

// The acceptance criterion: nothing underneath the dialog hears the keyboard.
func TestModalBlocksTheBackground(t *testing.T) {
	a, store := groupsStoreApp(t, savedGroup("prod", "h1"))
	a = pressKey(t, a, "d")

	// "1" would switch to the Status panel, "j" would move the cursor, "S"
	// would open the save prompt. While the question stands they do nothing,
	// and the question is still the question.
	for _, keystroke := range []string{"1", "j", "S"} {
		a = pressKey(t, a, keystroke)
		if a.Panel() != PanelGroups {
			t.Fatalf("%q reached the panels: Panel() = %v", keystroke, a.Panel())
		}
		if a.Saving() {
			t.Fatalf("%q opened the save prompt behind the dialog", keystroke)
		}
		if a.DeleteGroupPending() != "prod" {
			t.Fatalf("%q answered the question: pending = %q", keystroke, a.DeleteGroupPending())
		}
	}
	if !store.Exists("prod") {
		t.Fatal("a stray keystroke deleted the group")
	}
}

// Confirms answer to enter as well as y, and esc withdraws them.
func TestConfirmResolvesOnEnterAndCancelsOnEsc(t *testing.T) {
	a, store := groupsStoreApp(t, savedGroup("prod", "h1"))

	a = pressKey(t, a, "d")
	a = pressKey(t, a, "esc")
	if a.DeleteGroupPending() != "" {
		t.Fatal("esc left the question open")
	}
	if !store.Exists("prod") {
		t.Fatal("esc deleted the group anyway")
	}

	a = pressKey(t, a, "d")
	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a = model.(App)
	if a.DeleteGroupPending() != "" {
		t.Fatal("enter left the question open")
	}
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	// The removal runs in the Cmd (issue #225); settle drains it.
	a = settle(t, a, cmd)
	if store.Exists("prod") {
		t.Fatal("enter did not delete the group")
	}
}

func TestReadConfirm(t *testing.T) {
	cases := []struct {
		key  tea.KeyPressMsg
		want confirmAnswer
	}{
		{tea.KeyPressMsg{Code: tea.KeyEnter}, answerYes},
		{tea.KeyPressMsg{Code: 'y', Text: "y"}, answerYes},
		{tea.KeyPressMsg{Code: 'Y', Text: "Y", Mod: tea.ModShift}, answerYes},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, answerNo},
		{tea.KeyPressMsg{Code: 'n', Text: "n"}, answerNo},
		{tea.KeyPressMsg{Code: 'q', Text: "q"}, answerNone},
		{tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, answerNone},
	}
	app := App{keys: DefaultKeyMap()}
	for _, c := range cases {
		if got := readConfirm(app.keys, c.key); got != c.want {
			t.Fatalf("readConfirm(%q) = %v, want %v", c.key.String(), got, c.want)
		}
	}
}

// The focused input's cursor is the frame's cursor, sitting on the column the
// next character lands in.
func TestModalLiftsTheInputCursor(t *testing.T) {
	a, _ := groupsStoreApp(t)
	a = pressKey(t, a, "n")
	a = typeInto(t, a, "web")

	view := a.View()
	if view.Cursor == nil {
		t.Fatal("the open prompt has no cursor")
	}

	m, ok := a.activeModal()
	if !ok {
		t.Fatal("no dialog is open")
	}
	box, x, y, cursor := a.renderModal(m)
	// Two columns in from the box's left edge - border plus padding - and one
	// row under the hand-drawn title line, past "name: web".
	if wantX := x + 2 + len("name: web"); cursor.Position.X != wantX {
		t.Fatalf("cursor X = %d, want %d", cursor.Position.X, wantX)
	}
	if wantY := y + 1; cursor.Position.Y != wantY {
		t.Fatalf("cursor Y = %d, want %d", cursor.Position.Y, wantY)
	}
	if lipgloss.Width(box) == 0 {
		t.Fatal("the box is empty")
	}

	// Moving the caret moves the cursor with it.
	a = pressKey(t, a, "left")
	if got := a.View().Cursor.Position.X; got != cursor.Position.X-1 {
		t.Fatalf("cursor X = %d after left, want %d", got, cursor.Position.X-1)
	}
}

// A dialog that is answered rather than typed into shows no cursor.
func TestConfirmHasNoCursor(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "h1"))
	a = pressKey(t, a, "d")
	if c := a.View().Cursor; c != nil {
		t.Fatalf("the confirm carries a cursor at %+v", c.Position)
	}
}

// Every dialog the guard chain routes to has a box to go with it: a prompt
// that owns the keyboard without appearing on screen would be a trap.
func TestEveryCapturedPromptHasAModal(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "h1"))

	cases := []struct {
		name  string
		open  func(App) App
		title string
	}{
		{"new group", func(a App) App { return pressKey(t, a, "n") }, "New group"},
		{"delete group", func(a App) App { return pressKey(t, a, "d") }, "Delete group"},
		{"save as", func(a App) App { return pressKey(t, a, "S") }, "Save group as"},
		{"split", func(a App) App { return pressKey(t, a, "ctrl+s") }, "Split view"},
	}
	for _, c := range cases {
		opened := c.open(a)
		if view := plain(opened.View().Content); !strings.Contains(view, c.title) {
			t.Fatalf("%s: no box titled %q:\n%s", c.name, c.title, view)
		}
	}
}

// A long value is clipped to the box rather than wrapped out of it: the keys
// that answer the dialog stay on screen.
func TestModalClipsLongLines(t *testing.T) {
	a, _ := groupsStoreApp(t)
	a = pressKey(t, a, "n")
	a = typeInto(t, a, strings.Repeat("x", 400))

	m, ok := a.activeModal()
	if !ok {
		t.Fatal("no dialog is open")
	}
	box, _, _, _ := a.renderModal(m)
	if got, limit := lipgloss.Width(box), 120-2; got > limit {
		t.Fatalf("box width = %d, want at most %d", got, limit)
	}
	if !strings.Contains(plain(box), confirmHintlessSuffix) {
		t.Fatalf("the hint was pushed out of the box:\n%s", plain(box))
	}
}

// confirmHintlessSuffix is the tail of the new-group prompt's hint, which has
// to survive whatever is typed above it.
const confirmHintlessSuffix = "esc cancels"
