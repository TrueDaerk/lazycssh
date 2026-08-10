package ui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// boxedInput holds a [textinput.Model] behind a pointer, because the widget is
// ~7.8 KiB by value and the root model carries several of them: held inline
// they pushed App past the 64 KiB threshold above which Go heap-allocates
// every implicit copy, and App travels by value through every method call
// (issue #279, continuing #274).
//
// The pointer must not turn into shared state: two App copies holding the same
// *textinput.Model would see each other's typing. So every mutating method is
// copy-on-write - it clones the widget, mutates the clone, and repoints the
// field at it. The receiver's copy of App changes; every other copy keeps the
// old widget. Reads go straight through the pointer, which is safe because the
// pointee is never written in place.
//
// Do not reach through to the model and mutate it directly; add the missing
// mutator here instead, in the clone-first shape.
type boxedInput struct {
	m *textinput.Model
}

// boxInput boxes a widget. The model is copied in, so the caller's value
// cannot alias the box.
func boxInput(m textinput.Model) boxedInput {
	return boxedInput{m: &m}
}

// clone repoints the box at a private copy and returns it for mutation. A nil
// box - the zero value, e.g. a closed [hostPicker] - clones from a fresh
// widget rather than dereferencing nothing.
func (b *boxedInput) clone() *textinput.Model {
	var ti textinput.Model
	if b.m != nil {
		ti = *b.m
	} else {
		ti = textinput.New()
	}
	b.m = &ti
	return b.m
}

// Model is the boxed widget by value, for rendering paths that want the real
// type. Mutating the returned copy does not touch the box.
func (b boxedInput) Model() textinput.Model {
	if b.m == nil {
		return textinput.New()
	}
	return *b.m
}

// Focused reports whether the input has focus. A zero box is blurred.
func (b boxedInput) Focused() bool { return b.m != nil && b.m.Focused() }

// Value is the typed text. A zero box is empty.
func (b boxedInput) Value() string {
	if b.m == nil {
		return ""
	}
	return b.m.Value()
}

// Position is the cursor position in runes. A zero box is at 0.
func (b boxedInput) Position() int {
	if b.m == nil {
		return 0
	}
	return b.m.Position()
}

// SetValue sets the typed text, copy-on-write.
func (b *boxedInput) SetValue(s string) { b.clone().SetValue(s) }

// Focus gives the input focus, copy-on-write.
func (b *boxedInput) Focus() tea.Cmd { return b.clone().Focus() }

// Blur removes focus, copy-on-write.
func (b *boxedInput) Blur() { b.clone().Blur() }

// CursorEnd moves the cursor past the last rune, copy-on-write.
func (b *boxedInput) CursorEnd() { b.clone().CursorEnd() }

// Update feeds a message to the widget, copy-on-write. The widget's own Update
// already returns a new model; the box adopts it, so no clone is needed.
func (b *boxedInput) Update(msg tea.Msg) tea.Cmd {
	var ti textinput.Model
	if b.m != nil {
		ti = *b.m
	} else {
		ti = textinput.New()
	}
	next, cmd := ti.Update(msg)
	b.m = &next
	return cmd
}
