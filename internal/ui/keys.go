package ui

import (
	"fmt"
	"reflect"
	"strconv"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Area is the part of the interface a key press is dispatched to. Every binding
// belongs to exactly one area, plus the global set that works everywhere, so a
// key means one thing at a time.
type Area int

const (
	// AreaGlobal covers bindings that work wherever focus is.
	AreaGlobal Area = iota
	// AreaSidebar covers the numbered panels down the left.
	AreaSidebar
	// AreaGrid covers the host panes on the right.
	AreaGrid
	// AreaBroadcast covers the broadcast input bar under the grid.
	AreaBroadcast
)

// String returns the name shown as a help column heading.
func (a Area) String() string {
	switch a {
	case AreaGlobal:
		return "global"
	case AreaSidebar:
		return "sidebar"
	case AreaGrid:
		return "panes"
	case AreaBroadcast:
		return "broadcast"
	default:
		return "unknown(" + strconv.Itoa(int(a)) + ")"
	}
}

// KeyMap is every binding in the interface, declared once.
//
// The help overlay is generated from this struct, so documentation cannot drift
// from the bindings: a binding that is not here is not shown, and a binding that
// is here without a help string fails a test.
type KeyMap struct {
	// Global.
	Help    key.Binding
	Quit    key.Binding
	NextTab key.Binding
	PrevTab key.Binding
	Panel1  key.Binding
	Panel2  key.Binding
	Panel3  key.Binding
	Panel4  key.Binding
	Panel5  key.Binding

	// Broadcast scope. Fleet is deliberately awkward - it is the one mode that
	// ignores the working set, and it should not be reachable by cycling.
	BroadcastAll      key.Binding
	BroadcastSelected key.Binding
	BroadcastSingle   key.Binding
	BroadcastFleet    key.Binding
	CommandLine       key.Binding
	NextFailure       key.Binding
	QuickSave         key.Binding
	NewHost           key.Binding
	ConnectedOnly     key.Binding
	Retile            key.Binding
	Split             key.Binding
	NextSplit         key.Binding
	PrevSplit         key.Binding

	// Selection. App-level: a selection is about the run, not about a panel,
	// and it must be editable from wherever the user is reading.
	SelectAll key.Binding
	Invert    key.Binding
	ClearSel  key.Binding
	SelectUp  key.Binding
	SelectDwn key.Binding

	// Sidebar.
	Up          key.Binding
	Down        key.Binding
	Choose      key.Binding
	Toggle      key.Binding
	SaveSet     key.Binding
	NextChunk   key.Binding
	PrevChunk   key.Binding
	GroupNew    key.Binding
	GroupDelete key.Binding
	SessionEnd  key.Binding

	// Grid. A focused pane is a terminal: plain keys are forwarded to the
	// host, so everything lazycssh keeps for itself here is an alt or shift
	// chord that keystrokeBytes never encoded, plus the one reserved escape.
	// Broadcast bar. While the bar has the keyboard every plain key is a
	// keystroke for the targets, so the bar keeps only the reserved escape,
	// the pane chords, and the csshx-style ctrl+a prefix — plus enter as the
	// way back from view mode.
	BroadcastEscape key.Binding
	BroadcastEdit   key.Binding

	LeaveTyping  key.Binding
	ToggleSelect key.Binding
	PaneLeft     key.Binding
	PaneRight    key.Binding
	PaneUp       key.Binding
	PaneDown     key.Binding
	FullScreen   key.Binding
	Reconnect    key.Binding
	ClosePane    key.Binding

	CopyPane   key.Binding
	CopyBuffer key.Binding

	ScrollUp     key.Binding
	ScrollDown   key.Binding
	ScrollTop    key.Binding
	ScrollBottom key.Binding
	SearchPane   key.Binding
	NextMatch    key.Binding
	PrevMatch    key.Binding
	ClearSearch  key.Binding
}

// DefaultKeyMap is the shipped set of bindings.
//
// Bindings are single letters where lazygit-like muscle memory expects them and
// never overlap inside one area; a test proves the second part.
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Help: key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		// Plain q quits, lazygit style. Every context where q is typed rather
		// than pressed — a pane, the broadcast bar, any text input — takes the
		// keyboard before global bindings are consulted, so the letter still
		// reaches hosts and inputs there. ctrl+q stays for existing muscle
		// memory.
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+q"), key.WithHelp("q", "quit")),
		NextTab: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		PrevTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous panel")),
		Panel1:  key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "status panel")),
		Panel2:  key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "groups panel")),
		Panel3:  key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "sessions panel")),
		Panel4:  key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "command log")),
		Panel5:  key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "broadcast bar")),

		BroadcastAll:      key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "broadcast to the working set")),
		BroadcastSelected: key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "broadcast to the selection")),
		BroadcastSingle:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "broadcast to one pane")),
		BroadcastFleet:    key.NewBinding(key.WithKeys("ctrl+alt+b"), key.WithHelp("ctrl+alt+b", "broadcast to EVERY host")),
		CommandLine:       key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "send a command")),
		NextFailure: key.NewBinding(key.WithKeys("!"),
			key.WithHelp("!", "jump to the next failed host")),
		QuickSave: key.NewBinding(key.WithKeys("S"),
			key.WithHelp("S", "save the run as a session")),
		NewHost: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "connect a new host")),
		// While typing into a pane or the broadcast bar, ctrl+a stays a
		// keystroke for the hosts (readline start-of-line); the toggle works
		// everywhere else.
		ConnectedOnly: key.NewBinding(key.WithKeys("ctrl+a"),
			key.WithHelp("ctrl+a", "show only the connected hosts")),
		// Same typing exception as ctrl+a: while a pane or the broadcast bar
		// has the keyboard, these chords belong to the hosts (ctrl+r is
		// readline reverse-search there).
		Retile: key.NewBinding(key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "re-tile the grid for the current hosts")),
		Split: key.NewBinding(key.WithKeys("ctrl+s"),
			key.WithHelp("ctrl+s", "split the grid into chunks of N panes")),
		NextSplit: key.NewBinding(key.WithKeys("ctrl+right"),
			key.WithHelp("ctrl+→", "next screenful (page, then chunk; wraps)")),
		PrevSplit: key.NewBinding(key.WithKeys("ctrl+left"),
			key.WithHelp("ctrl+←", "previous screenful (page, then chunk; wraps)")),

		SelectAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select every host")),
		Invert:    key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "invert the selection")),
		ClearSel:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear the selection")),
		SelectUp:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "select the hosts that are up")),
		SelectDwn: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "select the hosts that are down")),

		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Choose:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "open / bring to the foreground")),
		Toggle:    key.NewBinding(key.WithKeys("space", " "), key.WithHelp("space", "open / bring to the foreground")),
		SaveSet:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "save the run as a group")),
		NextChunk: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next chunk of hosts")),
		PrevChunk: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "previous chunk of hosts")),
		GroupNew: key.NewBinding(key.WithKeys("n"),
			key.WithHelp("n", "new group (in the Groups panel)")),
		GroupDelete: key.NewBinding(key.WithKeys("d"),
			key.WithHelp("d", "delete this group (in the Groups panel)")),
		SessionEnd: key.NewBinding(key.WithKeys("x"),
			key.WithHelp("x", "end this session (in the Sessions panel)")),

		// Inside the broadcast bar ctrl+a is the csshx escape prefix, which
		// shadows both the global connected-only toggle and the readline
		// start-of-line the bar used to forward; ctrl+a a sends the literal.
		// From view mode the plain ctrl+a reaches the global toggle again.
		BroadcastEscape: key.NewBinding(key.WithKeys("ctrl+a"),
			key.WithHelp("ctrl+a", "prefix: next key is a lazycssh command; a = literal ctrl+a, esc = view mode")),
		BroadcastEdit: key.NewBinding(key.WithKeys("enter"),
			key.WithHelp("enter", "back to edit mode (from view mode)")),

		LeaveTyping: key.NewBinding(key.WithKeys("ctrl+]"),
			key.WithHelp("ctrl+]", "stop typing to the host")),
		ToggleSelect: key.NewBinding(key.WithKeys("alt+ ", "alt+space"),
			key.WithHelp("alt+space", "toggle this host's selection")),
		PaneLeft:   key.NewBinding(key.WithKeys("alt+left"), key.WithHelp("alt+←", "pane left")),
		PaneRight:  key.NewBinding(key.WithKeys("alt+right"), key.WithHelp("alt+→", "pane right")),
		PaneUp:     key.NewBinding(key.WithKeys("alt+up"), key.WithHelp("alt+↑", "pane up")),
		PaneDown:   key.NewBinding(key.WithKeys("alt+down"), key.WithHelp("alt+↓", "pane down")),
		FullScreen: key.NewBinding(key.WithKeys("alt+z"), key.WithHelp("alt+z", "full screen this pane")),
		Reconnect:  key.NewBinding(key.WithKeys("alt+r"), key.WithHelp("alt+r", "reconnect this host")),
		ClosePane:  key.NewBinding(key.WithKeys("alt+x"), key.WithHelp("alt+x", "close this host, again to remove")),

		// Copy goes through OSC 52, so it reaches the local clipboard even
		// when lazycssh itself runs over SSH; a terminal without OSC 52
		// support ignores the sequence and the status line still says what
		// was attempted.
		CopyPane: key.NewBinding(key.WithKeys("alt+y"),
			key.WithHelp("alt+y", "copy this pane's visible text")),
		CopyBuffer: key.NewBinding(key.WithKeys("alt+d"),
			key.WithHelp("alt+d", "copy this pane's whole scrollback")),

		ScrollUp:     key.NewBinding(key.WithKeys("shift+pgup"), key.WithHelp("shift+pgup", "scroll back")),
		ScrollDown:   key.NewBinding(key.WithKeys("shift+pgdown"), key.WithHelp("shift+pgdn", "scroll forward")),
		ScrollTop:    key.NewBinding(key.WithKeys("shift+home"), key.WithHelp("shift+home", "oldest retained output")),
		ScrollBottom: key.NewBinding(key.WithKeys("shift+end"), key.WithHelp("shift+end", "back to the tail")),
		SearchPane:   key.NewBinding(key.WithKeys("alt+/"), key.WithHelp("alt+/", "search the scrollback")),
		NextMatch:    key.NewBinding(key.WithKeys("alt+["), key.WithHelp("alt+[", "older match")),
		PrevMatch:    key.NewBinding(key.WithKeys("alt+]"), key.WithHelp("alt+]", "newer match")),
		ClearSearch:  key.NewBinding(key.WithKeys("alt+c"), key.WithHelp("alt+c", "clear the search")),
	}
}

