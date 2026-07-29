package ui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
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
	// Hosts are the host identifiers of the run, in order. It is used when
	// there is no [Config.Fleet], which is how the views are tested without a
	// transport.
	Hosts []string
	// Fleet is the live transport. When set it is the source of truth for the
	// host list, the connection states and the counts.
	Fleet Fleet
	// Targets reports the broadcast scope. Nil means the run has no router
	// yet, and the status panel says so rather than guessing a count.
	Targets Targeter
	// WorkingSet is the current subject of work: what the Status panel reports
	// and what the Groups panel lists and switches between.
	WorkingSet WorkingSets
	// Sessions is the saved-session store the Sessions panel lists and writes.
	Sessions SessionStore
	// RunPatterns are the host arguments as the user typed them, kept so
	// saving the run writes patterns rather than expanded hostnames.
	RunPatterns []string
	// RunDefaults are the connection options the run was started with, written
	// into a saved session.
	RunDefaults sessions.HostOptions
	// Sender delivers commands to the active broadcast set. Nil means the run
	// has no transport yet and the command line says so rather than pretending
	// to send.
	Sender CommandLine
	// Recorder receives the commands that were sent, for the audit trail.
	Recorder Recorder
	// CommandLog is the audit trail of what was sent this run. Nil means the
	// panel says so rather than pretending the run sent nothing.
	CommandLog CommandLog
	// Logging reports that session output is being written to disk, which is
	// off by default and must be visible for the whole run while it is on.
	Logging bool
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

	filter      textinput.Model
	saveInput   textinput.Model
	cmdInput    textinput.Model
	searchInput textinput.Model

	// scroll is each pane's scrollback offset in wrapped lines from the
	// bottom; a missing entry is the tail. searchTerm is the one term every
	// pane highlights.
	scroll     map[string]int
	searchTerm string

	cmdHistory    []string
	cmdHistoryPos int
	lastDelivery  string

	sessionRows      []sessionRow
	sessionsErr      error
	saveErr          error
	confirmOverwrite bool

	focus         Area
	panel         Panel
	paneIndex     int
	page          int
	passthrough   bool
	hostCursor    int
	groupCursor   int
	sessionCursor int
	logCursor     int
	showHelp      bool
	fullScreen    bool
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

	filter := textinput.New()
	filter.Placeholder = "filter hosts"
	filter.Prompt = ""

	save := textinput.New()
	save.Placeholder = "session name"
	save.Prompt = ""

	command := textinput.New()
	command.Placeholder = "command"
	command.Prompt = ""

	search := textinput.New()
	search.Placeholder = "search"
	search.Prompt = ""

	a := App{
		cfg:         cfg,
		keys:        keys,
		theme:       theme,
		help:        h,
		filter:      filter,
		saveInput:   save,
		cmdInput:    command,
		searchInput: search,
		scroll:      make(map[string]int),
		focus:       AreaSidebar,
		panel:       PanelStatus,
	}
	if len(a.hostIDs()) == 0 {
		// An argumentless start has nothing to show yet; the saved sessions
		// are the thing the user came to pick from.
		a.panel = PanelSessions
	}
	return a.loadSessions()
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

	case CommandResendMsg:
		// Resending goes through the same path as typing it: the current
		// broadcast set, the same report, the same audit entry.
		return a.sendCommand(msg.Command)

	case SessionsChangedMsg:
		return a.loadSessions(), nil

	case FleetUpdatedMsg:
		// Nothing to store: the panels read the fleet's live state when they
		// render. Redrawing is the whole effect.
		return a.followFocus(), nil

	case SessionOutputMsg:
		// Nothing to store here either: the pane reads the scrollback when it
		// renders. Redrawing is the whole effect.
		return a, nil

	case HostsChangedMsg:
		return a.withHosts(msg.Hosts).followFocus(), nil

	case tea.KeyPressMsg:
		return a.handleKey(msg)
	}

	return a, nil
}

