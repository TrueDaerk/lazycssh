package ui

import (
	"fmt"
	"reflect"
	"strconv"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
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
	Passthrough       key.Binding
	NextFailure       key.Binding

	// Sidebar.
	Up        key.Binding
	Down      key.Binding
	Choose    key.Binding
	Toggle    key.Binding
	Filter    key.Binding
	SelectAll key.Binding
	Invert    key.Binding
	ClearSel  key.Binding
	SelectUp  key.Binding
	SelectDwn key.Binding
	SaveSet   key.Binding
	NextChunk key.Binding
	PrevChunk key.Binding
	NewHost   key.Binding

	// Grid.
	PaneLeft   key.Binding
	PaneRight  key.Binding
	PaneUp     key.Binding
	PaneDown   key.Binding
	FullScreen key.Binding
	Reconnect  key.Binding
	ClosePane  key.Binding
	NextPage   key.Binding
	PrevPage   key.Binding

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
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Quit:    key.NewBinding(key.WithKeys("ctrl+q"), key.WithHelp("ctrl+q", "quit")),
		NextTab: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next area")),
		PrevTab: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "previous area")),
		Panel1:  key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "status panel")),
		Panel2:  key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "hosts panel")),
		Panel3:  key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "groups panel")),
		Panel4:  key.NewBinding(key.WithKeys("4"), key.WithHelp("4", "sessions panel")),
		Panel5:  key.NewBinding(key.WithKeys("5"), key.WithHelp("5", "command log")),

		BroadcastAll:      key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "broadcast to the working set")),
		BroadcastSelected: key.NewBinding(key.WithKeys("B"), key.WithHelp("B", "broadcast to the selection")),
		BroadcastSingle:   key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "broadcast to one pane")),
		BroadcastFleet:    key.NewBinding(key.WithKeys("ctrl+alt+b"), key.WithHelp("ctrl+alt+b", "broadcast to EVERY host")),
		CommandLine:       key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "send a command")),
		Passthrough: key.NewBinding(key.WithKeys("ctrl+]"),
			key.WithHelp("ctrl+]", "raw keystrokes to the hosts")),
		NextFailure: key.NewBinding(key.WithKeys("!"),
			key.WithHelp("!", "jump to the next failed host")),

		Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
		Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
		Choose:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "focus this host's pane")),
		Toggle:    key.NewBinding(key.WithKeys("space", " "), key.WithHelp("space", "toggle selection")),
		Filter:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		SelectAll: key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "select every host")),
		Invert:    key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "invert the selection")),
		ClearSel:  key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clear the selection")),
		SelectUp:  key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "select the hosts that are up")),
		SelectDwn: key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "select the hosts that are down")),
		SaveSet:   key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "save the working set or session")),
		NextChunk: key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "next chunk of hosts")),
		PrevChunk: key.NewBinding(key.WithKeys("["), key.WithHelp("[", "previous chunk of hosts")),
		NewHost:   key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "connect a new host")),

		PaneLeft:   key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "pane left")),
		PaneRight:  key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "pane right")),
		PaneUp:     key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "pane up")),
		PaneDown:   key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "pane down")),
		FullScreen: key.NewBinding(key.WithKeys("f"), key.WithHelp("f", "full screen this pane")),
		Reconnect:  key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "reconnect this host")),
		ClosePane:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "close this session")),
		NextPage:   key.NewBinding(key.WithKeys("pgdown", "n"), key.WithHelp("pgdn/n", "next page of panes")),
		PrevPage:   key.NewBinding(key.WithKeys("pgup", "p"), key.WithHelp("pgup/p", "previous page of panes")),

		ScrollUp:     key.NewBinding(key.WithKeys("ctrl+u"), key.WithHelp("ctrl+u", "scroll back")),
		ScrollDown:   key.NewBinding(key.WithKeys("ctrl+d"), key.WithHelp("ctrl+d", "scroll forward")),
		ScrollTop:    key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "oldest retained output")),
		ScrollBottom: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "back to the tail")),
		SearchPane:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search the scrollback")),
		NextMatch:    key.NewBinding(key.WithKeys("["), key.WithHelp("[", "older match")),
		PrevMatch:    key.NewBinding(key.WithKeys("]"), key.WithHelp("]", "newer match")),
		ClearSearch:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear the search")),
	}
}

// global returns the bindings that work wherever focus is.
func (k KeyMap) global() []key.Binding {
	return []key.Binding{
		k.Help, k.Quit, k.NextTab, k.PrevTab,
		k.Panel1, k.Panel2, k.Panel3, k.Panel4, k.Panel5,
		k.BroadcastAll, k.BroadcastSelected, k.BroadcastSingle, k.BroadcastFleet,
		k.CommandLine, k.Passthrough, k.NextFailure,
	}
}

// sidebar returns the bindings that act on the panel list.
func (k KeyMap) sidebar() []key.Binding {
	return []key.Binding{
		k.Up, k.Down, k.Choose, k.Toggle, k.Filter,
		k.SelectAll, k.Invert, k.ClearSel, k.SelectUp, k.SelectDwn,
		k.SaveSet, k.NextChunk, k.PrevChunk, k.NewHost,
	}
}

// grid returns the bindings that act on the host panes.
func (k KeyMap) grid() []key.Binding {
	return []key.Binding{
		k.PaneLeft, k.PaneRight, k.PaneUp, k.PaneDown,
		k.FullScreen, k.Reconnect, k.ClosePane, k.NextPage, k.PrevPage,
		k.ScrollUp, k.ScrollDown, k.ScrollTop, k.ScrollBottom,
		k.SearchPane, k.NextMatch, k.PrevMatch, k.ClearSearch,
	}
}

// Bindings returns the bindings of one area.
func (k KeyMap) Bindings(area Area) []key.Binding {
	switch area {
	case AreaSidebar:
		return k.sidebar()
	case AreaGrid:
		return k.grid()
	default:
		return k.global()
	}
}

// All returns every binding, in area order.
func (k KeyMap) All() []key.Binding {
	out := k.global()
	out = append(out, k.sidebar()...)
	out = append(out, k.grid()...)
	return out
}

// Areas returns the areas in the order the help shows them.
func Areas() []Area { return []Area{AreaGlobal, AreaSidebar, AreaGrid} }

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
		return []key.Binding{k.PaneLeft, k.PaneRight, k.FullScreen, k.Reconnect, k.NextTab, k.Help}
	default:
		return []key.Binding{k.NextTab, k.CommandLine, k.Passthrough, k.Help, k.Quit}
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
