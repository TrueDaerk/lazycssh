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

// wideBarApp is [barApp] on a terminal wide enough that every host's pane
// fits on one page: the paste hold decision reads the router's resolved
// target count, and a paged-away host must not be silently excluded from it.
func wideBarApp(t *testing.T, names ...string) (App, *fakeSender) {
	t.Helper()

	a, sender := barApp(t, names...)
	return resize(t, a, 60*(len(names)+1), 40), sender
}

// paste drives a synthetic bracketed paste, the way the terminal delivers one
// assembled message rather than a run of key presses (issue #248).
func paste(t *testing.T, a App, content string) App {
	t.Helper()

	model, _ := a.Update(tea.PasteMsg{Content: content})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	return next
}

// A multiline paste to more than one host is held rather than sent: the
// notice names the line count and the router's resolved target count, and
// nothing reaches a host until it is answered.
func TestBroadcastBarHoldsMultilinePasteToMultipleHosts(t *testing.T) {
	a, sender := wideBarApp(t, "web-01", "web-02")

	a = paste(t, a, "echo one\necho two\necho three")

	if len(sender.sent) != 0 {
		t.Fatalf("sent = %v before the paste was released", sender.sent)
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, "paste: 3 lines → 2 hosts") {
		t.Fatalf("bar does not show the hold notice:\n%s", view)
	}
	if !strings.Contains(view, "enter send") || !strings.Contains(view, "esc cancel") {
		t.Fatalf("bar does not name enter/esc:\n%s", view)
	}
}

// Enter releases a held paste verbatim: no trailing newline is added, and it
// goes out as one write, not one per line or per key.
func TestBroadcastBarReleasesHeldPasteOnEnter(t *testing.T) {
	a, sender := wideBarApp(t, "web-01", "web-02")

	content := "echo one\necho two\necho three"
	a = paste(t, a, content)
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEnter})

	if len(sender.sent) != 1 {
		t.Fatalf("sent = %v, want exactly one write", sender.sent)
	}
	if sender.sent[0] != content {
		t.Fatalf("sent %q, want the paste verbatim %q", sender.sent[0], content)
	}
	view := plain(a.View().Content)
	if strings.Contains(view, "paste:") {
		t.Fatalf("the hold notice is still showing after enter:\n%s", view)
	}
}

// Esc drops a held paste: nothing is sent and the notice clears.
func TestBroadcastBarCancelsHeldPasteOnEsc(t *testing.T) {
	a, sender := wideBarApp(t, "web-01", "web-02")

	a = paste(t, a, "echo one\necho two")
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyEsc})

	if len(sender.sent) != 0 {
		t.Fatalf("sent = %v, want nothing after esc", sender.sent)
	}
	if !strings.Contains(a.LastDelivery(), "discarded") {
		t.Fatalf("LastDelivery() = %q, want it to say the paste was discarded", a.LastDelivery())
	}
	view := plain(a.View().Content)
	if strings.Contains(view, "paste:") {
		t.Fatalf("the hold notice is still showing after esc:\n%s", view)
	}
}

// While a paste is held, keys other than enter and esc do not leak to the
// hosts: a stray keystroke must not silently answer a fleet-wide paste.
func TestBroadcastBarIgnoresOtherKeysWhileHoldingAPaste(t *testing.T) {
	a, sender := wideBarApp(t, "web-01", "web-02")

	a = paste(t, a, "echo one\necho two")
	a = pressKey(t, a, "l")

	if len(sender.sent) != 0 {
		t.Fatalf("sent = %v, want nothing while the paste is held", sender.sent)
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, "paste: 2 lines → 2 hosts") {
		t.Fatalf("the paste is no longer held:\n%s", view)
	}
}

// A single-line paste passes straight through even with several hosts in
// scope: only a multiline paste is the footgun this holds against.
func TestBroadcastBarSendsSingleLinePasteImmediately(t *testing.T) {
	a, sender := wideBarApp(t, "web-01", "web-02", "web-03")

	a = paste(t, a, "uptime")

	if len(sender.sent) != 1 || sender.sent[0] != "uptime" {
		t.Fatalf("sent = %v, want the single line sent immediately", sender.sent)
	}
	if strings.Contains(plain(a.View().Content), "paste:") {
		t.Fatalf("a single-line paste should not be held")
	}
}

// A multiline paste addressed to one host — single mode, or a working set of
// one — passes straight through: there is only one host to review it on.
func TestBroadcastBarSendsMultilinePasteImmediatelyToOneHost(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = paste(t, a, "echo one\necho two")

	if len(sender.sent) != 1 || sender.sent[0] != "echo one\necho two" {
		t.Fatalf("sent = %v, want the paste sent immediately", sender.sent)
	}
	if strings.Contains(plain(a.View().Content), "paste:") {
		t.Fatalf("a paste to one host should not be held")
	}
}

// broadcast.ModeSingle narrows the router to the focused host regardless of
// how many are in the working set: a multiline paste there is still a paste
// to one host, so it is never held.
func TestBroadcastBarSendsMultilinePasteImmediatelyInSingleMode(t *testing.T) {
	a, sender, router, _ := cmdApp(t, "web-01", "web-02", "web-03")
	a = resize(t, a, 260, 40)
	a = pressKey(t, a, "5")
	if a.Focus() != AreaBroadcast {
		t.Fatal("5 did not focus the broadcast bar")
	}

	if err := router.SetMode(broadcast.ModeSingle); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	router.SetFocus("web-01")

	a = paste(t, a, "echo one\necho two")

	if len(sender.sent) != 1 || sender.sent[0] != "echo one\necho two" {
		t.Fatalf("sent = %v, want the paste sent immediately", sender.sent)
	}
	if strings.Contains(plain(a.View().Content), "paste:") {
		t.Fatalf("a single-mode paste should not be held")
	}
}

// A paste outside the broadcast bar is not this package's footgun to guard:
// it is ignored rather than reaching a host unrouted.
func TestPasteOutsideTheBroadcastBarIsIgnored(t *testing.T) {
	a, sender, _, _ := cmdApp(t, "web-01", "web-02")

	a = paste(t, a, "echo one\necho two")

	if len(sender.sent) != 0 {
		t.Fatalf("sent = %v, want nothing without the broadcast bar focused", sender.sent)
	}
}
