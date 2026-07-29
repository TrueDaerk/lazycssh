package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// escapeKeystroke leaves passthrough. It is the telnet escape, for the same
// reason telnet chose it: it is not a key any shell wants, and a user who is
// stuck needs one sequence that always means "give me my keyboard back".
const escapeKeystroke = "ctrl+]"

// Passthrough reports whether raw keystrokes are going to the hosts.
func (a App) Passthrough() bool { return a.passthrough }

// togglePassthrough switches raw keystroke mode.
func (a App) togglePassthrough() App {
	a.passthrough = !a.passthrough
	if a.passthrough {
		a.lastDelivery = "passthrough: keys go to the hosts, " + escapeKeystroke + " to stop"
	} else {
		a.lastDelivery = "passthrough off"
	}
	return a
}

// handlePassthroughKey forwards one key press to the hosts.
//
// While passthrough is on, lazycssh has exactly one binding left. Everything
// else - tab, ctrl+c, ctrl+d, ctrl+r, the arrows - is encoded and written to the
// remote PTY, because a shell that cannot see tab or ctrl+c is not a shell.
func (a App) handlePassthroughKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == escapeKeystroke {
		return a.togglePassthrough(), nil
	}

	raw := keystrokeBytes(msg)
	if len(raw) == 0 {
		return a, nil
	}
	if a.cfg.Sender == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}

	// Raw keystrokes are never recorded. This is the mode a password is typed
	// in, and the audit trail is for commands, not for keys - see
	// wiki/core/command-log.md.
	delivery, err := a.cfg.Sender.Send(raw)
	if err != nil {
		a.lastDelivery = delivery.String() + ": " + err.Error()
	}
	return a, nil
}

// passthroughLabel is what the status bar says while passthrough is on. It
// always names the escape key: a user who cannot get their keyboard back has
// lost the program, not a keystroke.
func (a App) passthroughLabel() string {
	label := fmt.Sprintf("PASSTHROUGH — %s to stop", escapeKeystroke)
	if a.cfg.Targets == nil {
		return label
	}

	// Sending raw keys to more than one host means their replies - tab
	// completions above all - are answers to different questions. The panes
	// keep them apart, and the label says how many are answering.
	if n := a.cfg.Targets.Count(); n > 1 {
		label += fmt.Sprintf(" — %d hosts answer separately", n)
	}
	if a.cfg.Targets.Mode() != broadcast.ModeSingle && a.cfg.Targets.Count() > 1 {
		label += ", press s for one pane"
	}
	return label
}

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
	}

	if ctrl, ok := controlByte(msg); ok {
		return []byte{ctrl}
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

// describeKeystroke renders raw bytes for an error message, without letting a
// typed password appear in one: only the length is reported for anything longer
// than a single key.
func describeKeystroke(raw []byte) string {
	if len(raw) == 1 {
		return fmt.Sprintf("%q", string(raw))
	}
	return fmt.Sprintf("%d bytes", len(raw))
}

// passthroughHint is the short help line shown while passthrough is on. The
// generated help is suspended with every other binding, so this is the only
// thing on screen telling the user how to get out.
func passthroughHint() string {
	return strings.Join([]string{escapeKeystroke, "stop passthrough"}, " ")
}
