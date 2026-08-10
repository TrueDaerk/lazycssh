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

// The ctrl+a chord (issue #273).
//
// Paging between screenfuls is ctrl+shift+arrows, and in several common
// environments that chord never arrives: macOS Terminal.app does not transmit
// it, Mission Control and IDE keymaps eat it elsewhere. The chord is the
// portable way in — ctrl+a, then an arrow — and it works wherever focus is: in
// a focused pane, in the broadcast bar and at the app level.
//
// It follows GNU screen rather than inventing a dialect:
//
//   - ctrl+a arms the prefix; the next key press is a command.
//   - → / ← page, exactly like ctrl+shift+right / ctrl+shift+left.
//   - ctrl+a ctrl+a and ctrl+a a send one literal ctrl+a, because ctrl+a is
//     readline's beginning-of-line and typing must not lose it.
//   - esc cancels.
//   - anything else cancels the prefix and is then handled as if it had been
//     pressed on its own: a swallowed keystroke is worse than an unhandled one.
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
		a.lastDelivery = id + " is not connected — alt+r reconnects, " + escapeKeystroke + " leaves"
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
	return a.handleAppKey(msg)
}

// prefixLabel is the status-bar indicator of an armed prefix, empty while none
// is. It names what the next key may do, because a user who armed a chord by
// accident needs to read the way out.
func (a App) prefixLabel() string {
	if !a.prefixArmed {
		return ""
	}
	return prefixKeystroke + "… — ←/→ page · " +
		prefixKeystroke + "/a = literal " + prefixKeystroke + " · esc cancels"
}
