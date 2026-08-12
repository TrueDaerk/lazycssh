package ui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

// chunkedApp is ten hosts split into two chunks of five, which is the smallest
// fixture where a screenful step is visible in the host list.
func chunkedApp(t *testing.T) App {
	t.Helper()
	a, _, _ := splitApp(t)
	return applySplitSize(t, a, "5")
}

// ctrl+a → pages exactly like ctrl+shift+→ while a pane has the keyboard: the
// chord is the portable way in for the terminals that never send the shifted
// arrows (issue #273).
func TestPrefixPagesWhileTyping(t *testing.T) {
	a := focusGrid(t, chunkedApp(t))

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "right")
	if got := a.hostIDs()[0]; got != "web-06" {
		t.Fatalf("visible hosts start at %q after ctrl+a →, want the next screenful", got)
	}
	if a.Focus() != AreaGrid {
		t.Fatal("paging with the chord left the pane's terminal")
	}

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "left")
	if got := a.hostIDs()[0]; got != "web-01" {
		t.Fatalf("visible hosts start at %q after ctrl+a ←, want the previous screenful", got)
	}
}

// The same chord in the broadcast bar: it pages, and neither key reaches a
// host — an arrow after the prefix is a command, not a keystroke.
func TestPrefixPagesInTheBroadcastBar(t *testing.T) {
	a := chunkedApp(t)
	a = pressKey(t, a, "5")
	if a.Focus() != AreaBroadcast {
		t.Fatal("setup: the broadcast bar does not have focus")
	}

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "right")
	if got := a.hostIDs()[0]; got != "web-06" {
		t.Fatalf("visible hosts start at %q after ctrl+a → in the bar", got)
	}
	if a.prefixArmed || a.broadcastView {
		t.Fatal("paging did not return the bar to plain edit mode")
	}

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "left")
	if got := a.hostIDs()[0]; got != "web-01" {
		t.Fatalf("visible hosts start at %q after ctrl+a ← in the bar", got)
	}
}

// And at the app level, where the sidebar's own left/right would otherwise
// switch panels: an armed prefix is resolved before them.
func TestPrefixPagesAtTheAppLevel(t *testing.T) {
	a := chunkedApp(t)
	if a.Focus() != AreaSidebar {
		t.Fatal("setup: the sidebar does not have focus")
	}
	panel := a.panel

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "right")
	if got := a.hostIDs()[0]; got != "web-06" {
		t.Fatalf("visible hosts start at %q after ctrl+a → at the app level", got)
	}
	if a.panel != panel {
		t.Fatal("ctrl+a → switched the sidebar panel instead of paging")
	}

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "left")
	if got := a.hostIDs()[0]; got != "web-01" {
		t.Fatalf("visible hosts start at %q after ctrl+a ← at the app level", got)
	}
}

// ctrl+a is readline's beginning-of-line, so the prefix must hand it back:
// ctrl+a a and ctrl+a ctrl+a each deliver exactly one literal to the host.
func TestPrefixLiteralReachesTheFocusedHost(t *testing.T) {
	for _, second := range []string{"a", "ctrl+a"} {
		t.Run(second, func(t *testing.T) {
			a, fleet := typingApp(t, "web-01")

			a = pressKey(t, a, "ctrl+a")
			if got := fleet.sessions["web-01"].Written(); got != "" {
				t.Fatalf("the prefix itself reached the host: %q", got)
			}
			a = pressKey(t, a, second)
			if got := fleet.sessions["web-01"].Written(); got != "\x01" {
				t.Fatalf("the host received %q, want one literal ctrl+a", got)
			}
			if a.prefixArmed {
				t.Fatal("the prefix survived the literal")
			}
		})
	}
}

// The same for the broadcast targets, through the sender the bar writes to.
func TestPrefixLiteralReachesTheBroadcastTargets(t *testing.T) {
	for _, second := range []string{"a", "ctrl+a"} {
		t.Run(second, func(t *testing.T) {
			a, sender := barApp(t, "web-01", "web-02")

			a = pressKey(t, a, "ctrl+a")
			a = pressKey(t, a, second)
			if got := strings.Join(sender.sent, ","); got != "\x01" {
				t.Fatalf("sent = %q, want exactly one literal ctrl+a", got)
			}
		})
	}
}

// An unrelated key cancels the prefix and is then handled as though it had
// been pressed alone: while typing it reaches the host, unswallowed.
func TestPrefixCancelsOnAnUnrelatedKeyWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "x")
	if got := fleet.sessions["web-01"].Written(); got != "x" {
		t.Fatalf("the host received %q, want the cancelled key forwarded", got)
	}
	if a.prefixArmed {
		t.Fatal("the prefix survived an unrelated key")
	}
}

// The same at the app level: the key runs the command it always ran.
func TestPrefixCancelsOnAnUnrelatedKeyAtTheAppLevel(t *testing.T) {
	a := chunkedApp(t)

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "?")
	if !a.HelpVisible() {
		t.Fatal("ctrl+a ? did not fall through to the help command")
	}
	if a.prefixArmed {
		t.Fatal("the prefix survived an unrelated key")
	}
}

// The chord reaches the commands that hide behind a ctrl chord, at the app
// level as in the bar: ctrl+a r re-tiles although Retile is ctrl+r, because
// after the prefix a plain letter stands for its ctrl chord (issue #289).
func TestPrefixRunsACtrlCommand(t *testing.T) {
	a := chunkedApp(t)
	a.keptSlots = 99

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
}

