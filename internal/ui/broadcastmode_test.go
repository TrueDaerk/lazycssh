package ui

import (
	"strings"
	"testing"
)

// ctrl+a esc switches the bar to view mode; neither key of the sequence
// reaches a host, and the status bar says which mode is active.
func TestBroadcastBarCtrlAEscEntersViewMode(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "ctrl+a")
	if len(sender.sent) != 0 {
		t.Fatalf("the prefix was forwarded: %q", sender.sent)
	}
	a = pressKey(t, a, "esc")
	if !a.broadcastView {
		t.Fatal("ctrl+a esc did not enter view mode")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the sequence was forwarded: %q", sender.sent)
	}
	if view := plain(a.View().Content); !strings.Contains(view, "BROADCAST VIEW") {
		t.Fatalf("the status bar does not announce view mode:\n%s", view)
	}
}

// The pending prefix is visible: a user who typed ctrl+a is told what the
// second key may be, in the status bar and in the bar itself.
func TestBroadcastBarPendingPrefixIsIndicated(t *testing.T) {
	a, _ := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	if !a.broadcastPending {
		t.Fatal("ctrl+a did not arm the prefix")
	}
	if view := plain(a.View().Content); !strings.Contains(view, "ctrl+a…") {
		t.Fatalf("the pending prefix is not indicated:\n%s", view)
	}
}

// ctrl+a a sends exactly one literal ctrl+a, which is how a remote screen or
// tmux behind the broadcast stays reachable.
func TestBroadcastBarCtrlAASendsTheLiteral(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "a")
	if got := strings.Join(sender.sent, ","); got != "\x01" {
		t.Fatalf("sent = %q, want exactly one literal ctrl+a", got)
	}
	if a.broadcastPending || a.broadcastView {
		t.Fatal("the literal did not return the bar to plain edit mode")
	}
	if a.BroadcastLine() != "" {
		t.Fatalf("the literal leaked into the echo line: %q", a.BroadcastLine())
	}
}

// An unknown key after ctrl+a cancels the prefix and says so; neither key is
// sent, and the bar is back to normal for the next keystroke.
func TestBroadcastBarUnknownSequenceCancelsAndSaysSo(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "x")
	if len(sender.sent) != 0 {
		t.Fatalf("a cancelled sequence was forwarded: %q", sender.sent)
	}
	if !strings.Contains(a.LastDelivery(), "nothing was sent") {
		t.Fatalf("LastDelivery() = %q, want a note that nothing was sent", a.LastDelivery())
	}

	a = pressKey(t, a, "l")
	if got := strings.Join(sender.sent, ","); got != "<l>" {
		t.Fatalf("sent = %q, want edit mode to resume after the cancel", got)
	}
}

// View mode routes keys to app-level commands and never to the hosts: ? opens
// the help instead of reaching a host.
func TestBroadcastBarViewModeRoutesCommands(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "esc")
	a = pressKey(t, a, "?")
	if !a.HelpVisible() {
		t.Fatal("? in view mode did not reach the help command")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("view mode forwarded keys: %q", sender.sent)
	}
	if a.Focus() != AreaBroadcast || !a.broadcastView {
		t.Fatal("a view-mode command left the bar or the mode")
	}
}

// enter is the way back: it returns to edit mode without reaching a host, and
// the next keystroke is a keystroke again.
func TestBroadcastBarEnterReturnsToEditMode(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "esc")
	a = pressKey(t, a, "enter")
	if a.broadcastView {
		t.Fatal("enter did not leave view mode")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the mode switch was forwarded: %q", sender.sent)
	}

	a = pressKey(t, a, "l")
	if got := strings.Join(sender.sent, ","); got != "<l>" {
		t.Fatalf("sent = %q, want typing to resume", got)
	}
}

// The modal state does not outlive the bar's focus: leaving in view mode and
// coming back lands in edit mode, whatever the route back in.
func TestBroadcastBarReentryStartsInEditMode(t *testing.T) {
	a, _ := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "esc")
	a = pressKey(t, a, "ctrl+]")
	if a.Focus() != AreaBroadcast && !a.broadcastView {
		// Leaving already cleared the mode; re-entry must find it cleared too.
		a = pressKey(t, a, "5")
	}
	if a.Focus() != AreaBroadcast {
		t.Fatalf("Focus() = %v, want the bar again", a.Focus())
	}
	if a.broadcastView || a.broadcastPending {
		t.Fatal("the bar was re-entered in a stale mode")
	}
}

// 5 from view mode is the second way back to edit mode: selecting the bar is
// entering it fresh.
func TestBroadcastBarPanelKeyResetsViewMode(t *testing.T) {
	a, _ := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "esc")
	a = pressKey(t, a, "5")
	if a.Focus() != AreaBroadcast {
		t.Fatalf("Focus() = %v after 5", a.Focus())
	}
	if a.broadcastView {
		t.Fatal("5 did not return the bar to edit mode")
	}
}

// Issue #148: ctrl+a is a general "the next key is for lazycssh" prefix. An
// app chord after it runs the app command - ctrl+a ? opens the help - and
// sends nothing to the hosts.
func TestBroadcastBarPrefixDispatchesAppCommands(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "?")
	if !a.HelpVisible() {
		t.Fatal("ctrl+a ? did not reach the help command")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the prefixed command was forwarded: %q", sender.sent)
	}
	if a.broadcastPending {
		t.Fatal("the prefix survived its second key")
	}
}

// The prefix is one-shot: after ctrl+a ctrl+a the next key is a keystroke for
// the hosts again, not a chained prefix resolution.
func TestBroadcastBarPrefixDoesNotChain(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "a")
	if got := strings.Join(sender.sent, ","); got != "<a>" {
		t.Fatalf("sent = %q, want the plain letter after the one-shot prefix", got)
	}
}

// ctrl+a ? opens the help overlay from inside the bar.
func TestBroadcastBarPrefixOpensHelp(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "?")
	if !a.showHelp {
		t.Fatal("ctrl+a ? did not open the help overlay")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the help chord was forwarded: %q", sender.sent)
	}
}
