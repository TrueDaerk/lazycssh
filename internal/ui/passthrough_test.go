package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// enterPassthrough switches the app into raw keystroke mode.
func enterPassthrough(t *testing.T, a App) App {
	t.Helper()

	model, _ := a.Update(tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if !next.Passthrough() {
		t.Fatal("ctrl+] did not enter passthrough")
	}
	return next
}

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

// The acceptance criterion: the escape key back to normal mode is shown in the
// status bar while passthrough is active.
func TestPassthroughSaysHowToLeave(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01")
	a = enterPassthrough(t, a)

	view := plain(a.View().Content)
	if !strings.Contains(view, "PASSTHROUGH") || !strings.Contains(view, "ctrl+]") {
		t.Fatalf("the status bar does not say how to leave:\n%s", view)
	}

	a = press(t, a, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if a.Passthrough() {
		t.Fatal("ctrl+] did not leave passthrough")
	}
	if strings.Contains(plain(a.View().Content), "PASSTHROUGH") {
		t.Fatal("the passthrough marker survived leaving it")
	}
}

// tab reaches the remote shell rather than cycling focus, which is the whole
// point: completion happens on the host, not here.
func TestTabIsForwardedRatherThanCyclingFocus(t *testing.T) {
	a, sender, _, _ := cmdApp(t, "web-01")
	before := a.Focus()

	a = enterPassthrough(t, a)
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyTab})

	if a.Focus() != before {
		t.Fatalf("tab moved focus to %v instead of reaching the host", a.Focus())
	}
	if len(sender.sent) != 1 || sender.sent[0] != "\t" {
		t.Fatalf("sent = %q", sender.sent)
	}
}

// The acceptance criterion: ctrl+c reaches the remote hosts and does not kill
// lazycssh.
func TestCtrlCReachesTheHosts(t *testing.T) {
	a, sender, _, _ := cmdApp(t, "web-01", "web-02")
	a = enterPassthrough(t, a)

	model, cmd := a.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd != nil {
		t.Fatal("ctrl+c produced a command; it must not quit the program")
	}
	if !next.Passthrough() {
		t.Fatal("ctrl+c left passthrough")
	}
	if len(sender.sent) != 1 || sender.sent[0] != "\x03" {
		t.Fatalf("sent = %q, want the interrupt byte", sender.sent)
	}
}

// While passthrough is on, lazycssh has one binding left. Everything else is a
// keystroke for the hosts.
func TestOwnBindingsAreSuspended(t *testing.T) {
	a, sender, router, log := cmdApp(t, "web-01", "web-02")
	a = enterPassthrough(t, a)

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

	if a.Panel() != before {
		t.Fatalf("a suspended binding still changed the panel to %v", a.Panel())
	}
	if router.Mode() != broadcast.ModeAll {
		t.Fatalf("a suspended binding switched the broadcast mode to %v", router.Mode())
	}
	if a.CommandLineOpen() || a.HelpVisible() {
		t.Fatal("a suspended binding opened a prompt or the help")
	}
	if len(sender.sent) != 5 {
		t.Fatalf("sent = %q, want every key forwarded", sender.sent)
	}

	// Raw keystrokes are never recorded: this is the mode a password is typed
	// in, and the audit trail is for commands.
	if log.Len() != 0 {
		t.Fatalf("passthrough wrote %d entries to the command log", log.Len())
	}
}

// ctrl+q quits only outside passthrough, where it is a binding rather than a
// keystroke for the host.
func TestQuitWorksAgainAfterLeavingPassthrough(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01")
	a = enterPassthrough(t, a)
	a = press(t, a, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})

	_, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("ctrl+q did not quit after leaving passthrough")
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

// Sending raw keys to several hosts means their replies are answers to
// different questions. The panes keep them apart, and the label says so.
func TestPassthroughWarnsWhenSeveralHostsAnswer(t *testing.T) {
	a, _, router, _ := cmdApp(t, "web-01", "web-02", "web-03")
	a = enterPassthrough(t, a)

	label := plain(a.View().Content)
	if !strings.Contains(label, "3 hosts answer separately") {
		t.Fatalf("the label does not warn about divergent replies:\n%s", label)
	}
	if !strings.Contains(label, "press s for one pane") {
		t.Fatalf("the label does not offer single mode:\n%s", label)
	}

	if err := router.SetMode(broadcast.ModeSingle); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	router.SetFocus("web-01")
	if got := plain(a.View().Content); strings.Contains(got, "answer separately") {
		t.Fatalf("single mode still warns about several hosts:\n%s", got)
	}
}

func TestPassthroughWithoutATransport(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = enterPassthrough(t, a)
	a = press(t, a, tea.KeyPressMsg{Code: 'x', Text: "x"})

	if !strings.Contains(a.LastDelivery(), "nothing was sent") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
	if !a.Passthrough() {
		t.Fatal("a run with no transport dropped out of passthrough")
	}
}

func TestPassthroughHintNamesTheEscape(t *testing.T) {
	if !strings.Contains(passthroughHint(), escapeKeystroke) {
		t.Fatalf("passthroughHint() = %q", passthroughHint())
	}
}

func TestDescribeKeystrokeDoesNotEchoTypedText(t *testing.T) {
	if got := describeKeystroke([]byte("hunter2")); strings.Contains(got, "hunter2") {
		t.Fatalf("describeKeystroke() = %q", got)
	}
	if got := describeKeystroke([]byte{'\t'}); got != `"\t"` {
		t.Fatalf("describeKeystroke() = %q", got)
	}
}
