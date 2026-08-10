package ui

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// The host picker (issue #246): a browser for what the run *could* connect to.
// The new-host prompt in hostprompt.go answers "connect this pattern"; the
// picker answers "which hosts are there", which is a different question and
// needs a list rather than five completion hints.
//
// Everything it lists comes from a [HostSource] and arrives as a tagged
// [PickerItem], so ssh-config aliases, saved groups and the recent-host list
// are three sources of rows rather than three cases in this file; they live in
// hostsources.go (issue #254).
//
// The picker never dials. Like every other connect path it emits a
// [HostConnectMsg] and lets internal/program resolve and open the sessions,
// which is also what makes free text - `web-{01..04}`, `user@host:port` - work
// here without the UI knowing anything about expansion.

// pickerListHeight bounds how many host rows the picker draws at once. A modal
// that grows with ~/.ssh/config would cover the fleet it is being opened over;
// the rest is reached by filtering or by the cursor, which scrolls the window.
const pickerListHeight = 10

// hostPicker is the fuzzy picker's state: what has been typed, the candidates
// it filters, where the cursor is and which rows are marked.
//
// It is a value inside [App] and is copied with it, so every mutation returns a
// new picker instead of writing through a shared map - marks are a slice for
// that reason, and it keeps the connect order the order things were marked in.
type hostPicker struct {
	// open reports that the picker owns the keyboard. It is separate from the
	// input's focus only so a zero picker reads as closed.
	open bool
	// input is the fuzzy filter, boxed so the widget's ~7.8 KiB stays off
	// the App value (issue #279). The zero box reads as blurred and empty,
	// which is what a closed picker should say.
	input boxedInput
	// items are the candidates, snapshotted when the picker opened.
	items []PickerItem
	// cursor is the highlighted row within the current matches.
	cursor int
	// marked are the rows space/tab toggled, by name, in mark order.
	marked []string
}

// HostPickerOpen reports whether the fuzzy host picker has the keyboard.
func (a App) HostPickerOpen() bool { return a.picker.open }

// HostPickerFilter is what has been typed into the picker's filter.
func (a App) HostPickerFilter() string { return a.picker.input.Value() }

// HostPickerMatches are the names of the candidates the typed filter still
// selects, in source order.
func (a App) HostPickerMatches() []string {
	matches := a.picker.matches()
	names := make([]string, 0, len(matches))
	for _, item := range matches {
		names = append(names, item.Name)
	}
	return names
}

// HostPickerItems are the candidates the typed filter still selects, with
// their origin tags, in source order.
func (a App) HostPickerItems() []PickerItem { return slices.Clone(a.picker.matches()) }

// HostPickerCursor is the index of the highlighted row within
// [App.HostPickerMatches].
func (a App) HostPickerCursor() int { return a.picker.cursor }

// HostPickerMarked are the hosts marked for a multi-host connect, in mark
// order.
func (a App) HostPickerMarked() []string { return slices.Clone(a.picker.marked) }

// openHostPicker opens the picker over whatever is on screen and reads its
// candidates once. A run with no source opens it all the same: the free-text
// fallback is the whole point of it still being usable then.
func (a App) openHostPicker() App {
	p := hostPicker{open: true, input: boxInput(newLineInput("filter, or a host pattern"))}
	p.input.Focus()
	if a.cfg.HostSource != nil {
		p.items = a.cfg.HostSource.Items()
	}
	a.picker = p
	return a
}

// matches are the candidates the filter still selects, in source order. An
// empty filter matches everything, which is how the picker starts as a browser
// rather than as a prompt.
func (p hostPicker) matches() []PickerItem {
	filter := strings.TrimSpace(p.input.Value())
	if filter == "" {
		return p.items
	}
	var out []PickerItem
	for _, item := range p.items {
		if fuzzyMatch(filter, item.Name) {
			out = append(out, item)
		}
	}
	return out
}

// fuzzyMatch reports whether every rune of pattern occurs in s in order, case
// insensitively: "wb1" matches "web-01". A subsequence match is what a host
// list needs - the names are short and structured - and it costs no dependency.
func fuzzyMatch(pattern, s string) bool {
	want := []rune(strings.ToLower(pattern))
	if len(want) == 0 {
		return true
	}
	at := 0
	for _, r := range strings.ToLower(s) {
		if r != want[at] {
			continue
		}
		at++
		if at == len(want) {
			return true
		}
	}
	return false
}

// isMarked reports whether a host is marked for the multi-host connect.
func (p hostPicker) isMarked(host string) bool { return slices.Contains(p.marked, host) }

// toggleMark flips the mark on the highlighted row and steps down one, so a
// run of hosts is marked with a run of spaces. A filter that matches nothing
// has no row to mark, and the keystroke does nothing rather than marking what
// was typed - marks are for candidates, free text is for enter.
func (p hostPicker) toggleMark() hostPicker {
	matches := p.matches()
	if p.cursor < 0 || p.cursor >= len(matches) {
		return p
	}
	host := matches[p.cursor].Name

	marked := slices.Clone(p.marked)
	if i := slices.Index(marked, host); i >= 0 {
		marked = slices.Delete(marked, i, i+1)
	} else {
		marked = append(marked, host)
	}
	p.marked = marked
	p.cursor = clamp(p.cursor+1, 0, max(0, len(matches)-1))
	return p
}

