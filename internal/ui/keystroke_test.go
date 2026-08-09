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

// h and l are sidebar aliases for left/right, but that only applies while the
// sidebar has focus; a focused pane is a terminal, so the plain letters still
// reach the host (issue #220).
func TestPlainHAndLReachTheHostWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")

	a = press(t, a, tea.KeyPressMsg{Code: 'h', Text: "h"})
	press(t, a, tea.KeyPressMsg{Code: 'l', Text: "l"})

	if got := string(fleet.sessions["web-01"].Written()); got != "hl" {
		t.Fatalf("host received %q, want the literal letters", got)
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
		{Code: 'q', Text: "q"},
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
	if got := len(fleet.sessions["web-01"].Written()); got != 6 {
		t.Fatalf("host received %d bytes, want every key forwarded", got)
	}
}

// ctrl+] leaves typing and lands on the Status panel, which answers where the
// keys go now.
func TestLeaveTypingReturnsToTheStatusPanel(t *testing.T) {
	a, _ := typingApp(t, "web-01", "web-02")
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt | tea.ModShift}) // onto web-02

	a = press(t, a, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if a.Focus() != AreaSidebar || a.Panel() != PanelStatus {
		t.Fatalf("Focus() = %v, Panel() = %v after ctrl+]", a.Focus(), a.Panel())
	}

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+q did not quit after leaving typing")
	}
}

// shift+alt+arrows switch panes without leaving typing; the next keystroke
// reaches the newly focused host.
func TestShiftAltArrowsSwitchThePaneWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01", "web-02")

	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt | tea.ModShift})
	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v; shift+alt+right must stay in typing", a.Focus())
	}
	press(t, a, tea.KeyPressMsg{Code: 'w', Text: "w"})

	if got := string(fleet.sessions["web-02"].Written()); got != "w" {
		t.Fatalf("web-02 received %q after switching to it", got)
	}
	if got := fleet.sessions["web-01"].Written(); len(got) != 0 {
		t.Fatalf("web-01 received %q after focus left it", got)
	}
}

// Plain alt+arrows are the shell's word navigation (issue #202): they reach
// the focused host as ESC b / ESC f instead of switching panes.
func TestAltArrowsAreWordNavigationWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01", "web-02")

	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt})
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt})

	if got := string(fleet.sessions["web-01"].Written()); got != "\x1bb\x1bf" {
		t.Fatalf("web-01 received %q, want word navigation", got)
	}
	if got := fleet.sessions["web-02"].Written(); len(got) != 0 {
		t.Fatalf("web-02 received %q; alt+arrow must not switch panes", got)
	}
	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v; alt+arrow must stay in typing", a.Focus())
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

// What reaches a remote shell is the one thing a user cannot inspect, so the
// full path is pinned down: a key press converts to emulator events
// (paneKeyEvents) and the focused host's emulator encodes them; the bytes
// land on that session's stdin.
func TestTypingKeyEncodings(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.KeyPressMsg
		want string
	}{
		{"letter", tea.KeyPressMsg{Code: 'a', Text: "a"}, "a"},
		{"shifted letter", tea.KeyPressMsg{Code: 'a', ShiftedCode: 'A', Text: "A", Mod: tea.ModShift}, "A"},
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
		{"home", tea.KeyPressMsg{Code: tea.KeyHome}, "\x1b[H"},
		{"end", tea.KeyPressMsg{Code: tea.KeyEnd}, "\x1b[F"},
		{"delete", tea.KeyPressMsg{Code: tea.KeyDelete}, "\x1b[3~"},
		{"ctrl+a", tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl}, "\x01"},
		{"ctrl+c", tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}, "\x03"},
		{"ctrl+z", tea.KeyPressMsg{Code: 'z', Mod: tea.ModCtrl}, "\x1a"},
		{"ctrl+space", tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModCtrl}, "\x00"},
		{"opt+left is word backward", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt}, "\x1bb"},
		{"opt+right is word forward", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModAlt}, "\x1bf"},
		{"opt+backspace kills the previous word", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModAlt}, "\x1b\x7f"},
		{"opt+delete kills the next word", tea.KeyPressMsg{Code: tea.KeyDelete, Mod: tea.ModAlt}, "\x1bd"},
		{"cmd+left is line start", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper}, "\x01"},
		{"cmd+right is line end", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper}, "\x05"},
		{"cmd+backspace kills to line start", tea.KeyPressMsg{Code: tea.KeyBackspace, Mod: tea.ModSuper}, "\x15"},
		// ctrl+arrows are readline word movement, not paging (issue #208):
		// nothing in lazycssh claims them, and they reach the host as the
		// ESC b / ESC f readline defaults.
		{"ctrl+left is word backward", tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl}, "\x1bb"},
		{"ctrl+right is word forward", tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl}, "\x1bf"},
		{"alt+b is meta", tea.KeyPressMsg{Code: 'b', Mod: tea.ModAlt}, "\x1bb"},
		{"alt+dot is meta", tea.KeyPressMsg{Code: '.', Mod: tea.ModAlt}, "\x1b."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a, fleet := typingApp(t, "web-01")
			press(t, a, tc.msg)
			if got := fleet.sessions["web-01"].Written(); got != tc.want {
				t.Fatalf("host received %q, want %q", got, tc.want)
			}
		})
	}
}

// The encoding is per host: a host whose app turned on application cursor
// keys receives the SS3 form while a plain host receives CSI — the same key
// press, two different byte sequences (issue #206).
func TestTypingHonoursTheHostsCursorKeyMode(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	fleet.sessions["web-01"].Emit("\x1b[?1h") // remote app: application cursor keys

	press(t, a, tea.KeyPressMsg{Code: tea.KeyUp})
	if got := fleet.sessions["web-01"].Written(); got != "\x1bOA" {
		t.Fatalf("host received %q, want the application-mode encoding", got)
	}
}
