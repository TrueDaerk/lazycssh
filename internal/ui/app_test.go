package ui

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ansiPattern matches the styling escape sequences, so assertions can be made
// about what the user reads rather than about how it was coloured.
var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips styling from a rendered view.
func plain(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// pressKey drives the model with a synthetic key press, which is how the TUI is
// tested: no terminal, no goroutines, just Update.
func pressKey(t *testing.T, a App, keystroke string) App {
	t.Helper()

	model, _ := a.Update(keyMsgFor(t, keystroke))
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	return next
}

// keyMsgFor synthesises a key press from a human-readable keystroke, chords
// like "alt+shift+left" and "ctrl+]" included.
func keyMsgFor(t *testing.T, keystroke string) tea.KeyPressMsg {
	t.Helper()

	var mod tea.KeyMod
	rest := keystroke
	for {
		switch {
		case strings.HasPrefix(rest, "alt+"):
			mod |= tea.ModAlt
			rest = rest[len("alt+"):]
		case strings.HasPrefix(rest, "ctrl+") && len(rest) > len("ctrl+"):
			mod |= tea.ModCtrl
			rest = rest[len("ctrl+"):]
		case strings.HasPrefix(rest, "shift+") && rest != "shift+tab":
			mod |= tea.ModShift
			rest = rest[len("shift+"):]
		default:
			goto base
		}
	}
base:
	special := map[string]rune{
		"tab": tea.KeyTab, "shift+tab": tea.KeyTab, "enter": tea.KeyEnter,
		"esc": tea.KeyEscape, "backspace": tea.KeyBackspace,
		"left": tea.KeyLeft, "right": tea.KeyRight,
		"up": tea.KeyUp, "down": tea.KeyDown, "pgup": tea.KeyPgUp,
		"pgdown": tea.KeyPgDown, "home": tea.KeyHome, "end": tea.KeyEnd,
	}
	if rest == "shift+tab" {
		return tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}
	}
	if code, ok := special[rest]; ok {
		return tea.KeyPressMsg{Code: code, Mod: mod}
	}
	r := []rune(rest)
	if len(r) != 1 {
		t.Fatalf("keyMsgFor cannot synthesise %q", keystroke)
	}
	if mod == 0 {
		return tea.KeyPressMsg{Code: r[0], Text: rest}
	}
	return tea.KeyPressMsg{Code: r[0], Mod: mod}
}

// focusGrid enters the focused pane's terminal the way a user does: an
// shift+alt+arrow. It clamps at the first pane, so calling this right after
// setup does not move the focus.
func focusGrid(t *testing.T, a App) App {
	t.Helper()
	if a.Focus() == AreaGrid {
		return a
	}
	a = pressKey(t, a, "alt+shift+left")
	if a.Focus() != AreaGrid {
		t.Fatal("alt+shift+left did not enter the grid")
	}
	return a
}

// settle executes a command and feeds every message it produces back into
// Update, following the chain until it ends - the synchronous stand-in for the
// bubbletea runtime draining the async work the model started (issue #225).
func settle(t *testing.T, a App, cmd tea.Cmd) App {
	t.Helper()
	if cmd == nil {
		return a
	}
	msg := cmd()
	if msg == nil {
		return a
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			a = settle(t, a, c)
		}
		return a
	}
	model, next := a.Update(msg)
	app, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	return settle(t, app, next)
}

// loadedApp drives the group directory read the way Init does, so a fixture
// starts with its rows the way a running program would.
func loadedApp(t *testing.T, a App) App {
	t.Helper()
	return settle(t, a, a.loadGroupsCmd())
}

// resize drives a window size message through the model.
func resize(t *testing.T, a App, width, height int) App {
	t.Helper()
	model, _ := a.Update(tea.WindowSizeMsg{Width: width, Height: height})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	return next
}

func testApp() App {
	return NewApp(Config{
		SessionName: "prod-web",
		Hosts:       []string{"web-01", "web-02", "web-03"},
		Theme:       Options{Dark: true},
	})
}

func TestNewAppDefaults(t *testing.T) {
	a := testApp()
	if a.Focus() != AreaSidebar {
		t.Fatalf("Focus() = %v, want the sidebar", a.Focus())
	}
	if a.Panel() != PanelStatus {
		t.Fatalf("Panel() = %v, want the status panel", a.Panel())
	}
	if a.HelpVisible() {
		t.Fatal("the help overlay starts open")
	}
	if a.Init() == nil {
		t.Fatal("Init returned no command; the background colour is never requested")
	}
}

func TestCustomKeyMap(t *testing.T) {
	keys := DefaultKeyMap()
	a := NewApp(Config{Keys: &keys})
	if a.keys.Help.Help().Desc != keys.Help.Help().Desc {
		t.Fatal("the configured keymap was ignored")
	}
}

