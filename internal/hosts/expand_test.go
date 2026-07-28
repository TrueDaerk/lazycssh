package hosts

import (
	"errors"
	"strings"
	"testing"
)

func TestExpand(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		want []string
	}{
		{
			name: "plain hostname is returned unchanged",
			arg:  "srv1.example.com",
			want: []string{"srv1.example.com"},
		},
		{
			name: "empty argument stays empty",
			arg:  "",
			want: []string{""},
		},
		{
			name: "comma set",
			arg:  "srv-{a,b,c}",
			want: []string{"srv-a", "srv-b", "srv-c"},
		},
		{
			name: "comma set with an empty branch",
			arg:  "srv{,-backup}",
			want: []string{"srv", "srv-backup"},
		},
		{
			name: "numeric range",
			arg:  "srv{1..4}",
			want: []string{"srv1", "srv2", "srv3", "srv4"},
		},
		{
			name: "numeric range counts down when the endpoints say so",
			arg:  "srv{3..1}",
			want: []string{"srv3", "srv2", "srv1"},
		},
		{
			name: "single element range",
			arg:  "srv{7..7}",
			want: []string{"srv7"},
		},
		{
			name: "zero padding is preserved",
			arg:  "srv1-{08..11}.example.com",
			want: []string{"srv1-08.example.com", "srv1-09.example.com", "srv1-10.example.com", "srv1-11.example.com"},
		},
		{
			name: "padding width comes from the widest endpoint",
			arg:  "srv{08..100}",
			want: []string{"srv008", "srv009", "srv010"}[:0], // replaced below
		},
		{
			name: "no padding without a leading zero",
			arg:  "srv{8..11}",
			want: []string{"srv8", "srv9", "srv10", "srv11"},
		},
		{
			name: "range with a step",
			arg:  "srv{0..20..5}",
			want: []string{"srv0", "srv5", "srv10", "srv15", "srv20"},
		},
		{
			name: "step that does not divide the span stops before the end",
			arg:  "srv{1..10..4}",
			want: []string{"srv1", "srv5", "srv9"},
		},
		{
			name: "descending range with a step",
			arg:  "srv{9..1..4}",
			want: []string{"srv9", "srv5", "srv1"},
		},
		{
			name: "negative step is read as a magnitude, direction comes from the endpoints",
			arg:  "srv{1..5..-2}",
			want: []string{"srv1", "srv3", "srv5"},
		},
		{
			name: "negative numbers",
			arg:  "n{-2..1}",
			want: []string{"n-2", "n-1", "n0", "n1"},
		},
		{
			name: "negative numbers keep the sign outside the padding",
			arg:  "n{-02..1}",
			want: []string{"n-02", "n-01", "n00", "n01"},
		},
		{
			name: "letter range",
			arg:  "srv-{a..e}",
			want: []string{"srv-a", "srv-b", "srv-c", "srv-d", "srv-e"},
		},
		{
			name: "letter range descending with a step",
			arg:  "srv-{e..a..2}",
			want: []string{"srv-e", "srv-c", "srv-a"},
		},
		{
			name: "two braces form a cartesian product, leftmost varies slowest",
			arg:  "{web,db}-{1..3}.example.com",
			want: []string{
				"web-1.example.com", "web-2.example.com", "web-3.example.com",
				"db-1.example.com", "db-2.example.com", "db-3.example.com",
			},
		},
		{
			name: "three braces",
			arg:  "{a,b}{1,2}{x,y}",
			want: []string{"a1x", "a1y", "a2x", "a2y", "b1x", "b1y", "b2x", "b2y"},
		},
		{
			name: "nested braces",
			arg:  "srv-{a,{b,c}}",
			want: []string{"srv-a", "srv-b", "srv-c"},
		},
		{
			name: "nested braces with a range inside a branch",
			arg:  "{web-{1..2},db}",
			want: []string{"web-1", "web-2", "db"},
		},
		{
			name: "deeply nested braces",
			arg:  "{a,{b,{c,d}}}",
			want: []string{"a", "b", "c", "d"},
		},
		{
			name: "escaped braces are literal",
			arg:  `srv\{1,2\}`,
			want: []string{"srv{1,2}"},
		},
		{
			name: "escaped brace next to a real one",
			arg:  `\{{a,b}\}`,
			want: []string{"{a}", "{b}"},
		},
		{
			name: "escaped comma inside a brace does not split",
			arg:  `{a\,b,c}`,
			want: []string{"a,b", "c"},
		},
		{
			name: "escaped backslash",
			arg:  `srv\\1`,
			want: []string{`srv\1`},
		},
		{
			name: "comma outside braces is an ordinary character",
			arg:  "a,b",
			want: []string{"a,b"},
		},
		{
			name: "duplicates are kept, as in bash",
			arg:  "{a,a}",
			want: []string{"a", "a"},
		},
		{
			name: "user and port survive expansion",
			arg:  "root@srv{1..2}.example.com:2222",
			want: []string{"root@srv1.example.com:2222", "root@srv2.example.com:2222"},
		},
		{
			name: "the motivating example",
			arg:  "srv1-{01..40}.example.com",
			want: nil, // built below
		},
	}

	// Two cases are easier to build than to write out.
	for i := range tests {
		switch tests[i].name {
		case "padding width comes from the widest endpoint":
			var want []string
			for n := 8; n <= 100; n++ {
				want = append(want, "srv"+pad3(n))
			}
			tests[i].want = want
		case "the motivating example":
			var want []string
			for n := 1; n <= 40; n++ {
				want = append(want, "srv1-"+pad2(n)+".example.com")
			}
			tests[i].want = want
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Expand(tt.arg)
			if err != nil {
				t.Fatalf("Expand(%q) returned error: %v", tt.arg, err)
			}
			assertEqual(t, tt.arg, got, tt.want)
		})
	}
}

