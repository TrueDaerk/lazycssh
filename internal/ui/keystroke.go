package ui

import (
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// escapeKeystroke leaves a terminal input - the focused pane, the broadcast
// bar. It is the telnet escape, for the same reason telnet chose it: it is not
// a key any shell wants, and a user who is stuck needs one sequence that
// always means "give me my keyboard back".
const escapeKeystroke = "ctrl+]"

// keystrokeBytes encodes a key press the way a terminal would send it to a PTY.
//
// It is deliberately explicit rather than clever: what reaches a remote shell is
// the one thing in this program a user cannot inspect, so every mapping is
// written down and tested.
func keystrokeBytes(msg tea.KeyPressMsg) []byte {
	switch msg.String() {
	case "enter":
		// A terminal sends carriage return; the remote line discipline turns it
		// into a newline.
		return []byte{'\r'}
	case "tab":
		return []byte{'\t'}
	case "shift+tab":
		return []byte("\x1b[Z")
	case "backspace":
		return []byte{0x7f}
	case "delete":
		return []byte("\x1b[3~")
	case "esc":
		return []byte{0x1b}
	case "space":
		return []byte{' '}
	case "up":
		return []byte("\x1b[A")
	case "down":
		return []byte("\x1b[B")
	case "right":
		return []byte("\x1b[C")
	case "left":
		return []byte("\x1b[D")
	case "home":
		return []byte("\x1b[H")
	case "end":
		return []byte("\x1b[F")
	case "pgup":
		return []byte("\x1b[5~")
	case "pgdown":
		return []byte("\x1b[6~")

	// cmd+arrow on macOS: line start and end, the same bytes as home and end.
	case "super+left":
		return []byte("\x1b[H")
	case "super+right":
		return []byte("\x1b[F")

	// opt+arrow on macOS: word navigation. ESC b / ESC f rather than the
	// CSI 1;3 arrow forms, because readline and zle bind the ESC letters by
	// default and the modified arrows only via distribution inputrc files.
	case "alt+left":
		return []byte("\x1bb")
	case "alt+right":
		return []byte("\x1bf")
	case "alt+up":
		return []byte("\x1b[1;3A")
	case "alt+down":
		return []byte("\x1b[1;3B")
	}

	if ctrl, ok := controlByte(msg); ok {
		return []byte{ctrl}
	}

	// alt+<printable> is meta: ESC then the character, which is how a terminal
	// sends it with the common "metaSendsEscape" behavior — alt+b and alt+f
	// stay word navigation instead of degrading to plain letters. Chords bound
	// to pane management are intercepted before this function sees them; keys
	// above MaxRune are bubbletea's special codes, not characters.
	if msg.Mod&tea.ModAlt != 0 && msg.Code >= ' ' && msg.Code != 0x7f && msg.Code <= unicode.MaxRune {
		return append([]byte{0x1b}, []byte(string(msg.Code))...)
	}

	// Plain text, including anything an input method produced.
	if msg.Text != "" {
		return []byte(msg.Text)
	}
	if msg.Code >= ' ' && msg.Code != 0x7f {
		return []byte(string(msg.Code))
	}
	return nil
}

// controlByte encodes ctrl+<letter> the way a terminal does: ctrl+a is 0x01,
// ctrl+c is 0x03, and so on. ctrl+c has to reach the remote host rather than
// killing lazycssh, which is the whole reason this function exists.
func controlByte(msg tea.KeyPressMsg) (byte, bool) {
	if msg.Mod&tea.ModCtrl == 0 {
		return 0, false
	}

	switch {
	case msg.Code >= 'a' && msg.Code <= 'z':
		return byte(msg.Code-'a') + 1, true
	case msg.Code >= 'A' && msg.Code <= 'Z':
		return byte(msg.Code-'A') + 1, true
	case msg.Code == ' ' || msg.Code == '@':
		return 0, true
	case msg.Code == '[':
		return 0x1b, true
	case msg.Code == '\\':
		return 0x1c, true
	case msg.Code == '_' || msg.Code == '/':
		return 0x1f, true
	}
	return 0, false
}
