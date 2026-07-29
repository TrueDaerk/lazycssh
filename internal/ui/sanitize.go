package ui

import "strings"

// Remote output arrives with whatever escape sequences the host felt like
// sending. Colours are wanted; cursor movement is not - a pane renders
// scrollback text, not a terminal, and a stray "clear screen" or "cursor up"
// from one host must not corrupt the layout around it. There is no VT
// emulation here by design; see the full-emulation idea issue.

// tabStop is the column width a tab advances to. Panes render text, not a
// terminal, so the classic default is the only sensible interpretation.
const tabStop = 8

// sanitizeLine neutralizes everything in one scrollback line that could move
// the cursor or otherwise escape the pane. SGR sequences (colours, bold - the
// ones ending in 'm') pass through; every other escape sequence and control
// byte is removed, and tabs are expanded to spaces.
//
// A line that still contains an SGR sequence is closed with a reset, so an
// unbalanced colour from one host cannot bleed into the border or the
// neighbouring pane.
func sanitizeLine(line string) string {
	if !strings.ContainsAny(line, "\x1b\t\x00\x01\x02\x03\x04\x05\x06\a\b\v\f\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1c\x1d\x1e\x1f\x7f") {
		return line
	}

	var b strings.Builder
	b.Grow(len(line))

	sgr := false
	col := 0 // printable column, for tab expansion

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == 0x1b:
			seq, keep := escapeSequence(line[i:])
			if keep {
				b.WriteString(line[i : i+seq])
				sgr = true
			}
			i += seq - 1

		case c == '\t':
			spaces := tabStop - col%tabStop
			b.WriteString(strings.Repeat(" ", spaces))
			col += spaces

		case c < 0x20 || c == 0x7f:
			// Control bytes: BEL, backspace, vertical tab and friends. CR and
			// LF never reach here - the scrollback buffer consumes them.

		default:
			b.WriteByte(c)
			col++
		}
	}

	if sgr {
		b.WriteString("\x1b[m")
	}
	return b.String()
}

// escapeSequence measures the escape sequence starting at s[0] == ESC and
// reports whether it is an SGR sequence worth keeping. An unterminated
// sequence swallows the rest of the line: rendering half an escape sequence
// is worse than dropping the tail the remote never finished.
func escapeSequence(s string) (length int, keepSGR bool) {
	if len(s) < 2 {
		return len(s), false
	}

	switch s[1] {
	case '[': // CSI: parameters and intermediates, then one final byte
		for i := 2; i < len(s); i++ {
			if c := s[i]; c >= 0x40 && c <= 0x7e {
				return i + 1, c == 'm'
			}
		}
		return len(s), false

	case ']', 'P', 'X', '^', '_': // OSC and the string commands: run to ST or BEL
		for i := 2; i < len(s); i++ {
			if s[i] == '\a' {
				return i + 1, false
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2, false
			}
		}
		return len(s), false

	default:
		// Intermediate bytes then one final byte: charset selection like
		// "ESC ( B". Without intermediates this is a plain two-byte sequence -
		// keypad modes, RIS.
		for i := 1; i < len(s); i++ {
			if s[i] < 0x20 || s[i] > 0x2f {
				return i + 1, false
			}
		}
		return len(s), false
	}
}
