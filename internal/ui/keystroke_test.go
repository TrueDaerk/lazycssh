package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// press drives one raw key press and returns the model.
func press(t *testing.T, a App, msg tea.KeyPressMsg) App {
	t.Helper()

	model, _ := a.Update(msg)
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	return next
}

// typingApp builds an app over a live fake fleet with the first host connected
// and its pane focused, so keystrokes have somewhere to go.
func typingApp(t *testing.T, names ...string) (App, *fakeFleet) {
	t.Helper()

	a, fleet, _, _ := statusApp(t, names...)
	for _, name := range names {
		fleet.connect(t, name)
	}
	a = focusGrid(t, a)
	if a.Focus() != AreaGrid {
		t.Fatal("setup: the grid does not have focus")
	}
	return a, fleet
}

// A focused pane is a terminal: a plain key press reaches that host, and only
// that host, immediately.
func TestTypingReachesTheFocusedHostOnly(t *testing.T) {
	a, fleet := typingApp(t, "web-01", "web-02")

	a = press(t, a, tea.KeyPressMsg{Code: 'l', Text: "l"})
	a = press(t, a, tea.KeyPressMsg{Code: 's', Text: "s"})
	press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := fleet.sessions["web-01"].Written(); string(got) != "ls\r" {
		t.Fatalf("web-01 received %q, want the keystrokes", got)
	}
	if got := fleet.sessions["web-02"].Written(); len(got) != 0 {
		t.Fatalf("web-02 received %q, want nothing", got)
	}
}

// tab and ctrl+c belong to the remote shell while typing: completion and
// interrupt happen on the host, not here.
func TestTypingForwardsTabAndCtrlC(t *testing.T) {
	a, fleet := typingApp(t, "web-01")

	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	model, cmd := a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatal("ctrl+c produced a command; it must not quit the program")
	}
	a = model.(App)

	if got := string(fleet.sessions["web-01"].Written()); got != "\t\x03" {
		t.Fatalf("host received %q, want tab then interrupt", got)
	}
	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v; tab must not cycle while typing", a.Focus())
	}
}

// While typing, lazycssh keeps only the reserved escape and the alt/shift
// chords. Everything else - including every app-level binding - is a keystroke
// for the host.
func TestAppBindingsAreForwardedWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	router := a.cfg.Targets

	before := a.Panel()
	for _, msg := range []tea.KeyPressMsg{
		{Code: 'b', Text: "b"},
		{Code: '2', Text: "2"},
		{Code: ':', Text: ":"},
		{Code: '?', Text: "?"},
		{Code: 'q', Mod: tea.ModCtrl},
	} {
		a = press(t, a, msg)
	}

	if a.Panel() != before || a.Focus() != AreaGrid {
		t.Fatal("a forwarded binding still acted on the app")
	}
	if router.Mode() != broadcast.ModeAll {
		t.Fatalf("a forwarded binding switched the broadcast mode to %v", router.Mode())
	}
	if a.CommandLineOpen() || a.HelpVisible() {
		t.Fatal("a forwarded binding opened a prompt or the help")
	}
	if got := len(fleet.sessions["web-01"].Written()); got != 5 {
		t.Fatalf("host received %d bytes, want every key forwarded", got)
	}
}

// ctrl+] leaves typing and lands on the Hosts panel with the cursor on the
// host that was just typed to.
func TestLeaveTypingReturnsToTheHostsPanel(t *testing.T) {
	a, _ := typingApp(t, "web-01", "web-02")
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}) // onto web-02

	a = press(t, a, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if a.Focus() != AreaSidebar || a.Panel() != PanelHosts {
		t.Fatalf("Focus() = %v, Panel() = %v after ctrl+]", a.Focus(), a.Panel())
	}
	if a.SelectedHost() != "web-02" {
		t.Fatalf("SelectedHost() = %q, want the host just typed to", a.SelectedHost())
	}

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+q did not quit after leaving typing")
	}
}

// alt+arrows switch panes without leaving typing; the next keystroke reaches
// the newly focused host.
func TestAltArrowsSwitchThePaneWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01", "web-02")

	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})
	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v; alt+right must stay in typing", a.Focus())
	}
	press(t, a, tea.KeyPressMsg{Code: 'w', Text: "w"})

	if got := string(fleet.sessions["web-02"].Written()); got != "w" {
		t.Fatalf("web-02 received %q after switching to it", got)
	}
	if got := fleet.sessions["web-01"].Written(); len(got) != 0 {
		t.Fatalf("web-01 received %q after focus left it", got)
	}
}

// Typing into a host that cannot take input says so instead of dropping the
// keystrokes silently.
func TestTypingIntoADeadHostSaysSo(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.fail(t, "web-01")
	a = focusGrid(t, a)

	a = press(t, a, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if !strings.Contains(a.LastDelivery(), "not connected") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
}

func TestTypingWithoutATransport(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = focusGrid(t, a)
	a = press(t, a, tea.KeyPressMsg{Code: 'x', Text: "x"})

	if !strings.Contains(a.LastDelivery(), "nothing was sent") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
}

// The status bar always names where keystrokes go, and it survives NoColor
// because the words carry the meaning.
func TestStatusBarNamesTheTypingTarget(t *testing.T) {
	a, _ := typingApp(t, "web-01")

	view := plain(a.View().Content)
	if !strings.Contains(view, "TYPING web-01") || !strings.Contains(view, escapeKeystroke) {
		t.Fatalf("the status bar does not say where keys go:\n%s", view)
	}

	a = press(t, a, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if strings.Contains(plain(a.View().Content), "TYPING") {
		t.Fatal("the typing marker survived leaving")
	}
}

// What reaches a remote shell is the one thing a user cannot inspect, so every
// mapping is written down.
func TestKeystrokeBytes(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		want string
	}{
		{"letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, "a"},
		{"digit", tea.KeyPressMsg{Code: '7', Text: "7"}, "7"},
		{"space", tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}, " "},
		{"enter is a carriage return", tea.KeyPressMsg{Code: tea.KeyEnter}, "\r"},
		{"tab", tea.KeyPressMsg{Code: tea.KeyTab}, "\t"},
		{"shift+tab", tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift}, "\x1b[Z"},
		{"backspace", tea.KeyPressMsg{Code: tea.KeyBackspace}, "\x7f"},
		{"escape", tea.KeyPressMsg{Code: tea.KeyEscape}, "\x1b"},
		{"up", tea.KeyPressMsg{Code: tea.KeyUp}, "\x1b[A"},
		{"down", tea.KeyPressMsg{Code: tea.KeyDown}, "\x1b[B"},
		{"right", tea.KeyPressMsg{Code: tea.KeyRight}, "\x1b[C"},
		{"left", tea.KeyPressMsg{Code: tea.KeyLeft}, "\x1b[D"},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, "\x1b[3~"},
		{"ctrl+a", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "\x01"},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "\x03"},
		{"ctrl+d", tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}, "\x04"},
		{"ctrl+r", tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}, "\x12"},
		{"ctrl+z", tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}, "\x1a"},
		{"ctrl+space", tea.KeyPressMsg{Code: ' ', Mod: tea.ModCtrl}, "\x00"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(keystrokeBytes(tc.msg)); got != tc.want {
				t.Fatalf("keystrokeBytes() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestKeystrokeBytesIgnoresWhatItCannotEncode(t *testing.T) {
	if got := keystrokeBytes(tea.KeyPressMsg{}); got != nil {
		t.Fatalf("keystrokeBytes(zero) = %q", got)
	}
}