// handleKey dispatches a key press. Bindings are matched by area, so a key
// means one thing at a time; see [KeyMap].
func (a App) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// Passthrough owns the keyboard entirely, except for the one key that gets
	// it back. A shell that cannot see tab or ctrl+c is not a shell.
	if a.passthrough {
		return a.handlePassthroughKey(msg)
	}

	// The command line has the keyboard while it is open: a command containing
	// a "b" must not switch the broadcast mode, and ctrl+c while editing must
	// not reach forty machines.
	if a.cmdInput.Focused() {
		return a.handleCommandLineKey(msg)
	}

	// The save prompt has the keyboard while it is open, for the same reason
	// the filter does: a session called "x" has to be nameable.
	if a.Saving() {
		return a.handleSaveKey(msg)
	}

	// The filter input has the keyboard while it is open: a host called "x"
	// must be typeable without closing a pane.
	if a.filter.Focused() {
		return a.handleFilterKey(msg)
	}

	// So does the scrollback search, for the same reason.
	if a.searchInput.Focused() {
		return a.handleSearchKey(msg)
	}

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

	case key.Matches(msg, a.keys.CommandLine):
		return a.openCommandLine(), nil

	case key.Matches(msg, a.keys.Passthrough):
		return a.togglePassthrough(), nil

	case key.Matches(msg, a.keys.BroadcastAll):
		return a.setBroadcastMode(broadcast.ModeAll), nil
	case key.Matches(msg, a.keys.BroadcastSelected):
		return a.setBroadcastMode(broadcast.ModeSelected), nil
	case key.Matches(msg, a.keys.BroadcastSingle):
		// Single follows the focused pane, because "this host only" means the
		// pane the user is looking at.
		next := a.setBroadcastMode(broadcast.ModeSingle)
		return next.syncFocusTarget(), nil
	case key.Matches(msg, a.keys.BroadcastFleet):
		return a.setBroadcastMode(broadcast.ModeFleet), nil

	case key.Matches(msg, a.keys.NextFailure):
		// Global, not a grid key: "which host went wrong" is the question
		// whatever has focus.
		return a.jumpToNextFailure().syncFocusTarget(), nil
	}

	if panel, ok := a.panelForKey(msg); ok {
		a.panel = panel
		a.focus = AreaSidebar
		return a, nil
	}

	// Everything below here is dispatched by focus, so the same key press means
	// one thing at a time. The bindings of the area that does not have focus
	// are not consulted at all.
	switch a.focus {
	case AreaSidebar:
		return a.handleSidebarKey(msg)
	case AreaGrid:
		return a.handleGridKey(msg)
	default:
		return a, nil
	}
}

// handleFilterKey feeds the filter input, which owns the keyboard while it is
// open.
func (a App) handleFilterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		// Keep the filter, give the keyboard back.
		a.filter.Blur()
		return a, nil
	case "esc":
		// Abandon it: esc undoes, enter confirms.
		a.filter.SetValue("")
		a.filter.Blur()
		a.hostCursor = 0
		return a, nil
	}

	var cmd tea.Cmd
	a.filter, cmd = a.filter.Update(msg)
	a.hostCursor = clamp(a.hostCursor, 0, max(0, len(a.hostRows())-1))
	return a, cmd
}

// handleSidebarKey moves within the panel list, or within the selected panel
// when that panel owns a list of its own.
func (a App) handleSidebarKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch a.panel {
	case PanelHosts:
		return a.handleHostsKey(msg)
	case PanelGroups:
		return a.handleGroupsKey(msg)
	case PanelSessions:
		return a.handleSessionsKey(msg)
	case PanelCommandLog:
		return a.handleLogKey(msg)
	}

	switch {
	case key.Matches(msg, a.keys.Up):
		return a.movePanel(-1), nil
	case key.Matches(msg, a.keys.Down):
		return a.movePanel(+1), nil
	case key.Matches(msg, a.keys.Choose):
		// Choosing a host is choosing its pane; the panel that owns the list
		// says which host, and the grid is where the user wanted to end up.
		a.focus = AreaGrid
		return a, nil
	}
	return a, nil
}

// handleSaveKey drives the save-as prompt and the overwrite question. An
// existing session is never replaced without the user answering for it.
func (a App) handleSaveKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if a.confirmOverwrite {
		switch msg.String() {
		case "y", "Y":
			a.confirmOverwrite = false
			return a.commitSave(true)
		default:
			return a.cancelSave(), nil
		}
	}

	switch msg.String() {
	case "enter":
		return a.commitSave(false)
	case "esc":
		return a.cancelSave(), nil
	}

	var cmd tea.Cmd
	a.saveInput, cmd = a.saveInput.Update(msg)
	return a, cmd
}

