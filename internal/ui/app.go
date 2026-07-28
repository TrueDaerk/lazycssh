package ui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Panel is one of the numbered panels down the left, in the order the number
// keys select them.
type Panel int

const (
	// PanelStatus answers "what happens if I type".
	PanelStatus Panel = iota
	// PanelHosts lists every host with its connection state.
	PanelHosts
	// PanelGroups lists the working sets.
	PanelGroups
	// PanelSessions lists the saved session configs.
	PanelSessions
	// PanelCommandLog is the audit trail of what was sent this run.
	PanelCommandLog
)

// Panels returns the panels in sidebar order.
func Panels() []Panel {
	return []Panel{PanelStatus, PanelHosts, PanelGroups, PanelSessions, PanelCommandLog}
}

// Title is the heading shown in the sidebar.
func (p Panel) Title() string {
	switch p {
	case PanelStatus:
		return "Status"
	case PanelHosts:
		return "Hosts"
	case PanelGroups:
		return "Groups"
	case PanelSessions:
		return "Sessions"
	case PanelCommandLog:
		return "Command log"
	default:
		return "unknown(" + strconv.Itoa(int(p)) + ")"
	}
}

// Number is the key that jumps to this panel.
func (p Panel) Number() int { return int(p) + 1 }

// Config is what the root model needs to exist. Everything is passed in;
// nothing is read from a global.
type Config struct {
	// SessionName is shown in the status bar, empty for an ad hoc run.
	SessionName string
	// Hosts are the host identifiers of the run, in order.
	Hosts []string
	// Theme selects the palette.
	Theme Options
	// Keys overrides the bindings. The zero value means [DefaultKeyMap].
	Keys *KeyMap
	// Insecure reports that host key verification is off, which the status bar
	// then says for the whole run.
	Insecure bool
}

// App is the root bubbletea model: it owns the layout, the focus and the panel
// selection, and it renders the frame every other view draws into.
//
// Model mutation happens only in [App.Update]. Nothing here dials, reads or
// writes a host; the transport reports through messages.
type App struct {
	cfg    Config
	keys   KeyMap
	theme  Theme
	help   help.Model
	layout Layout

	focus    Area
	panel    Panel
	showHelp bool
}

// NewApp builds the root model.
func NewApp(cfg Config) App {
	keys := DefaultKeyMap()
	if cfg.Keys != nil {
		keys = *cfg.Keys
	}

	theme := NewTheme(cfg.Theme)
	h := help.New()
	h.Styles = HelpStyles(theme)

	return App{
		cfg:   cfg,
		keys:  keys,
		theme: theme,
		help:  h,
		focus: AreaSidebar,
		panel: PanelStatus,
	}
}

// Init asks the terminal for its background colour so the palette can match it.
func (a App) Init() tea.Cmd { return tea.RequestBackgroundColor }

// Focus is the area that receives key presses.
func (a App) Focus() Area { return a.focus }

// Panel is the selected sidebar panel.
func (a App) Panel() Panel { return a.panel }

// Layout is the current geometry.
func (a App) Layout() Layout { return a.layout }

// Theme is the current styles, rebuilt when the terminal reports its
// background.
func (a App) Theme() Theme { return a.theme }

// HelpVisible reports whether the overlay is open.
func (a App) HelpVisible() bool { return a.showHelp }

// Update handles one message. It is the only place the model changes.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.layout = ComputeLayout(msg.Width, msg.Height)
		a.help.SetWidth(max(0, a.layout.Width-2))
		return a, nil

	case tea.BackgroundColorMsg:
		opts := a.cfg.Theme
		opts.Dark = msg.IsDark()
		a.cfg.Theme = opts
		a.theme = NewTheme(opts)
		a.help.Styles = HelpStyles(a.theme)
		return a, nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)
	}

	return a, nil
}

// handleKey dispatches a key press. Bindings are matched by area, so a key
// means one thing at a time; see [KeyMap].
func (a App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// While the overlay is open it is the only thing listening: a user reading
	// the help is not also driving the panes.
	if a.showHelp {
		if key.Matches(msg, a.keys.Quit) {
			return a, tea.Quit
		}
		a.showHelp = false
		return a, nil
	}

	switch {
	case key.Matches(msg, a.keys.Quit):
		return a, tea.Quit

	case key.Matches(msg, a.keys.Help):
		a.showHelp = true
		return a, nil

	case key.Matches(msg, a.keys.NextTab):
		a.focus = nextArea(a.focus)
		return a, nil

	case key.Matches(msg, a.keys.PrevTab):
		a.focus = prevArea(a.focus)
		return a, nil
	}

	if panel, ok := a.panelForKey(msg); ok {
		a.panel = panel
		a.focus = AreaSidebar
		return a, nil
	}

	return a, nil
}

// panelForKey maps a number key to the panel it selects.
func (a App) panelForKey(msg tea.KeyPressMsg) (Panel, bool) {
	numbered := []key.Binding{a.keys.Panel1, a.keys.Panel2, a.keys.Panel3, a.keys.Panel4, a.keys.Panel5}
	for i, binding := range numbered {
		if key.Matches(msg, binding) {
			return Panel(i), true
		}
	}
	return 0, false
}

