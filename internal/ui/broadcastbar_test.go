package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// barApp builds an app with a recording sender and the broadcast bar focused.
func barApp(t *testing.T, names ...string) (App, *fakeSender) {
	t.Helper()

	a, sender, _, _ := cmdApp(t, names...)
	a = pressKey(t, a, "5")
	if a.Focus() != AreaBroadcast {
		t.Fatal("6 did not focus the broadcast bar")
	}
	return a, sender
}

// Every keystroke in the bar fans out live: the sender sees each key as it is
// typed, enter not required.
func TestBroadcastBarForwardsEveryKeystroke(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "l")
	a = pressKey(t, a, "s")
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	press(t, a, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})

	if got := strings.Join(sender.sent, ","); got != "<l>,<s>,<tab>,<ctrl+c>" {
		t.Fatalf("sent = %q, want each keystroke separately", got)
	}
}

// The bar assembles the typed line for the command log — printable text
// appends, backspace trims, enter clears — but never displays it: the panes
// show each host's own echo. The enter itself reaches the hosts as a carriage
// return.
func TestBroadcastBarTracksTheLineWithoutEchoingIt(t *testing.T) {
	a, sender := barApp(t, "web-01")

	for _, r := range "lss" {
		a = pressKey(t, a, string(r))
	}
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if a.BroadcastLine() != "ls" {
		t.Fatalf("BroadcastLine() = %q", a.BroadcastLine())
	}
	if strings.Contains(plain(a.View().Content), "ls▏") {
		t.Fatalf("the bar echoes the typed line:\n%s", plain(a.View().Content))
	}

	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if a.BroadcastLine() != "" {
		t.Fatalf("BroadcastLine() = %q after enter", a.BroadcastLine())
	}
	if sender.sent[len(sender.sent)-1] != "<enter>" {
		t.Fatalf("enter sent %q, want the enter key event", sender.sent[len(sender.sent)-1])
	}
}

// Individual keystrokes are never recorded; the assembled line is, once, on
// enter - the audit trail is for commands.
func TestBroadcastBarRecordsTheLineOnEnterOnly(t *testing.T) {
	a, _, _, log := cmdApp(t, "web-01")
	a = pressKey(t, a, "5")

	for _, r := range "uptime" {
		a = pressKey(t, a, string(r))
	}
	if log.Len() != 0 {
		t.Fatalf("typing wrote %d entries to the command log", log.Len())
	}
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if log.Len() != 1 {
		t.Fatalf("enter wrote %d entries, want exactly one", log.Len())
	}

	// An empty line records nothing.
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})
	if log.Len() != 1 {
		t.Fatalf("an empty enter wrote an entry")
	}
}

// The bar always names its target count, and the status bar says BROADCASTING
// while it has the keyboard.
func TestBroadcastBarNamesItsTargets(t *testing.T) {
	a, _ := barApp(t, "web-01", "web-02", "web-03")

	view := plain(a.View().Content)
	if !strings.Contains(view, "Broadcast [5]") {
		t.Fatalf("the bar has no title:\n%s", view)
	}
	if !strings.Contains(view, "BROADCASTING") || !strings.Contains(view, escapeKeystroke) {
		t.Fatalf("the status bar does not warn about broadcasting:\n%s", view)
	}
}

// ctrl+] hands the keyboard back to the app.
func TestBroadcastBarLeaveKey(t *testing.T) {
	a, sender := barApp(t, "web-01")
	a = pressKey(t, a, "ctrl+]")

	if a.Focus() != AreaSidebar {
		t.Fatalf("Focus() = %v after ctrl+]", a.Focus())
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the escape was forwarded: %q", sender.sent)
	}
}

// The bar survives a run without a transport by saying so.
func TestBroadcastBarWithoutATransport(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "5")
	a = pressKey(t, a, "x")

	if !strings.Contains(a.LastDelivery(), "nothing was sent") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
}

// Tab cycles into the bar after the last panel.
func TestTabReachesTheBroadcastBar(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	for range len(Panels()) {
		a = pressKey(t, a, "tab")
	}
	if a.Focus() != AreaBroadcast {
		t.Fatalf("Focus() = %v after tabbing past every panel", a.Focus())
	}
}

// Issue #133: a keystroke that reached nobody without a single error - an
// empty scope, every host down - must say so in the status line instead of
// reading as delivered typing.
func TestBroadcastBarSaysWhenNobodyReceived(t *testing.T) {
	a, sender := barApp(t, "web-01")
	sender.delivery = broadcast.Delivery{Mode: broadcast.ModeAll, Scope: 1, Targets: 0, Delivered: 0}

	a = pressKey(t, a, "l")

	if !strings.Contains(a.LastDelivery(), "no host can take input") {
		t.Fatalf("LastDelivery() = %q, want a zero-delivery warning", a.LastDelivery())
	}
}

// The same warning covers the ctrl+a a literal path.
func TestBroadcastRawSaysWhenNobodyReceived(t *testing.T) {
	a, sender := barApp(t, "web-01")
	sender.delivery = broadcast.Delivery{Mode: broadcast.ModeAll, Scope: 0, Targets: 0, Delivered: 0}

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "a")

	if !strings.Contains(a.LastDelivery(), "no host can take input") {
		t.Fatalf("LastDelivery() = %q, want a zero-delivery warning", a.LastDelivery())
	}
}
