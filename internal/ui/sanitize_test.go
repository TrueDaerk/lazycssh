package ui

import "testing"

func TestSanitizeLine(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"plain text is untouched", "hello world", "hello world"},
		{"SGR colours survive", "\x1b[31mred\x1b[0m file", "\x1b[31mred\x1b[0m file\x1b[m"},
		{"256-colour SGR survives", "\x1b[38;5;196mx\x1b[m", "\x1b[38;5;196mx\x1b[m\x1b[m"},
		{"cursor up is removed", "before\x1b[2Aafter", "beforeafter"},
		{"cursor position is removed", "\x1b[10;20Htext", "text"},
		{"clear screen is removed", "\x1b[2Jclean", "clean"},
		{"erase line is removed", "a\x1b[Kb", "ab"},
		{"OSC title ending in BEL is removed", "\x1b]0;evil title\atext", "text"},
		{"OSC ending in ST is removed", "\x1b]8;;http://x\x1b\\link", "link"},
		{"DCS is removed", "\x1bPq#0\x1b\\rest", "rest"},
		{"charset selection is removed", "\x1b(Btext", "text"},
		{"RIS is removed", "\x1bctext", "text"},
		{"unterminated CSI swallows the tail", "ok\x1b[12;", "ok"},
		{"unterminated OSC swallows the tail", "ok\x1b]0;title", "ok"},
		{"lone trailing escape is dropped", "ok\x1b", "ok"},
		{"BEL is dropped", "ding\adong", "dingdong"},
		{"backspace is dropped", "ab\bc", "abc"},
		{"DEL is dropped", "a\x7fb", "ab"},
		{"tab expands to the next stop", "a\tb", "a       b"},
		{"tab at a stop advances a full stop", "12345678\tx", "12345678        x"},
		{"mixed: colours kept, movement gone", "\x1b[32mok\x1b[0m\x1b[1A\x1b[2K", "\x1b[32mok\x1b[0m\x1b[m"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeLine(tc.in); got != tc.want {
				t.Fatalf("sanitizeLine(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