func TestResizeRecomputesTheLayout(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	if a.Layout().Width != 120 || a.Layout().Height != 40 {
		t.Fatalf("Layout() = %+v", a.Layout())
	}
	if !a.Layout().SidebarVisible() {
		t.Fatal("no sidebar at 120x40")
	}
}

// The acceptance criterion: Update and View survive a resize down to 40x10, and
// well past it.
func TestViewSurvivesEverySize(t *testing.T) {
	sizes := [][2]int{
		{120, 40}, {80, 24}, {40, 10}, {30, 8}, {24, 4}, {20, 3}, {1, 1}, {0, 0},
	}

	a := testApp()
	for _, size := range sizes {
		a = resize(t, a, size[0], size[1])
		view := a.View()
		if size[0] > 0 && size[1] > 0 && view.Content == "" && !a.Layout().TooSmall {
			t.Fatalf("%dx%d rendered nothing", size[0], size[1])
		}
		for _, line := range strings.Split(plain(view.Content), "\n") {
			if width := len([]rune(line)); size[0] > 0 && width > size[0] {
				t.Fatalf("%dx%d: a line is %d columns wide, the terminal has %d",
					size[0], size[1], width, size[0])
			}
		}
	}
}

func TestViewBeforeTheFirstResize(t *testing.T) {
	if got := testApp().View().Content; got != "" {
		t.Fatalf("View() = %q before any window size message", got)
	}
}

func TestTooSmallSaysSo(t *testing.T) {
	a := resize(t, testApp(), 20, 2)
	if !a.Layout().TooSmall {
		t.Fatal("20x2 was not reported as too small")
	}
	if !strings.Contains(plain(a.View().Content), "too small") {
		t.Fatalf("View() = %q", a.View().Content)
	}

	// Narrower than the apology, the apology is cut rather than wrapped.
	a = resize(t, a, 10, 2)
	if got := plain(a.View().Content); strings.Contains(got, "\n") || len([]rune(got)) > 10 {
		t.Fatalf("View() = %q at 10x2", got)
	}
}

// Tab walks the lazygit cycle: every sidebar panel in order, then the grid,
// then round to the first panel again; shift+tab walks it backwards.
func TestTabCyclesFocus(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	if a.Panel() != PanelStatus {
		t.Fatalf("setup: Panel() = %v", a.Panel())
	}

	for _, want := range Panels()[1:] {
		a = pressKey(t, a, "tab")
		if a.Focus() != AreaSidebar || a.Panel() != want {
			t.Fatalf("Focus() = %v, Panel() = %v, want the %v panel", a.Focus(), a.Panel(), want)
		}
	}
	a = pressKey(t, a, "tab")
	if a.Focus() != AreaBroadcast {
		t.Fatalf("Focus() = %v after the last panel, want the broadcast bar", a.Focus())
	}

	// The bar is a terminal: tab is a keystroke for the targets there, and
	// ctrl+] is the way back to the app level.
	a = pressKey(t, a, "ctrl+]")
	if a.Focus() != AreaSidebar {
		t.Fatalf("Focus() = %v after ctrl+]", a.Focus())
	}

	// The grid is not a tab stop at all - it is entered with enter or an
	// alt+arrow, because inside it tab belongs to the host.
	a = pressKey(t, a, "alt+shift+left")
	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v after alt+shift+left, want the grid", a.Focus())
	}
	a = pressKey(t, a, "ctrl+]")
	a = pressKey(t, a, "shift+tab")
	if a.Focus() != AreaSidebar && a.Focus() != AreaBroadcast {
		t.Fatalf("Focus() = %v after shift+tab", a.Focus())
	}
}

func TestNumberKeysSelectPanelsAndFocusTheSidebar(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	a = pressKey(t, a, "4") // start away from the first panel

	for _, panel := range Panels() {
		// The panel's own number is the key: the Output diff panel sits on 6,
		// because 5 has meant the broadcast bar since the bar existed.
		a = pressKey(t, a, strconv.Itoa(panel.Number()))
		if a.Panel() != panel {
			t.Fatalf("key %d selected %v, want %v", panel.Number(), a.Panel(), panel)
		}
		if a.Focus() != AreaSidebar {
			t.Fatalf("key %d did not move focus to the sidebar", panel.Number())
		}
	}
}

func TestFocusIsVisibleInTheView(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	sidebarFocused := a.View().Content

	a = pressKey(t, a, "tab")
	gridFocused := a.View().Content

	if sidebarFocused == gridFocused {
		t.Fatal("moving focus changed nothing on screen")
	}
}

