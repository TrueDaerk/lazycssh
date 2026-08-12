package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

// literalPrefixByte is what ctrl+a ctrl+a and ctrl+a a put on the wire: the
// control character the prefix took away. readline reads it as
// beginning-of-line, a remote screen or tmux as its own escape.
const literalPrefixByte = 0x01

// prefixKeystroke names the chord key in the status bar, so the indicator and
// the binding cannot drift apart.
var prefixKeystroke = DefaultKeyMap().Prefix.Help().Key

// The ctrl+a chord (issues #273, #289).
//
// Paging between screenfuls is ctrl+shift+arrows, and in several common
// environments that chord never arrives: macOS Terminal.app does not transmit
// it, Mission Control and IDE keymaps eat it elsewhere. The chord is the
// portable way in — ctrl+a, then an arrow — and it works wherever focus is: in
// a focused pane, in the broadcast bar and at the app level.
//
// It follows GNU screen rather than inventing a dialect:
//
//   - ctrl+a arms the prefix; the next key press is a lazycssh command.
//   - → / ← page, exactly like ctrl+shift+right / ctrl+shift+left.
//   - ctrl+a ctrl+a and ctrl+a a send one literal ctrl+a, because ctrl+a is
//     readline's beginning-of-line and typing must not lose it.
//   - esc cancels — inside the broadcast bar it switches to view mode.
//   - every app-level command is reachable through it, including from inside
//     the broadcast bar, where the keys otherwise belong to the hosts (issue
//     #289). ctrl+a r re-tiles because Retile is ctrl+r: after the prefix a
//     plain letter stands for its ctrl chord, screen's own ctrl+a c ≡
//     ctrl+a ctrl+c rule, so the command set does not need a second keymap.
//   - anything else cancels the prefix and is then handled as if it had been
//     pressed on its own: a swallowed keystroke is worse than an unhandled one.
//
// The prefix and its literal are the two bindings a keymap file may not move
// (see keysconfig.go): they are how a user leaves a mode and how a remote
// screen, tmux or readline stays reachable, not a preference. Everything else
// the chord dispatches follows the effective keymap, so ctrl+a r follows Retile
// wherever it was rebound to.
//
// The armed state is one bool on the model, mutated only in Update, and it is
// announced in the status bar for as long as it lasts: the user must know that
// the next key is a command rather than input.

// armPrefix arms the chord. It is the same in every context, so the callers
// all go through here rather than each setting the flag.
func (a App) armPrefix() App {
	a.prefixArmed = true
	return a
}

// resolvePrefixPaging handles the part of the chord that means the same thing
// everywhere: the arrows page between screenfuls, down the same [App.stepView]
// path ctrl+shift+arrows take. It reports handled=false for every other key,
// which each context resolves for itself - the literal, the cancel and the
// passthrough differ between a pane, the bar and the app level.
//
// The caller clears the armed flag before calling this, so a chord cannot
// chain into another one.
func (a App) resolvePrefixPaging(msg tea.KeyPressMsg) (App, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, a.keys.PrefixNext):
		next, cmd := a.stepView(+1)
		return next, cmd, true
	case key.Matches(msg, a.keys.PrefixPrev):
		next, cmd := a.stepView(-1)
		return next, cmd, true
	}
	return a, nil, false
}

// resolveTypingPrefix is the key after ctrl+a while a pane has the keyboard.
// Paging and the literal are the chord's own; esc cancels; everything else is
// handed to the pane as though the prefix had never been pressed, so ctrl+a
// followed by a stray key costs the user nothing but the prefix.
func (a App) resolveTypingPrefix(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a.prefixArmed = false
	if next, cmd, handled := a.resolvePrefixPaging(msg); handled {
		return next, cmd
	}
	switch {
	case key.Matches(msg, a.keys.PrefixLiteral):
		return a.sendPaneLiteral()
	case key.Matches(msg, a.keys.PrefixCancel):
		return a, nil
	}
	return a.handleTypingKey(msg)
}

// sendPaneLiteral delivers one literal ctrl+a to the focused host - the
// keystroke the prefix took away.
func (a App) sendPaneLiteral() (tea.Model, tea.Cmd) {
	id := a.FocusedHost()
	if id == "" {
		return a, nil
	}
	if a.cfg.Panes == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}
	if !a.cfg.Panes.SendKey(id, term.KeyEvent{Code: 'a', Mod: term.ModCtrl}) {
		a.lastDelivery = id + " is not connected — " + a.reconnectKey() + " reconnects, " + a.escapeKey() + " leaves"
	}
	return a, nil
}