// global returns the bindings that work wherever focus is.
func (k KeyMap) global() []key.Binding {
	return []key.Binding{
		k.Help, k.Quit, k.NextTab, k.PrevTab,
		k.Panel1, k.Panel2, k.Panel3, k.Panel4, k.Panel5,
		k.BroadcastAll, k.BroadcastSelected, k.BroadcastSingle, k.BroadcastFleet,
		k.CommandLine, k.NextFailure, k.QuickSave, k.NewHost,
		k.ConnectedOnly, k.Retile, k.Split, k.NextSplit, k.PrevSplit,
		k.SelectAll, k.Invert, k.ClearSel, k.SelectUp, k.SelectDwn,
	}
}

// sidebar returns the bindings that act on the panel list.
func (k KeyMap) sidebar() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.Choose, k.Toggle,
		k.SaveSet, k.NextChunk, k.PrevChunk,
		k.GroupNew, k.GroupDelete, k.SessionEnd,
	}
}

// grid returns the bindings that act on the host panes.
func (k KeyMap) grid() []key.Binding {
	return []key.Binding{
		k.LeaveTyping, k.ToggleSelect,
		k.PaneLeft, k.PaneRight, k.PaneUp, k.PaneDown,
		k.FullScreen, k.Reconnect, k.ClosePane,
		k.CopyPane, k.CopyBuffer,
		k.ScrollUp, k.ScrollDown, k.ScrollTop, k.ScrollBottom,
		k.SearchPane, k.NextMatch, k.PrevMatch, k.ClearSearch,
	}
}