// chosen is what enter connects: the marked rows if there are any, otherwise
// the highlighted candidate, otherwise the typed text as a literal host pattern
// - which is how a machine that is not in ~/.ssh/config is reached from here.
// Nothing typed and nothing to highlight chooses nothing.
//
// A row becomes its patterns, not its name: a `grp` row connects every host of
// the group it names, which is what picking a group has to mean.
func (p hostPicker) chosen() []string {
	if len(p.marked) > 0 {
		var out []string
		for _, name := range p.marked {
			out = append(out, p.patternsFor(name)...)
		}
		return out
	}
	if matches := p.matches(); p.cursor >= 0 && p.cursor < len(matches) {
		return matches[p.cursor].connect()
	}
	if typed := strings.TrimSpace(p.input.Value()); typed != "" {
		return []string{typed}
	}
	return nil
}

// patternsFor is what connecting the named row sends. A name with no item
// behind it - which the marks cannot produce, since only rows can be marked -
// is connected as itself rather than dropped.
func (p hostPicker) patternsFor(name string) []string {
	for _, item := range p.items {
		if item.Name == name {
			return item.connect()
		}
	}
	return []string{name}
}

// handleHostPickerKey feeds the picker, which owns the keyboard while it is
// open: the arrows move the cursor, space/tab mark, enter connects, esc closes
// with nothing done. Everything else is filter text, so a host called "b" can
// be typed without switching the broadcast mode.
func (a App) handleHostPickerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.PromptCancel):
		a.picker = hostPicker{}
		return a, nil

	case key.Matches(msg, a.keys.PickerUp):
		a.picker.cursor = clamp(a.picker.cursor-1, 0, max(0, len(a.picker.matches())-1))
		return a, nil

	case key.Matches(msg, a.keys.PickerDown):
		a.picker.cursor = clamp(a.picker.cursor+1, 0, max(0, len(a.picker.matches())-1))
		return a, nil

	case key.Matches(msg, a.keys.PickerMark):
		a.picker = a.picker.toggleMark()
		return a, nil

	case key.Matches(msg, a.keys.PromptSubmit):
		patterns := a.picker.chosen()
		a.picker = hostPicker{}
		if len(patterns) == 0 {
			return a, nil
		}
		// A fresh request: whatever the last one complained about is stale.
		a.connectErr = ""
		return a, func() tea.Msg { return HostConnectMsg{Patterns: patterns} }
	}

	before := a.picker.input.Value()
	cmd := a.picker.input.Update(msg)
	if a.picker.input.Value() != before {
		// The match list moved under the cursor; the first row is the only
		// position that still means something.
		a.picker.cursor = 0
	}
	return a, cmd
}

// hostPickerModal is the picker's dialog: the filter, the matching hosts with
// the cursor and the marks on them, and a footer naming the keys that answer
// it.
func (a App) hostPickerModal() modal {
	p := a.picker
	matches := p.matches()

	m := a.prompt("Add hosts", "filter", p.input, a.hostPickerHint(matches))
	m.Lines = append(m.Lines, a.hostPickerRows(matches)...)
	if len(p.marked) > 0 {
		m.Lines = append(m.Lines, a.theme.Muted.Render(
			fmt.Sprintf("  %d marked — enter connects them all", len(p.marked))))
	}
	return m
}

// hostPickerRows renders the visible slice of the match list, or the line that
// says what enter would do instead when nothing matches.
func (a App) hostPickerRows(matches []PickerItem) []string {
	p := a.picker
	if len(matches) == 0 {
		if strings.TrimSpace(p.input.Value()) == "" {
			return []string{a.theme.Muted.Render("  no hosts to offer — type a host pattern")}
		}
		lines := []string{a.theme.Muted.Render("  no match — enter connects what is typed")}
		// The expansion preview (issue #249): typed text is only ever used as a
		// pattern once nothing in the source matches it, so that is the only
		// case previewing it here means anything.
		if line := a.hostPatternPreviewLine(p.input.Value()); line != "" {
			lines = append(lines, line)
		}
		return lines
	}

	cursor := clamp(p.cursor, 0, len(matches)-1)
	first, last := visibleRange(cursor, len(matches), pickerListHeight)

	lines := make([]string, 0, last-first+1)
	for i := first; i < last; i++ {
		// The mark is a character as well as a style, so it survives a
		// terminal without colour.
		marker := "  "
		if p.isMarked(matches[i].Name) {
			marker = "▸ "
		}
		// The origin tag is text in the label rather than a style of its own,
		// for the same reason the mark is: it has to survive a terminal
		// without colour, and it has to stay readable inside the cursor's
		// full-row highlight.
		label := marker + matches[i].Kind.Tag() + "  " + matches[i].Name
		switch {
		case i == cursor:
			lines = append(lines, a.theme.ListCursor(true).Render(label))
		case p.isMarked(matches[i].Name):
			lines = append(lines, a.theme.Selected.Render(label))
		default:
			lines = append(lines, a.theme.Base.Render(label))
		}
	}
	if hidden := len(matches) - last; hidden > 0 {
		lines = append(lines, a.theme.Muted.Render(fmt.Sprintf("  +%d more", hidden)))
	}
	return lines
}

// hostPickerHint names the keys that answer the picker. What enter does
// depends on what is in front of the user, so the footer says which of the
// three it would be.
func (a App) hostPickerHint(matches []PickerItem) string {
	submit := "connects the highlighted host"
	switch {
	case len(a.picker.marked) > 0:
		submit = "connects the marked hosts"
	case len(matches) == 0:
		submit = "connects what is typed"
	}
	return promptHint(
		does(a.keys.PickerMark, "marks"),
		does(a.keys.PromptSubmit, submit),
		does(a.keys.PromptCancel, "cancels"))
}