// resolveAppPrefix is the key after ctrl+a while no terminal-like input owns
// the keyboard. Paging works exactly as it does while typing; the literal has
// nowhere to go from here and says so rather than silently doing nothing;
// everything else is dispatched as the app-level command it is.
func (a App) resolveAppPrefix(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a.prefixArmed = false
	if next, cmd, handled := a.resolvePrefixPaging(msg); handled {
		return next, cmd
	}
	switch {
	case key.Matches(msg, a.keys.PrefixLiteral):
		a.lastDelivery = "no host has the keyboard: the literal ctrl+a goes to a pane or the broadcast bar"
		return a, nil
	case key.Matches(msg, a.keys.PrefixCancel):
		return a, nil
	}
	return a.handleAppKey(a.chordCommandKey(msg))
}

// chordCommandKey is the key an armed prefix hands to the command dispatch.
//
// A key that is a command already is passed through. A plain character that is
// not stands for its ctrl chord, which is how ctrl+a r reaches Retile's ctrl+r
// without a second keymap - GNU screen's ctrl+a c ≡ ctrl+a ctrl+c, and the
// reason the commands that hide behind ctrl chords (because a host would
// otherwise eat them) are reachable from inside a terminal-like input at all.
// A key that is neither is returned unchanged, for the caller to resolve.
func (a App) chordCommandKey(msg tea.KeyPressMsg) tea.KeyPressMsg {
	if a.keys.isCommand(msg) {
		return msg
	}
	if chord, ok := withCtrl(msg); ok && a.keys.isCommand(chord) {
		return chord
	}
	return msg
}

// isCommand reports whether a key press runs an app-level command: the global
// bindings are exactly the ones that are live wherever focus is, which makes
// them the chord's command set.
func (k KeyMap) isCommand(msg tea.KeyPressMsg) bool {
	return key.Matches(msg, k.global()...)
}

// withCtrl is the same key press with ctrl held. Only a plain character has
// one: a chord, an arrow or a named key either carries a modifier already or
// has no ctrl form a terminal would report.
func withCtrl(msg tea.KeyPressMsg) (tea.KeyPressMsg, bool) {
	if msg.Mod != 0 || msg.Text == "" {
		return msg, false
	}
	msg.Mod = tea.ModCtrl
	// ctrl+r carries no text; leaving it would make the key stringify as the
	// letter again and match nothing.
	msg.Text = ""
	return msg, true
}

// prefixLabel is the status-bar indicator of an armed prefix, empty while none
// is. It names what the next key may do, because a user who armed a chord by
// accident needs to read the way out. Every key in it comes from the binding
// that handles it, so a remapped chord key cannot leave a lie on the bar.
func (a App) prefixLabel() string {
	if !a.prefixArmed {
		return ""
	}
	prefix := a.prefixKey()
	return prefix + "… — " + a.pagingLabel() + " page · " +
		prefix + "/a = literal " + prefix + " · " +
		firstKey(a.keys.PrefixCancel, "esc") + " cancels"
}

// firstKey is the key a binding is written with in a hint: its first, or the
// fallback when something disabled the binding entirely.
func firstKey(b key.Binding, fallback string) string {
	if keys := b.Keys(); len(keys) > 0 {
		return keys[0]
	}
	return fallback
}

// prefixKey names the chord prefix as the user presses it. It comes from the
// binding rather than a constant, so the indicator and the binding cannot drift
// apart - the property that survives the keymap file, since the prefix is one
// of the two bindings it may not move.
func (a App) prefixKey() string {
	if label := a.keys.Prefix.Help().Key; label != "" {
		return label
	}
	return prefixKeystroke
}

// pagingLabel names the two keys that page after the prefix, "←/→" by default.
func (a App) pagingLabel() string {
	return keyArrow(firstKey(a.keys.PrefixPrev, "left")) + "/" +
		keyArrow(firstKey(a.keys.PrefixNext, "right"))
}

// keyArrow renders the arrow keys as arrows, the way the shipped help labels
// write them, and everything else as the name it is bound to.
func keyArrow(pressed string) string {
	switch pressed {
	case "left":
		return "←"
	case "right":
		return "→"
	case "up":
		return "↑"
	case "down":
		return "↓"
	}
	return pressed
}