// nextArea cycles focus between the two areas the user moves between. The
// global area is not a focus target: it is what is live everywhere.
func nextArea(a Area) Area {
	if a == AreaSidebar {
		return AreaGrid
	}
	return AreaSidebar
}

// prevArea cycles the other way. With two focus targets it is the same move,
// and it exists so the binding keeps working when a third area is added.
func prevArea(a Area) Area { return nextArea(a) }

// View renders the whole frame.
func (a App) View() tea.View {
	if a.layout.TooSmall {
		// Even the apology has to fit: a terminal three columns wide gets three
		// columns of it rather than a line that wraps into the scrollback.
		return tea.NewView(a.theme.Muted.
			MaxWidth(max(0, a.layout.Width)).
			MaxHeight(max(0, a.layout.Height)).
			Render("terminal too small"))
	}
	if a.layout.Width == 0 || a.layout.Height == 0 {
		// No WindowSizeMsg has arrived yet. Rendering nothing is honest; the
		// first resize message is on its way.
		return tea.NewView("")
	}

	body := a.renderMain()
	if a.layout.SidebarVisible() {
		body = lipgloss.JoinHorizontal(lipgloss.Top, a.renderSidebar(), body)
	}

	view := lipgloss.JoinVertical(lipgloss.Left, body, a.renderStatusBar())
	if a.showHelp {
		view = a.renderHelpOverlay()
	}
	return tea.NewView(view)
}

// renderSidebar draws the numbered panel list.
func (a App) renderSidebar() string {
	r := a.layout.Sidebar
	focused := a.focus == AreaSidebar

	var b strings.Builder
	for i, panel := range Panels() {
		if i > 0 {
			b.WriteString("\n")
		}
		label := fmt.Sprintf("[%d] %s", panel.Number(), panel.Title())
		switch {
		case panel == a.panel && focused:
			b.WriteString(a.theme.Cursor.Render(label))
		case panel == a.panel:
			b.WriteString(a.theme.Selected.Render(label))
		default:
			b.WriteString(a.theme.Muted.Render(label))
		}
	}

	return a.frame(a.theme.PanelFrame(focused), r, b.String())
}

// renderMain draws the pane grid area. The grid itself arrives with its own
// issue; until then the frame reports what it will hold, rather than pretending
// to show output.
func (a App) renderMain() string {
	r := a.layout.Main
	focused := a.focus == AreaGrid

	hosts := len(a.cfg.Hosts)
	body := a.theme.Muted.Render(fmt.Sprintf("%d host%s", hosts, plural(hosts)))
	if hosts == 0 {
		body = a.theme.Muted.Render("no hosts")
	}

	return a.frame(a.theme.PaneFrame(focused), r, body)
}

// renderStatusBar draws the bottom line: what is selected, how many hosts are in
// the run, and every flag that weakens a default.
func (a App) renderStatusBar() string {
	parts := []string{a.theme.Base.Render("lazycssh")}
	if a.cfg.SessionName != "" {
		parts = append(parts, a.theme.Base.Render("@"+a.cfg.SessionName))
	}
	hosts := len(a.cfg.Hosts)
	parts = append(parts, a.theme.Muted.Render(fmt.Sprintf("%d host%s", hosts, plural(hosts))))
	parts = append(parts, a.theme.Muted.Render(a.panel.Title()))
	if a.cfg.Insecure {
		parts = append(parts, a.theme.StatusInsecure.Render("HOST KEYS UNVERIFIED"))
	}

	line := strings.Join(parts, " ")
	short := a.help.ShortHelpView(a.keys.For(a.focus).ShortHelp())
	if short != "" {
		line += "  " + short
	}
	return a.theme.StatusBar.
		Width(a.layout.Width).
		MaxHeight(StatusBarHeight).
		Render(line)
}

// renderHelpOverlay draws the full help, generated from the keymap.
func (a App) renderHelpOverlay() string {
	ctx, ok := a.keys.For(a.focus).(contextHelp)
	if !ok {
		return ""
	}

	var b strings.Builder
	b.WriteString(a.theme.Title.Render("Keys — " + a.focus.String() + " has focus"))
	b.WriteString("\n\n")
	b.WriteString(a.help.FullHelpView(ctx.FullHelp()))

	return a.theme.HelpOverlay.
		MaxWidth(a.layout.Width).
		MaxHeight(a.layout.Height).
		Render(b.String())
}

// frame draws content inside a bordered box sized to a rect. The border eats
// two columns and two rows, and the inner size is clamped at zero so a tiny
// terminal cannot ask lipgloss for a negative width.
func (a App) frame(style lipgloss.Style, r Rect, content string) string {
	return style.
		Width(max(0, r.Width-2)).
		Height(max(0, r.Height-2)).
		MaxWidth(max(0, r.Width)).
		MaxHeight(max(0, r.Height)).
		Render(content)
}

// plural is the "s" that makes a count read as English.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