func TestExpandRejectsMalformedPatterns(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantPos int
		wantMsg string // substring
	}{
		{
			name:    "unmatched opening brace",
			arg:     "srv{1..3",
			wantPos: 3,
			wantMsg: "unmatched '{'",
		},
		{
			name:    "unmatched closing brace",
			arg:     "srv}1",
			wantPos: 3,
			wantMsg: "unmatched '}'",
		},
		{
			name:    "brace with neither comma nor range",
			arg:     "srv{abc}",
			wantPos: 3,
			wantMsg: "neither alternatives",
		},
		{
			name:    "empty brace",
			arg:     "srv{}",
			wantPos: 3,
			wantMsg: "neither alternatives",
		},
		{
			name:    "mismatched range endpoint types",
			arg:     "srv{a..5}",
			wantPos: 3,
			wantMsg: "must both be integers or both be single letters",
		},
		{
			name:    "multi letter range endpoints",
			arg:     "srv{aa..az}",
			wantPos: 3,
			wantMsg: "must both be integers or both be single letters",
		},
		{
			name:    "non integer step",
			arg:     "srv{1..9..x}",
			wantPos: 3,
			wantMsg: `step "x" is not an integer`,
		},
		{
			name:    "zero step",
			arg:     "srv{1..9..0}",
			wantPos: 3,
			wantMsg: "must not be zero",
		},
		{
			name:    "trailing backslash",
			arg:     `srv\`,
			wantPos: 3,
			wantMsg: "trailing backslash",
		},
		{
			name:    "nested unmatched brace reports the inner one",
			arg:     "{a,{b}",
			wantPos: 3,
			wantMsg: "neither alternatives",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Expand(tt.arg)
			if err == nil {
				t.Fatalf("Expand(%q) = %q, want an error", tt.arg, got)
			}

			var syntaxErr *SyntaxError
			if !errors.As(err, &syntaxErr) {
				t.Fatalf("Expand(%q) returned %T, want *SyntaxError", tt.arg, err)
			}
			if syntaxErr.Arg != tt.arg {
				t.Errorf("SyntaxError.Arg = %q, want %q", syntaxErr.Arg, tt.arg)
			}
			if syntaxErr.Pos != tt.wantPos {
				t.Errorf("SyntaxError.Pos = %d, want %d (message: %s)", syntaxErr.Pos, tt.wantPos, syntaxErr)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to contain %q", err, tt.wantMsg)
			}
			// The offending argument must be quoted in the message, so a user
			// with forty arguments knows which one to fix.
			if !strings.Contains(err.Error(), tt.arg) {
				t.Errorf("error = %q, want it to name the argument %q", err, tt.arg)
			}
		})
	}
}

func TestExpandRejectsOversizedPatterns(t *testing.T) {
	tests := []struct {
		name string
		arg  string
	}{
		{name: "a single huge range", arg: "srv{1..200000}"},
		{name: "a product of moderate ranges", arg: "{1..500}-{1..500}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Expand(tt.arg); err == nil {
				t.Fatalf("Expand(%q) produced %d entries, want an error", tt.arg, len(got))
			} else if !strings.Contains(err.Error(), tt.arg) {
				t.Errorf("error = %q, want it to name the argument", err)
			}
		})
	}
}

func TestExpandAll(t *testing.T) {
	t.Run("concatenates in argument order", func(t *testing.T) {
		got, err := ExpandAll([]string{"a{1,2}", "b", "c{x,y}"})
		if err != nil {
			t.Fatalf("ExpandAll returned error: %v", err)
		}
		assertEqual(t, "a{1,2} b c{x,y}", got, []string{"a1", "a2", "b", "cx", "cy"})
	})

	t.Run("nil arguments produce nothing", func(t *testing.T) {
		got, err := ExpandAll(nil)
		if err != nil {
			t.Fatalf("ExpandAll returned error: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("ExpandAll(nil) = %q, want nothing", got)
		}
	})

	t.Run("one bad argument fails the whole call", func(t *testing.T) {
		got, err := ExpandAll([]string{"good{1,2}", "bad{", "also-good"})
		if err == nil {
			t.Fatalf("ExpandAll = %q, want an error", got)
		}
		if got != nil {
			t.Errorf("ExpandAll returned %q alongside the error, want nothing: a partial host list must not be usable", got)
		}
		if !strings.Contains(err.Error(), "bad{") {
			t.Errorf("error = %q, want it to name the offending argument", err)
		}
	})
}

func assertEqual(t *testing.T, arg string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("Expand(%q) returned %d entries, want %d\ngot:  %q\nwant: %q", arg, len(got), len(want), got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("Expand(%q)[%d] = %q, want %q", arg, i, got[i], want[i])
		}
	}
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func pad3(n int) string {
	switch {
	case n < 10:
		return "00" + itoa(n)
	case n < 100:
		return "0" + itoa(n)
	default:
		return itoa(n)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