// handleLogKey drives the Command log panel: the arrows move through the
// history and enter sends an entry again.
func (a App) handleLogKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	entries := len(a.logEntries())

	switch {
	case key.Matches(msg, a.keys.Up):
		if a.logCursor <= 0 {
			return a.movePanel(-1), nil
		}
		return a.moveLogCursor(-1), nil
	case key.Matches(msg, a.keys.Down):
		if a.logCursor >= entries-1 {
			return a.movePanel(+1), nil
		}
		return a.moveLogCursor(+1), nil
	case key.Matches(msg, a.keys.Choose):
		return a.resendSelectedCommand()
	}
	return a, nil
}

// handleSessionsKey drives the Sessions panel.
func (a App) handleSessionsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := len(a.sessionRows)

	switch {
	case key.Matches(msg, a.keys.Up):
		if a.sessionCursor <= 0 {
			return a.movePanel(-1), nil
		}
		return a.moveSessionCursor(-1), nil
	case key.Matches(msg, a.keys.Down):
		if a.sessionCursor >= rows-1 {
			return a.movePanel(+1), nil
		}
		return a.moveSessionCursor(+1), nil
	case key.Matches(msg, a.keys.Choose):
		return a.launchSelectedSession(false)
	case key.Matches(msg, a.keys.Toggle):
		// Space merges rather than replaces, which is the same "add to what I
		// have" meaning it carries in the Hosts panel.
		return a.launchSelectedSession(true)
	case key.Matches(msg, a.keys.SaveSet):
		return a.beginSave(), nil
	}
	return a, nil
}

// handleGroupsKey drives the Groups panel: the arrows move the group cursor and
// enter makes that group the working set, which is the one keystroke the panel
// exists for.
func (a App) handleGroupsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := len(a.groupRows())

	switch {
	case key.Matches(msg, a.keys.Up):
		if a.groupCursor <= 0 {
			return a.movePanel(-1), nil
		}
		return a.moveGroupCursor(-1), nil
	case key.Matches(msg, a.keys.Down):
		if a.groupCursor >= rows-1 {
			return a.movePanel(+1), nil
		}
		return a.moveGroupCursor(+1), nil
	case key.Matches(msg, a.keys.Choose):
		next, err := a.activateSelectedGroup()
		if err != nil {
			// A working set that cannot be activated is a bug in the model,
			// not something the user can fix; it must not take the run down.
			return a, nil
		}
		return next, nil
	case key.Matches(msg, a.keys.NextChunk):
		if a.cfg.WorkingSet != nil {
			a.cfg.WorkingSet.Next()
		}
		return a, nil
	case key.Matches(msg, a.keys.PrevChunk):
		if a.cfg.WorkingSet != nil {
			a.cfg.WorkingSet.Prev()
		}
		return a, nil
	}
	return a, nil
}

// handleHostsKey drives the Hosts panel: the arrows move the host cursor rather
// than the panel selection, because the list is what the user came for.
func (a App) handleHostsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := len(a.hostRows())

	switch {
	case key.Matches(msg, a.keys.Up):
		// Off the top of the list is the panel above it, so the panel list
		// stays reachable with the same keys that drive the host list.
		if a.hostCursor <= 0 {
			return a.movePanel(-1), nil
		}
		return a.moveHostCursor(-1), nil
	case key.Matches(msg, a.keys.Down):
		if a.hostCursor >= rows-1 {
			return a.movePanel(+1), nil
		}
		return a.moveHostCursor(+1), nil
	case key.Matches(msg, a.keys.Toggle):
		return a.toggleSelectedHost(), nil
	case key.Matches(msg, a.keys.Choose):
		return a.focusSelectedHost(), nil
	case key.Matches(msg, a.keys.Filter):
		a.filter.Focus()
		return a, nil
	}

	if next, handled := a.handleSelectionKey(msg.String()); handled {
		return next, nil
	}
	return a, nil
}