func TestHelpOverlayTogglesAndSwallowsTheNextKey(t *testing.T) {
	a := resize(t, testApp(), 120, 40)

	a = pressKey(t, a, "?")
	if !a.HelpVisible() {
		t.Fatal("? did not open the help")
	}
	if !strings.Contains(plain(a.View().Content), "connect a new host") {
		t.Fatalf("the overlay does not list the bindings:\n%s", plain(a.View().Content))
	}

	// While the overlay is open it is the only thing listening.
	before := a.Panel()
	a = pressKey(t, a, "2")
	if a.HelpVisible() {
		t.Fatal("a key press did not close the help")
	}
	if a.Panel() != before {
		t.Fatal("the key that closed the help also changed the panel")
	}
}

// Each column in the overlay is headed by the area it lists bindings for,
// from contextHelp.Titles() - otherwise only the box title names the focused
// area and the rest of the columns are unlabeled.
func TestHelpOverlayLabelsItsColumns(t *testing.T) {
	// Wide enough that every area's column fits without the width-based
	// column drop the help bubble does when the overlay is narrower than
	// its content (TestHelpOverlayTogglesAndSwallowsTheNextKey covers that
	// narrower case).
	a := resize(t, testApp(), 600, 60)
	a = pressKey(t, a, "?")

	view := plain(a.View().Content)
	ctx, ok := a.keys.For(a.focus).(contextHelp)
	if !ok {
		t.Fatalf("keys.For(%v) did not return a contextHelp", a.focus)
	}
	for _, title := range ctx.Titles() {
		if !strings.Contains(view, title) {
			t.Fatalf("the overlay does not label the %q column:\n%s", title, view)
		}
	}
}

// The help is a popup over the frame, not a replacement for it: the fleet
// stays visible underneath.
func TestHelpOverlayCompositesOverTheFrame(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	a = pressKey(t, a, "?")

	view := plain(a.View().Content)
	if !strings.Contains(view, "Keybindings") {
		t.Fatalf("the overlay has no title:\n%s", view)
	}
	if !strings.Contains(view, "web-01") {
		t.Fatalf("the frame is not visible under the overlay:\n%s", view)
	}
}

// The sidebar is a stack of titled boxes: every panel's title is always
// visible, only the selected panel shows its body.
func TestSidebarStacksEveryPanel(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	view := plain(a.View().Content)

	for _, want := range []string{"Status [1]", "Groups [2]", "Sessions [3]", "Command log [4]"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the sidebar does not show the %q box:\n%s", want, view)
		}
	}

	// PanelStatus is selected, so its body is on screen.
	if !strings.Contains(view, "session: prod-web") {
		t.Fatalf("the selected panel's body is missing:\n%s", view)
	}
}

func TestQuitBinding(t *testing.T) {
	a := resize(t, testApp(), 120, 40)

	model, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+q returned no command")
	}
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if msg := cmd(); msg == nil {
		t.Fatal("the quit command produced no message")
	}

	// It works from inside the help overlay too, which is where a confused
	// user is most likely to be.
	a = pressKey(t, a, "?")
	_, cmd = a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+q inside the help overlay returned no command")
	}
}

// Plain q quits wherever no input has the keyboard, lazygit style.
func TestPlainQQuits(t *testing.T) {
	a := resize(t, testApp(), 120, 40)

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Fatalf("q produced %v, want tea.Quit", msg)
	}
}

// q closes the help overlay rather than quitting the app - lazygit convention
// for the topmost overlay. ctrl+q still force-quits from there (TestQuitBinding).
func TestQClosesHelpOverlayWithoutQuitting(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	a = pressKey(t, a, "?")
	if !a.HelpVisible() {
		t.Fatal("? did not open the help")
	}

	model, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd != nil && cmd() == tea.Quit() {
		t.Fatal("q inside the help overlay quit the program")
	}
	a = model.(App)
	if a.HelpVisible() {
		t.Fatal("q did not close the help overlay")
	}

	// esc closes it too - the other half of the lazygit convention.
	a = pressKey(t, a, "?")
	a = pressKey(t, a, "esc")
	if a.HelpVisible() {
		t.Fatal("esc did not close the help overlay")
	}
}

// A q typed into an open text input is a letter, not the quit key.
func TestQIsTypeableInInputs(t *testing.T) {
	a := resize(t, testApp(), 120, 40)

	a = pressKey(t, a, ":")
	model, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	// The text input may return its own command (cursor blink); it only must
	// not be the quit.
	if cmd != nil && cmd() == tea.Quit() {
		t.Fatal("q inside the command line quit the program")
	}
	a = model.(App)
	if got := a.CommandLineValue(); got != "q" {
		t.Fatalf("CommandLineValue() = %q, want the typed letter", got)
	}
}

func TestUnknownKeyChangesNothing(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	before := a.View().Content

	a = pressKey(t, a, "z")
	if a.View().Content != before {
		t.Fatal("an unbound key changed the view")
	}
}

