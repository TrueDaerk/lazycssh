package ui

import (
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// pickerApp is a run with a fixed set of ssh-config aliases behind the picker.
func pickerApp(t *testing.T, aliases []string, hosts ...string) App {
	t.Helper()

	fleet := newFakeFleet(hosts...)
	return resize(t, NewApp(Config{
		Fleet:         fleet,
		Panes:         fleet,
		ConfigAliases: aliases,
		Theme:         Options{Dark: true},
	}), 120, 40)
}

// openPicker opens the picker the way a user does and fails if it did not.
func openPicker(t *testing.T, a App) App {
	t.Helper()
	a = pressKey(t, a, "A")
	if !a.HostPickerOpen() {
		t.Fatal("A did not open the host picker")
	}
	return a
}

// enter submits and returns the message the model emitted, if any.
func submit(t *testing.T, a App) (App, tea.Msg) {
	t.Helper()

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	if cmd == nil {
		return next, nil
	}
	return next, cmd()
}

// The picker lists every concrete alias the source offers, whether or not the
// run already holds it: dialling the same machine twice is two panes, and the
// picker is a browser rather than a to-do list.
func TestHostPickerListsEveryAlias(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02", "db-01"}, "web-01"))

	if got, want := a.HostPickerMatches(), []string{"web-01", "web-02", "db-01"}; !slices.Equal(got, want) {
		t.Fatalf("HostPickerMatches() = %v, want %v", got, want)
	}

	view := plain(a.View().Content)
	if !strings.Contains(view, "Add hosts") {
		t.Fatalf("the picker is not rendered:\n%s", view)
	}
	for _, want := range []string{"web-01", "web-02", "db-01"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the picker does not list %q:\n%s", want, view)
		}
	}
}

// The filter is a case-insensitive subsequence match, and its keys are filter
// text rather than commands: typing "b" must not switch the broadcast mode.
func TestHostPickerFuzzyFilterNarrowsTheList(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02", "db-prod-01", "cache-7"}))

	a = typeInto(t, a, "WB")
	if got, want := a.HostPickerMatches(), []string{"web-01", "web-02"}; !slices.Equal(got, want) {
		t.Fatalf("HostPickerMatches() after %q = %v, want %v", a.HostPickerFilter(), got, want)
	}
	if a.HostPickerFilter() != "WB" {
		t.Fatalf("HostPickerFilter() = %q, want the typed text", a.HostPickerFilter())
	}

	a = typeInto(t, a, "2")
	if got, want := a.HostPickerMatches(), []string{"web-02"}; !slices.Equal(got, want) {
		t.Fatalf("HostPickerMatches() after %q = %v, want %v", a.HostPickerFilter(), got, want)
	}
}

// Typing moves the cursor back to the first match: the row it was on is not
// the row it would land on once the list moved under it.
func TestHostPickerTypingResetsTheCursor(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02", "db-01"}))

	a = pressKey(t, a, "down")
	if a.HostPickerCursor() != 1 {
		t.Fatalf("HostPickerCursor() = %d after down, want 1", a.HostPickerCursor())
	}
	a = typeInto(t, a, "0")
	if a.HostPickerCursor() != 0 {
		t.Fatalf("HostPickerCursor() = %d after typing, want the first match", a.HostPickerCursor())
	}
}

// The cursor stops at the ends rather than wrapping, so holding an arrow key
// cannot walk off the list.
func TestHostPickerCursorClampsAtTheEnds(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02"}))

	a = pressKey(t, a, "up")
	if a.HostPickerCursor() != 0 {
		t.Fatalf("HostPickerCursor() = %d at the top", a.HostPickerCursor())
	}
	for range 5 {
		a = pressKey(t, a, "down")
	}
	if a.HostPickerCursor() != 1 {
		t.Fatalf("HostPickerCursor() = %d at the bottom, want the last row", a.HostPickerCursor())
	}
}

// Enter on a highlighted row connects that one host and closes the picker.
func TestHostPickerEnterConnectsTheHighlightedHost(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02", "db-01"}))
	a = pressKey(t, a, "down")

	a, msg := submit(t, a)
	connect, ok := msg.(HostConnectMsg)
	if !ok || !slices.Equal(connect.Patterns, []string{"web-02"}) {
		t.Fatalf("enter produced %#v, want the highlighted host", msg)
	}
	if a.HostPickerOpen() {
		t.Fatal("the picker did not close on enter")
	}
}