// handleGridKey moves between panes.
//
// Left and right step through the panes in host order; up and down do the same
// until the grid geometry exists, at which point they move by a row. Neither
// wraps: stepping off the last pane onto the first is how a user types into the
// machine at the other end of the fleet.
func (a App) handleGridKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, a.keys.PaneLeft):
		return a.movePane(-1).followFocus(), nil
	case key.Matches(msg, a.keys.PaneRight):
		return a.movePane(+1).followFocus(), nil
	case key.Matches(msg, a.keys.PaneUp):
		return a.movePane(-a.grid().Columns).followFocus(), nil
	case key.Matches(msg, a.keys.PaneDown):
		return a.movePane(+a.grid().Columns).followFocus(), nil
	case key.Matches(msg, a.keys.NextPage):
		return a.pageBy(+1), nil
	case key.Matches(msg, a.keys.PrevPage):
		return a.pageBy(-1), nil
	case key.Matches(msg, a.keys.FullScreen):
		a.fullScreen = !a.fullScreen
		return a, nil
	case key.Matches(msg, a.keys.Reconnect):
		if id := a.FocusedHost(); id != "" {
			return a, func() tea.Msg { return ReconnectHostMsg{ID: id} }
		}
		return a, nil
	case key.Matches(msg, a.keys.ClosePane):
		if id := a.FocusedHost(); id != "" {
			return a, func() tea.Msg { return CloseHostMsg{ID: id} }
		}
		return a, nil

	case key.Matches(msg, a.keys.ScrollUp):
		return a.scrollBy(+a.scrollPage()), nil
	case key.Matches(msg, a.keys.ScrollDown):
		return a.scrollBy(-a.scrollPage()), nil
	case key.Matches(msg, a.keys.ScrollTop):
		return a.scrollToTop(), nil
	case key.Matches(msg, a.keys.ScrollBottom):
		return a.scrollToBottom(), nil
	case key.Matches(msg, a.keys.SearchPane):
		return a.openSearch(), nil
	case key.Matches(msg, a.keys.NextMatch):
		return a.stepMatch(-1), nil
	case key.Matches(msg, a.keys.PrevMatch):
		return a.stepMatch(+1), nil
	case key.Matches(msg, a.keys.ClearSearch):
		return a.clearSearch(), nil
	}
	return a, nil
}

// grid is the current tiling of the main area.
func (a App) grid() Grid { return TileGrid(a.layout.Main, len(a.hostIDs())) }

// Grid returns the tiling the view is drawing, which is what the tests and the
// paging logic ask about.
func (a App) Grid() Grid { return a.grid() }

