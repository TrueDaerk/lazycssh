package ui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

// escapeKeystroke leaves a terminal input - the focused pane, the broadcast
// bar. It is the telnet escape, for the same reason telnet chose it: it is not
// a key any shell wants, and a user who is stuck needs one sequence that
// always means "give me my keyboard back".
//
// It is the default of [KeyMap.LeaveTyping]; the hints read the effective
// binding through [App.escapeKey] rather than this constant, so a keymap file
// that moves it moves what the status bar promises with it (issue #289).
const escapeKeystroke = "ctrl+]"

// escapeKey names the way out of a terminal input as it is actually bound, for
// the status bar and the delivery notices that promise it.
func (a App) escapeKey() string { return firstKey(a.keys.LeaveTyping, escapeKeystroke) }

// reconnectKey names the per-pane reconnect chord as it is actually bound, for
// the notice a dead pane shows when a keystroke had nowhere to go.
func (a App) reconnectKey() string { return firstKey(a.keys.Reconnect, "alt+r") }

// textMods are the modifiers that only transform which text a key produces;
// a key carrying nothing beyond these is plain typed input, not a chord.
const textMods = tea.ModShift | tea.ModCapsLock | tea.ModNumLock

// motionKey translates the macOS-conventional editing chords into the
// readline/ZLE emacs-mode defaults — the "natural text editing" convention
// (issue #202, #206): option+arrows jump words (ESC b / ESC f), cmd+arrows go
// to line start/end (ctrl+a / ctrl+e), option+backspace kills the previous
// word (ESC DEL), option+forward-delete kills the next word (ESC d),
// cmd+backspace kills to line start (ctrl+u). Shift-augmented variants behave
// the same; a PTY has no selection to extend.
//
// ctrl+arrows are word movement too (issue #208): they are the Linux/Windows
// readline convention, and since paging moved to ctrl+shift+arrows nothing in
// lazycssh claims them. The vt emulator's encoder drops ctrl-modified arrows,
// so they are translated to the ESC b / ESC f readline defaults here — same
// destination the xterm CSI 1;5D encoding reaches through an inputrc.
func motionKey(k tea.KeyPressMsg) (term.KeyEvent, bool) {
	mod := k.Mod &^ textMods
	isCmd := mod == tea.ModSuper || mod == tea.ModMeta
	switch {
	case mod == tea.ModCtrl && k.Code == tea.KeyLeft:
		return term.KeyEvent{Code: 'b', Mod: term.ModAlt}, true
	case mod == tea.ModCtrl && k.Code == tea.KeyRight:
		return term.KeyEvent{Code: 'f', Mod: term.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyLeft:
		return term.KeyEvent{Code: 'b', Mod: term.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyRight:
		return term.KeyEvent{Code: 'f', Mod: term.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyBackspace:
		return term.KeyEvent{Code: term.KeyBackspace, Mod: term.ModAlt}, true
	case mod == tea.ModAlt && k.Code == tea.KeyDelete:
		return term.KeyEvent{Code: 'd', Mod: term.ModAlt}, true
	case isCmd && k.Code == tea.KeyLeft:
		return term.KeyEvent{Code: 'a', Mod: term.ModCtrl}, true
	case isCmd && k.Code == tea.KeyRight:
		return term.KeyEvent{Code: 'e', Mod: term.ModCtrl}, true
	case isCmd && k.Code == tea.KeyBackspace:
		return term.KeyEvent{Code: 'u', Mod: term.ModCtrl}, true
	}
	return term.KeyEvent{}, false
}

// paneKeyEvents converts a bubbletea key press into the emulator key events a
// pane forwards; the two structs share the same shape (code, shifted code,
// modifiers, text). Two adjustments:
//
//   - the macOS editing chords are pre-translated by [motionKey],
//   - the emulator's encoder writes a plain key only when no modifier is set,
//     so shifted or caps-locked characters (shift+a → "A") would be dropped;
//     such presses are replayed as their produced text instead.
func paneKeyEvents(k tea.KeyPressMsg) []term.KeyEvent {
	if ev, ok := motionKey(k); ok {
		return []term.KeyEvent{ev}
	}
	if k.Text != "" && k.Mod != 0 && k.Mod&^textMods == 0 {
		evs := make([]term.KeyEvent, 0, 1)
		for _, r := range k.Text {
			evs = append(evs, term.KeyEvent{Code: r, Text: string(r)})
		}
		return evs
	}
	return []term.KeyEvent{{
		Code:        k.Code,
		ShiftedCode: k.ShiftedCode,
		Mod:         term.KeyMod(k.Mod),
		Text:        k.Text,
	}}
}