// Space marks and steps down, tab marks too, and enter connects every mark in
// mark order - the whole point of the multi-select.
func TestHostPickerMarksConnectTogether(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02", "db-01"}))

	a = pressKey(t, a, " ")
	if got, want := a.HostPickerMarked(), []string{"web-01"}; !slices.Equal(got, want) {
		t.Fatalf("HostPickerMarked() = %v, want %v", got, want)
	}
	if a.HostPickerCursor() != 1 {
		t.Fatalf("marking did not step down: cursor = %d", a.HostPickerCursor())
	}

	a = pressKey(t, a, "down") // onto db-01
	a = pressKey(t, a, "tab")
	if got, want := a.HostPickerMarked(), []string{"web-01", "db-01"}; !slices.Equal(got, want) {
		t.Fatalf("HostPickerMarked() = %v, want %v", got, want)
	}

	_, msg := submit(t, a)
	connect, ok := msg.(HostConnectMsg)
	if !ok || !slices.Equal(connect.Patterns, []string{"web-01", "db-01"}) {
		t.Fatalf("enter produced %#v, want both marked hosts", msg)
	}
}

// Marking the same row twice unmarks it, and an unmarked picker falls back to
// the highlighted row again.
func TestHostPickerMarkTogglesOff(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02"}))

	a = pressKey(t, a, " ")
	a = pressKey(t, a, "up")
	a = pressKey(t, a, " ")
	if got := a.HostPickerMarked(); len(got) != 0 {
		t.Fatalf("HostPickerMarked() = %v, want none left", got)
	}

	// Unmarking steps down like marking does, so the cursor is back on the
	// second row and enter falls back to it.
	_, msg := submit(t, a)
	connect, ok := msg.(HostConnectMsg)
	if !ok || !slices.Equal(connect.Patterns, []string{"web-02"}) {
		t.Fatalf("enter produced %#v, want the highlighted host", msg)
	}
}

// A filter that matches nothing turns enter into a literal connect, brace
// expansion and all: the picker is also the way to reach a machine the config
// has never heard of.
func TestHostPickerFreeTextConnectsWhatIsTyped(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01"}))
	a = typeInto(t, a, "db-{01..02}")

	if got := a.HostPickerMatches(); len(got) != 0 {
		t.Fatalf("HostPickerMatches() = %v, want no alias to match", got)
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, "no match") {
		t.Fatalf("the picker does not say what enter would do:\n%s", view)
	}

	_, msg := submit(t, a)
	connect, ok := msg.(HostConnectMsg)
	if !ok || !slices.Equal(connect.Patterns, []string{"db-{01..02}"}) {
		t.Fatalf("enter produced %#v, want the typed pattern", msg)
	}
}

// An empty picker with nothing typed connects nothing rather than emitting an
// empty request.
func TestHostPickerEnterOnNothingDoesNothing(t *testing.T) {
	a := openPicker(t, pickerApp(t, nil))

	a, msg := submit(t, a)
	if msg != nil {
		t.Fatalf("enter on an empty picker produced %#v", msg)
	}
	if a.HostPickerOpen() {
		t.Fatal("the picker did not close on enter")
	}
}

// Esc abandons the picker: no connect, and nothing typed or marked survives
// into the next opening.
func TestHostPickerEscClosesWithoutConnecting(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01", "web-02"}))
	a = typeInto(t, a, "web")
	a = pressKey(t, a, " ")

	model, cmd := a.Update(keyMsgFor(t, "esc"))
	a = model.(App)
	if cmd != nil {
		t.Fatalf("esc produced a command: %#v", cmd())
	}
	if a.HostPickerOpen() {
		t.Fatal("esc did not close the picker")
	}

	a = openPicker(t, a)
	if a.HostPickerFilter() != "" || len(a.HostPickerMarked()) != 0 {
		t.Fatalf("the reopened picker kept state: filter %q, marks %v",
			a.HostPickerFilter(), a.HostPickerMarked())
	}
}

// ctrl+q works from inside the picker: a text input must not trap the user.
func TestCtrlQQuitsFromTheHostPicker(t *testing.T) {
	a := openPicker(t, pickerApp(t, []string{"web-01"}))

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+q inside the host picker did not quit")
	}
}

// The candidates come from the source, not from the alias slice: the picker's
// item list is pluggable (issue #246).
func TestHostPickerReadsItsSource(t *testing.T) {
	fleet := newFakeFleet()
	a := resize(t, NewApp(Config{
		Fleet:         fleet,
		Panes:         fleet,
		ConfigAliases: []string{"ignored"},
		HostSource:    aliasSource{"from-source"},
		Theme:         Options{Dark: true},
	}), 120, 40)

	a = openPicker(t, a)
	if got, want := a.HostPickerMatches(), []string{"from-source"}; !slices.Equal(got, want) {
		t.Fatalf("HostPickerMatches() = %v, want %v", got, want)
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		pattern, candidate string
		want               bool
	}{
		{"", "web-01", true},
		{"web", "web-01", true},
		{"WB1", "web-01", true},
		{"w01", "web-01", true},
		{"web-01", "web-01", true},
		{"bw", "web-01", false},
		{"web2", "web-01", false},
		{"webbb", "web-01", false},
		{"DB", "db-prod", true},
	}
	for _, tc := range tests {
		if got := fuzzyMatch(tc.pattern, tc.candidate); got != tc.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tc.pattern, tc.candidate, got, tc.want)
		}
	}
}
