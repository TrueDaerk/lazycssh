package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
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
	if !a.prefixArmed {
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
	if a.prefixArmed || a.broadcastView {
		t.Fatal("the literal did not return the bar to plain edit mode")
	}
	if a.BroadcastLine() != "" {
		t.Fatalf("the literal leaked into the echo line: %q", a.BroadcastLine())
	}
}

// Issue #214: a key the command set does not claim still forwards. It reaches
// the targets as the keystroke it is and stays out of the assembled line, so a
// stray chord costs the user the prefix and nothing else.
func TestBroadcastBarPrefixForwardsOtherKeys(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "x")
	if got := strings.Join(sender.sent, ","); got != "<x>" {
		t.Fatalf("sent = %q, want the prefixed key forwarded", got)
	}
	if a.prefixArmed || a.broadcastView {
		t.Fatal("the forward did not return the bar to plain edit mode")
	}
	if a.BroadcastLine() != "" {
		t.Fatalf("a prefixed key entered the assembled line: %q", a.BroadcastLine())
	}

	a = pressKey(t, a, "l")
	if got := strings.Join(sender.sent, ","); got != "<x>,<l>" {
		t.Fatalf("sent = %q, want edit mode to resume after the prefix", got)
	}
}

// Issue #289: in the bar the prefix is the command prefix. ctrl+a ? opens the
// help instead of reaching a host, and nothing is sent.
func TestBroadcastBarPrefixRunsAppCommands(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "?")
	if !a.HelpVisible() {
		t.Fatal("ctrl+a ? did not run the help command")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the command was forwarded to the hosts: %q", sender.sent)
	}
	if a.prefixArmed {
		t.Fatal("the prefix survived its second key")
	}
}

// The acceptance criterion of issue #289: ctrl+a r re-tiles, although Retile is
// bound to ctrl+r. After the prefix a plain letter stands for its ctrl chord,
// which is how the commands a host would otherwise eat are reachable from
// inside the broadcast line.
func TestBroadcastBarPrefixRetiles(t *testing.T) {
	a, sender := barApp(t, "web-01", "web-02")
	a = resize(t, a, 120, 40)
	// A kept shape wider than the fleet is what re-tiling drops.
	a.keptSlots = 12

	a = pressKey(t, a, "ctrl+a")
	model, cmd := a.Update(keyMsgFor(t, "r"))
	a = model.(App)

	if got, want := a.keptSlots, len(a.hostIDs()); got != want {
		t.Fatalf("keptSlots = %d, want ctrl+a r to have re-tiled to %d", got, want)
	}
	if cmd == nil {
		t.Fatal("ctrl+a r produced no command; the PTYs would keep the old size")
	}
	if _, ok := cmd().(GridChangedMsg); !ok {
		t.Fatalf("ctrl+a r produced a %T, want the re-tile", cmd())
	}
	if len(sender.sent) != 0 {
		t.Fatalf("the command reached the hosts: %q", sender.sent)
	}
	if a.Focus() != AreaBroadcast || a.broadcastView || a.prefixArmed {
		t.Fatal("running a chord command left the bar or its mode")
	}
}

// The chord follows the keymap, not a table of its own: a rebound Retile moves
// which letter re-tiles after the prefix with it.
func TestBroadcastBarPrefixCommandFollowsARebinding(t *testing.T) {
	a, _ := barApp(t, "web-01", "web-02")
	a = resize(t, a, 120, 40)
	a.keys.Retile = key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("ctrl+t", "re-tile"))
	a.keptSlots = 12

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "r")
	if a.keptSlots != 12 {
		t.Fatal("ctrl+a r still re-tiled although Retile moved to ctrl+t")
	}

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "t")
	if got, want := a.keptSlots, len(a.hostIDs()); got != want {
		t.Fatalf("keptSlots = %d, want the rebound chord to re-tile to %d", got, want)
	}
}

// Non-text keys the command set does not claim forward through the same
// encoding as plain typing. The arrows are the exception since issue #273 -
// they page - so backspace stands in for the rest of the passthrough.
func TestBroadcastBarPrefixForwardsNonTextKeys(t *testing.T) {
	a, sender := barApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	press(t, a, tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := strings.Join(sender.sent, ","); got != "<backspace>" {
		t.Fatalf("sent = %q, want the key forwarded", got)
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
	if a.broadcastView || a.prefixArmed {
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
	if a.prefixArmed || a.broadcastView {
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
