package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The box exists so App stays under the implicit-heap threshold, but the
// pointer must not become shared state: an App copy mutating its input must
// not change what another copy reads. Every mutator clones before writing;
// these tests pin that discipline mutator by mutator, because one in-place
// write would leak typing across model copies silently.

func TestBoxedInputMutationsDoNotLeakAcrossCopies(t *testing.T) {
	original := boxInput(newLineInput("test"))
	original.SetValue("before")

	tests := []struct {
		name   string
		mutate func(b *boxedInput)
		check  func(t *testing.T, mutated boxedInput)
	}{
		{
			name:   "SetValue",
			mutate: func(b *boxedInput) { b.SetValue("after") },
			check: func(t *testing.T, mutated boxedInput) {
				if got := mutated.Value(); got != "after" {
					t.Fatalf("mutated copy reads %q, want %q", got, "after")
				}
			},
		},
		{
			name:   "Focus",
			mutate: func(b *boxedInput) { b.Focus() },
			check: func(t *testing.T, mutated boxedInput) {
				if !mutated.Focused() {
					t.Fatal("mutated copy is not focused")
				}
			},
		},
		{
			name: "Blur",
			mutate: func(b *boxedInput) {
				b.Focus()
				b.Blur()
			},
			check: func(t *testing.T, mutated boxedInput) {
				if mutated.Focused() {
					t.Fatal("mutated copy is still focused")
				}
			},
		},
		{
			name:   "CursorEnd",
			mutate: func(b *boxedInput) { b.CursorEnd() },
			check: func(t *testing.T, mutated boxedInput) {
				if got, want := mutated.Position(), len("before"); got != want {
					t.Fatalf("mutated copy's cursor is at %d, want %d", got, want)
				}
			},
		},
		{
			name: "Update",
			mutate: func(b *boxedInput) {
				b.Focus()
				b.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
			},
			check: func(t *testing.T, mutated boxedInput) {
				if got := mutated.Value(); got == "before" {
					t.Fatal("Update did not reach the mutated copy")
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			copied := original
			before := original.Value()
			beforeFocus := original.Focused()
			beforePos := original.Position()

			tc.mutate(&copied)
			tc.check(t, copied)

			if got := original.Value(); got != before {
				t.Fatalf("mutating a copy changed the original's value to %q, want %q", got, before)
			}
			if got := original.Focused(); got != beforeFocus {
				t.Fatalf("mutating a copy changed the original's focus to %v", got)
			}
			if got := original.Position(); got != beforePos {
				t.Fatalf("mutating a copy changed the original's cursor to %d, want %d", got, beforePos)
			}
		})
	}
}

// The zero box backs a closed hostPicker, so its reads must answer like an
// empty blurred input rather than dereferencing nil, and a mutator must summon
// a usable widget rather than crash.
func TestBoxedInputZeroValue(t *testing.T) {
	var b boxedInput

	if b.Focused() {
		t.Fatal("zero box reports focused")
	}
	if got := b.Value(); got != "" {
		t.Fatalf("zero box reads %q, want empty", got)
	}
	if got := b.Position(); got != 0 {
		t.Fatalf("zero box's cursor is at %d, want 0", got)
	}

	b.SetValue("typed")
	if got := b.Value(); got != "typed" {
		t.Fatalf("after SetValue on a zero box, Value is %q, want %q", got, "typed")
	}
}

// Model hands out the widget by value for rendering; writing to that copy must
// not reach back into the box.
func TestBoxedInputModelIsACopy(t *testing.T) {
	b := boxInput(newLineInput("test"))
	b.SetValue("kept")

	m := b.Model()
	m.SetValue("changed")

	if got := b.Value(); got != "kept" {
		t.Fatalf("mutating Model()'s return changed the box to %q, want %q", got, "kept")
	}
}