func TestBackgroundColorMessageRebuildsTheTheme(t *testing.T) {
	a := NewApp(Config{Hosts: []string{"h1"}})
	if a.Theme().Palette != LightPalette() {
		t.Fatal("setup: the app did not start on the light palette")
	}

	model, _ := a.Update(tea.BackgroundColorMsg{Color: lipglossBlack{}})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if next.Theme().Palette != DarkPalette() {
		t.Fatal("a dark background did not switch the palette")
	}
}

// lipglossBlack is a colour dark enough for BackgroundColorMsg.IsDark.
type lipglossBlack struct{}

func (lipglossBlack) RGBA() (r, g, b, a uint32) { return 0, 0, 0, 0xffff }

func TestStatusBarShowsTheRunAndItsWarnings(t *testing.T) {
	a := resize(t, NewApp(Config{
		SessionName: "prod-web",
		Hosts:       []string{"web-01", "web-02"},
		Theme:       Options{Dark: true},
		Insecure:    true,
	}), 120, 40)

	view := plain(a.View().Content)
	for _, want := range []string{"lazycssh", "@prod-web", "2 hosts", "HOST KEYS UNVERIFIED"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the status bar does not mention %q:\n%s", want, view)
		}
	}
}

func TestStatusBarWithoutASession(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	view := plain(a.View().Content)
	if strings.Contains(view, "@") {
		t.Fatalf("an ad hoc run rendered a session name:\n%s", view)
	}
	if !strings.Contains(view, "1 host") || strings.Contains(view, "1 hosts") {
		t.Fatalf("the host count is not singular:\n%s", view)
	}
}

func TestStatusBarShowsTheVersion(t *testing.T) {
	a := resize(t, NewApp(Config{
		Hosts:   []string{"h1"},
		Theme:   Options{Dark: true},
		Version: "1.2.3",
	}), 120, 40)

	view := plain(a.View().Content)
	if !strings.Contains(view, "lazycssh v1.2.3") {
		t.Fatalf("the status bar does not show the version:\n%s", view)
	}
}

func TestStatusBarWithoutAVersion(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	view := plain(a.View().Content)
	if !strings.Contains(view, "lazycssh") || strings.Contains(view, "lazycssh v") {
		t.Fatalf("the status bar should show the bare name without a version:\n%s", view)
	}
}

func TestMainReportsAnEmptyRun(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	if !strings.Contains(plain(a.View().Content), "no hosts") {
		t.Fatalf("an empty run does not say so:\n%s", plain(a.View().Content))
	}
}

func TestPanelTitles(t *testing.T) {
	seen := make(map[string]bool)
	// The numbers are jump keys, not positions: 5 stays the broadcast bar's,
	// so the Output diff panel skips to 6.
	numbers := []int{1, 2, 3, 4, 6}
	for i, panel := range Panels() {
		if panel.Number() != numbers[i] {
			t.Fatalf("%v.Number() = %d, want %d", panel, panel.Number(), numbers[i])
		}
		title := panel.Title()
		if title == "" || seen[title] {
			t.Fatalf("panel %d has an empty or duplicate title %q", i, title)
		}
		seen[title] = true
	}
	if got := Panel(42).Title(); got != "unknown(42)" {
		t.Fatalf("Panel(42).Title() = %q", got)
	}
}

func TestUnknownMessageIsIgnored(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	model, cmd := a.Update(struct{ name string }{"nonsense"})
	if cmd != nil {
		t.Fatal("an unknown message produced a command")
	}
	if next, ok := model.(App); !ok || next.View().Content != a.View().Content {
		t.Fatal("an unknown message changed the view")
	}
}

// An argumentless start forces nothing: no input has the keyboard, the empty
// grid names the options, and the first action is the user's call.
func TestArgumentlessStartFocusesNoInput(t *testing.T) {
	a := NewApp(Config{Theme: Options{Dark: true}})
	if a.Panel() != PanelStatus {
		t.Fatalf("Panel() = %v, want the Status panel on an empty start", a.Panel())
	}
	if a.ConnectPromptOpen() || a.CommandLineOpen() || a.Saving() {
		t.Fatal("an input has the keyboard on an empty start")
	}
}

func TestEmptyRunRendersAHint(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	view := plain(a.View().Content)
	for _, want := range []string{"no hosts", "press n and type a host", "[3] Sessions", "lazycssh <host...>"} {
		if !strings.Contains(view, want) {
			t.Errorf("the empty grid does not say %q:\n%s", want, view)
		}
	}
}

func TestEmptyRunQuitsCleanly(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	model, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	if cmd == nil {
		t.Fatal("ctrl+q on an empty run produced no command, want quit")
	}
	if msg := cmd(); msg != tea.Quit() {
		t.Fatalf("ctrl+q produced %v, want tea.Quit", msg)
	}
}