// broadcastBar returns the bindings that act while the broadcast bar has the
// keyboard. Everything else is a keystroke for the targets.
func (k KeyMap) broadcastBar() []key.Binding {
	return []key.Binding{k.LeaveTyping, k.BroadcastEscape, k.BroadcastEdit}
}

// Bindings returns the bindings of one area.
func (k KeyMap) Bindings(area Area) []key.Binding {
	switch area {
	case AreaSidebar:
		return k.sidebar()
	case AreaGrid:
		return k.grid()
	case AreaBroadcast:
		return k.broadcastBar()
	default:
		return k.global()
	}
}

// matchesAppBinding reports whether a key means something at the app level
// from anywhere - the global bindings, which is what handleAppKey dispatches
// without a sidebar panel focused. The broadcast bar's ctrl+a prefix asks
// before forwarding, so an unbound second key can say so instead of vanishing
// (issue #148).
func (k KeyMap) matchesAppBinding(msg tea.KeyPressMsg) bool {
	for _, b := range k.global() {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
}

// All returns every binding, in area order.
func (k KeyMap) All() []key.Binding {
	out := k.global()
	out = append(out, k.sidebar()...)
	out = append(out, k.grid()...)
	// The bar shares LeaveTyping with the grid; only its own two are new here.
	out = append(out, k.BroadcastEscape, k.BroadcastEdit)
	return out
}

// Areas returns the areas in the order the help shows them.
func Areas() []Area { return []Area{AreaGlobal, AreaSidebar, AreaGrid, AreaBroadcast} }

// For returns a [help.KeyMap] describing the bindings that apply while area has
// focus: the area's own bindings plus the global ones.
//
// This is what makes the help context sensitive. What the overlay lists while a
// pane is focused is exactly what the pane handles.
func (k KeyMap) For(area Area) help.KeyMap {
	return contextHelp{keys: k, area: area}
}

// contextHelp adapts a [KeyMap] and an [Area] to the interface the help bubble
// wants.
type contextHelp struct {
	keys KeyMap
	area Area
}

// ShortHelp is the one-line hint along the bottom: the handful of bindings a
// user needs while they are where they are.
func (c contextHelp) ShortHelp() []key.Binding {
	k := c.keys
	switch c.area {
	case AreaSidebar:
		return []key.Binding{k.Up, k.Down, k.Choose, k.Toggle, k.NextTab, k.Help}
	case AreaGrid:
		// While typing, every plain key goes to the host - the hints may only
		// name chords lazycssh actually keeps.
		return []key.Binding{k.LeaveTyping, k.PaneLeft, k.PaneRight, k.FullScreen, k.ClosePane}
	case AreaBroadcast:
		return []key.Binding{k.LeaveTyping, k.BroadcastEscape, k.BroadcastEdit}
	default:
		return []key.Binding{k.NextTab, k.CommandLine, k.Help, k.Quit}
	}
}

// FullHelp is the overlay: the focused area first, because that is what the
// question "what can I do right now" is about, then everything else.
func (c contextHelp) FullHelp() [][]key.Binding {
	groups := [][]key.Binding{c.keys.Bindings(c.area)}
	for _, area := range Areas() {
		if area == c.area {
			continue
		}
		groups = append(groups, c.keys.Bindings(area))
	}
	return groups
}

// Titles returns the column headings matching [contextHelp.FullHelp], in the
// same order, so the overlay can label its columns.
func (c contextHelp) Titles() []string {
	titles := []string{c.area.String()}
	for _, area := range Areas() {
		if area == c.area {
			continue
		}
		titles = append(titles, area.String())
	}
	return titles
}

// HelpStyles builds the help bubble's styles from the theme, so the overlay
// cannot drift from the rest of the interface.
func HelpStyles(t Theme) help.Styles {
	return help.Styles{
		ShortKey:       t.Key,
		ShortDesc:      t.Desc,
		ShortSeparator: t.Muted,
		Ellipsis:       t.Muted,
		FullKey:        t.Key,
		FullDesc:       t.Desc,
		FullSeparator:  t.Muted,
	}
}

// bindingFields returns every [key.Binding] field of a KeyMap by name, which is
// how the tests check that no binding was declared and then left out of the
// help.
func bindingFields(k KeyMap) map[string]key.Binding {
	out := make(map[string]key.Binding)
	v := reflect.ValueOf(k)
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Type != reflect.TypeOf(key.Binding{}) {
			continue
		}
		binding, ok := v.Field(i).Interface().(key.Binding)
		if !ok {
			continue
		}
		out[field.Name] = binding
	}
	return out
}

// describe renders a binding for an error message.
func describe(name string, b key.Binding) string {
	return fmt.Sprintf("%s (keys %v, help %q/%q)", name, b.Keys(), b.Help().Key, b.Help().Desc)
}
