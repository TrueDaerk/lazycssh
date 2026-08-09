package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
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

// Issue #214: the prefix forwards by default. A key that is neither the
// literal nor esc reaches the targets as the keystroke it is, runs no lazycssh
// command, and stays out of the assembled line.
func TestBroadcastBarPrefixForwardsOtherKeys(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "c")
	if got := strings.Join(sender.sent, ","); got != "<c>" {
		t.Fatalf("sent = %q, want the prefixed key forwarded", got)
	}
	if a.broadcastPending || a.broadcastView {
		t.Fatal("the forward did not return the bar to plain edit mode")
	}
	if a.BroadcastLine() != "" {
		t.Fatalf("a prefixed key entered the assembled line: %q", a.BroadcastLine())
	}

	a = pressKey(t, a, "l")
	if got := strings.Join(sender.sent, ","); got != "<c>,<l>" {
		t.Fatalf("sent = %q, want edit mode to resume after the prefix", got)
	}
}

// A prefixed app chord is a keystroke too: ctrl+a ? forwards the question mark
// instead of opening the help, because the prefix no longer dispatches
// commands (superseding issue #148).
func TestBroadcastBarPrefixDoesNotRunAppCommands(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "?")
	if a.HelpVisible() {
		t.Fatal("ctrl+a ? still ran the help command")
	}
	if got := strings.Join(sender.sent, ","); got != "<?>" {
		t.Fatalf("sent = %q, want the chord forwarded to the hosts", got)
	}
	if a.broadcastPending {
		t.Fatal("the prefix survived its second key")
	}
}

// Non-text keys forward through the same encoding as plain typing.
func TestBroadcastBarPrefixForwardsArrowKeys(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	press(t, a, tea.KeyPressMsg{Code: tea.KeyRight})
	if got := strings.Join(sender.sent, ","); got != "<right>" {
		t.Fatalf("sent = %q, want the arrow forwarded", got)
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

// ctrl+a ctrl+a is the GNU-screen double press: exactly one literal ctrl+a
// reaches the targets, which is how ctrl+a ctrl+a c opens a screen window on
// every host (issue #214).
func TestBroadcastBarCtrlACtrlASendsTheLiteral(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "ctrl+a")
	if got := strings.Join(sender.sent, ","); got != "\x01" {
		t.Fatalf("sent = %q, want exactly one literal ctrl+a", got)
	}
	if a.broadcastPending || a.broadcastView {
		t.Fatal("the double press did not return the bar to plain edit mode")
	}
	if a.BroadcastLine() != "" {
		t.Fatalf("the literal leaked into the echo line: %q", a.BroadcastLine())
	}
}

// The prefix is one-shot: after ctrl+a ctrl+a the next key is a keystroke for
// the hosts again, not a chained prefix resolution.
func TestBroadcastBarPrefixDoesNotChain(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "a")
	if got := strings.Join(sender.sent, ","); got != "\x01,<a>" {
		t.Fatalf("sent = %q, want the plain letter after the one-shot prefix", got)
	}
}

// esc is still the way to the app commands from inside the bar: view mode
// opens the help overlay and forwards nothing.
func TestBroadcastBarViewModeOpensHelp(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "esc")
	a = pressKey(t, a, "?")
	if !a.showHelp {
		t.Fatal("? in view mode did not open the help overlay")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the help chord was forwarded: %q", sender.sent)
	}
}