// FullScreen reports whether one pane fills the main area.
func (a App) FullScreen() bool { return a.fullScreen }

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

	bottom := a.renderStatusBar()
	if a.cmdInput.Focused() {
		bottom = a.renderCommandLine()
	}
	if a.searchInput.Focused() {
		bottom = a.renderSearchLine()
	}

	view := lipgloss.JoinVertical(lipgloss.Left, body, bottom)
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

		// Only the selected panel opens. Five open panels on an 80-column
		// terminal would show none of them usefully.
		if panel == a.panel {
			if body := a.panelBody(panel, r.Width-2, panelBodyHeight(r, len(Panels()))); body != "" {
				b.WriteString("\n")
				b.WriteString(indent(body))
			}
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

	if len(a.hostIDs()) == 0 {
		// The empty state says what to do next rather than showing an empty
		// frame: this is the argumentless start.
		hint := "no hosts\n\n" +
			"pick a session in [4] Sessions and press enter,\n" +
			"or start with hosts: lazycssh <host...>"
		return a.frame(a.theme.PaneFrame(focused, false), r, a.theme.Muted.Render(hint))
	}

	// Full screen is one pane in the whole area, which is what reading a stack
	// trace or driving an interactive program needs.
	if a.fullScreen {
		return a.renderPane(a.paneIndex, r, focused)
	}

	g := a.grid()
	if g.Empty() {
		return a.frame(a.theme.PaneFrame(focused, false), r, a.theme.Muted.Render("no room for a pane"))
	}

	first := a.clampedPage(g) * g.PerPage

	var rows []string
	for row := range g.Rows {
		var cells []string
		for col := range g.Columns {
			slot := row*g.Columns + col
			if slot >= len(g.Cells) {
				break
			}
			host := first + slot
			if host >= len(a.hostIDs()) {
				// An empty slot on the last page keeps the panes the size they
				// were, rather than letting them jump about as hosts come and go.
				cells = append(cells, a.frame(a.theme.Pane, g.Cells[slot], ""))
				continue
			}
			cells = append(cells, a.renderPane(host, g.Cells[slot], focused))
		}
		if len(cells) > 0 {
			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, rows...)
}

// renderPane draws one host's pane: a one-line header naming the host, then
// the session's scrollback following its tail.
func (a App) renderPane(host int, cell Rect, gridFocused bool) string {
	if host < 0 || host >= len(a.hostIDs()) {
		return a.frame(a.theme.Pane, cell, "")
	}

	focused := gridFocused && host == a.paneIndex

	// The border eats two columns and rows, the header the top line of what
	// remains.
	content := a.paneHeader(host, cell.Width-2, focused)
	if body := a.paneBody(a.hostIDs()[host], cell.Width-2, cell.Height-3); body != "" {
		content = content + "\n" + body
	}

	return a.frame(a.theme.PaneFrame(focused, a.commandFailed(a.hostIDs()[host])), cell, content)
}

// renderStatusBar draws the bottom line: what is selected, how many hosts are in
// the run, and every flag that weakens a default.
func (a App) renderStatusBar() string {
	parts := []string{a.theme.Base.Render("lazycssh")}
	if a.cfg.SessionName != "" {
		parts = append(parts, a.theme.Base.Render("@"+a.cfg.SessionName))
	}
	hosts := len(a.hostIDs())
	parts = append(parts, a.theme.Muted.Render(fmt.Sprintf("%d host%s", hosts, plural(hosts))))
	if a.cfg.Targets != nil {
		scope := a.theme.Base
		if a.cfg.Targets.Warning() {
			scope = a.theme.StatusWarning
		}
		parts = append(parts, scope.Render(a.cfg.Targets.Describe()))
	}
	if label := a.windowLabel(); label != "" {
		parts = append(parts, a.theme.Muted.Render(label))
	}
	if a.lastDelivery != "" {
		parts = append(parts, a.theme.Muted.Render(a.lastDelivery))
	}
	if summary := a.failureSummary(); summary != "" {
		parts = append(parts, summary)
	}
	if label := a.scrollLabel(); label != "" {
		// A pane that is not following its tail says so: fresh output landing
		// behind a frozen window must not look like a quiet host.
		parts = append(parts, a.theme.StatusWarning.Render(label))
	}
	if a.searchTerm != "" {
		parts = append(parts, a.theme.Muted.Render(fmt.Sprintf("search %q", a.searchTerm)))
	}

	// The flags that weaken a default live on the status bar as well as in the
	// Status panel, because the bar is the one thing that is on screen whatever
	// the user has scrolled to.
	parts = append(parts, a.activeFlags()...)

	if a.passthrough {
		// Everything else is suspended, so the status bar is the only thing
		// that can say how to get the keyboard back.
		return a.theme.StatusInsecure.
			Width(a.layout.Width).
			MaxHeight(StatusBarHeight).
			Render(a.passthroughLabel())
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

// indent shifts a panel body one column in, so it reads as belonging to the
// panel above it rather than as another entry in the list.
func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = " " + line
	}
	return strings.Join(lines, "\n")
}

// panelBodyHeight is how many rows the open panel's body may use: the sidebar
// minus its border and minus the one line each panel title costs.
func panelBodyHeight(sidebar Rect, panels int) int {
	return max(1, sidebar.Height-2-panels)
}

// setBroadcastMode switches the broadcast scope. A run with no router yet has
// no scope to switch, and says nothing rather than pretending.
func (a App) setBroadcastMode(m broadcast.Mode) App {
	if a.cfg.Targets == nil {
		return a
	}
	// An invalid mode cannot come from a binding, and a router that refuses one
	// is a bug in this file rather than something the user can act on.
	_ = a.cfg.Targets.SetMode(m)
	return a
}

// syncFocusTarget tells the router which pane has focus, which is what single
// mode sends to.
func (a App) syncFocusTarget() App {
	if a.cfg.Targets == nil {
		return a
	}
	a.cfg.Targets.SetFocus(a.FocusedHost())
	return a
}

// BroadcastMode is the scope the next keystroke goes to.
func (a App) BroadcastMode() broadcast.Mode { return a.broadcastMode() }