// The non-negotiable invariant of issue #289: whatever a keymap file moves,
// ctrl+a stays the command prefix and ctrl+a ctrl+a stays the literal - a
// remote screen, tmux or readline must not be rebindable out of reach.
func TestPrefixAndItsLiteralSurviveARemappedKeyMap(t *testing.T) {
	keys, err := ParseKeyMap([]byte("SelectAll: z\nLeaveTyping: ctrl+g\nPrefixCancel: ctrl+e\n"), "keys.yaml")
	if err != nil {
		t.Fatalf("ParseKeyMap() = %v", err)
	}

	a, sender, router, _ := cmdApp(t, "web-01", "web-02")
	a.keys = &keys
	a = pressKey(t, a, "5")
	if a.Focus() != AreaBroadcast {
		t.Fatal("setup: the broadcast bar does not have focus")
	}

	a = pressKey(t, a, "ctrl+a")
	if !a.prefixArmed {
		t.Fatal("ctrl+a stopped arming the prefix under a user keymap")
	}
	a = pressKey(t, a, "ctrl+a")
	if got := strings.Join(sender.sent, ","); got != "\x01" {
		t.Fatalf("sent = %q, want the literal ctrl+a", got)
	}

	// The remapped chord keys follow the file: ctrl+e cancels into view mode,
	// and the remapped command runs after the prefix.
	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "ctrl+e")
	if !a.broadcastView {
		t.Fatal("the remapped chord cancel did not enter view mode")
	}
	a = pressKey(t, a, "enter")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "z")
	if got := router.SelectionCount(); got != 2 {
		t.Fatalf("SelectionCount() = %d, want the remapped select-all to have run", got)
	}
	if got := strings.Join(sender.sent, ","); got != "\x01" {
		t.Fatalf("sent = %q, want the commands to have reached no host", got)
	}
}

// esc cancels the armed prefix outside the bar without reaching the host: a
// user who armed a chord by accident has a way out that sends nothing.
func TestPrefixEscCancelsWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "esc")
	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("the host received %q, want nothing from a cancelled chord", got)
	}
	if a.prefixArmed {
		t.Fatal("esc did not cancel the prefix")
	}
}

// An armed prefix swallows the next key press, so it is on the status bar for
// exactly as long as it lasts — in a pane and at the app level alike.
func TestPrefixIsIndicatedInTheStatusBar(t *testing.T) {
	for _, tc := range []struct {
		name  string
		focus func(t *testing.T, a App) App
	}{
		{"typing", func(t *testing.T, a App) App { return focusGrid(t, a) }},
		{"app level", func(t *testing.T, a App) App { return a }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := tc.focus(t, chunkedApp(t))

			if view := plain(a.View().Content); strings.Contains(view, "ctrl+a…") {
				t.Fatalf("the prefix is announced before it was armed:\n%s", view)
			}
			a = pressKey(t, a, "ctrl+a")
			if view := plain(a.View().Content); !strings.Contains(view, "ctrl+a…") {
				t.Fatalf("the armed prefix is not on the status bar:\n%s", view)
			}
			a = pressKey(t, a, "right")
			if view := plain(a.View().Content); strings.Contains(view, "ctrl+a…") {
				t.Fatalf("the indicator outlived the resolved chord:\n%s", view)
			}
		})
	}
}

// The chord is documented where every binding is: the `?` overlay, generated
// from the keymap, so the help cannot drift from what the handlers match.
func TestHelpOverlayDocumentsTheChord(t *testing.T) {
	k := DefaultKeyMap()
	model := help.New()
	th := NewTheme(Options{Dark: true})
	model.Styles = HelpStyles(&th)
	model.SetWidth(400)

	rendered := model.FullHelpView(k.For(AreaChord).FullHelp())
	for _, b := range k.Bindings(AreaChord) {
		if !strings.Contains(rendered, b.Help().Desc) {
			t.Fatalf("the overlay does not mention %q:\n%s", b.Help().Desc, rendered)
		}
	}
	if !strings.Contains(rendered, k.PrefixNext.Help().Key) {
		t.Fatalf("the overlay does not name the paging chord:\n%s", rendered)
	}
}

// The prefix is not bar state: it survives the routing that resets the bar's
// modes, so a chord armed in a pane is still armed on the next key press.
func TestPrefixSurvivesOutsideTheBroadcastBar(t *testing.T) {
	a, _ := typingApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	if !a.prefixArmed {
		t.Fatal("ctrl+a did not arm the prefix while typing")
	}
}

// The literal has nowhere to go when no terminal has the keyboard, and says
// so rather than pretending it sent something.
func TestPrefixLiteralAtTheAppLevelReportsNoTarget(t *testing.T) {
	a := chunkedApp(t)

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "a")
	if !strings.Contains(a.lastDelivery, "no host has the keyboard") {
		t.Fatalf("lastDelivery = %q, want the literal to report where it goes", a.lastDelivery)
	}
}

// A pane with no transport behind it reports the dead session rather than
// swallowing the literal.
func TestPrefixLiteralOnADeadPaneSaysSo(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	fleet.sessions["web-01"].Disconnect(errors.New("connection reset"))

	a = pressKey(t, a, "ctrl+a")
	a = pressKey(t, a, "a")
	if !strings.Contains(a.lastDelivery, "web-01") {
		t.Fatalf("lastDelivery = %q, want the dead pane named", a.lastDelivery)
	}
}

// keyMsgFor synthesises the chord's own key the way a terminal sends it, so
// the tests above press what a user presses.
func TestPrefixKeyIsSynthesisedAsCtrlA(t *testing.T) {
	msg := keyMsgFor(t, "ctrl+a")
	if msg.Code != 'a' || msg.Mod != tea.ModCtrl {
		t.Fatalf("keyMsgFor(ctrl+a) = %+v", msg)
	}
}
