// Package hosts resolves the host arguments given on the command line into the
// concrete list of machines to connect to.
package hosts

import (
	"fmt"
	"strconv"
	"strings"
)

// maxExpansions bounds what a single argument may expand to. Brace expansion is
// multiplicative, so a typo like {1..1000}{1..1000} would otherwise allocate a
// million strings before anyone notices.
const maxExpansions = 100_000

// SyntaxError reports a malformed host pattern. Unlike bash, which silently
// leaves a pattern it cannot parse as a literal, lazycssh rejects it: a typo in
// a hostname pattern must surface before it is used to open connections.
type SyntaxError struct {
	// Arg is the offending argument, exactly as given on the command line.
	Arg string
	// Pos is the byte offset within Arg where the problem starts.
	Pos int
	// Msg describes what is wrong.
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("invalid host pattern %q at position %d: %s", e.Arg, e.Pos, e.Msg)
}

// Expand performs bash-style brace expansion on a single host argument.
//
//	srv1-{01..40}.example.com   -> srv1-01.example.com … srv1-40.example.com
//	{web,db}-{1..3}.example.com -> web-1… web-3, db-1… db-3
//	srv-{a..c}                  -> srv-a, srv-b, srv-c
//
// The expansion is performed by lazycssh itself and never by a shell, so a
// quoted argument behaves exactly like an unquoted one and no argument can
// reach a shell as code. Ordering matches bash: the leftmost brace varies
// slowest. Duplicates are not removed, also matching bash.
//
// A backslash escapes the following character, so `\{` is a literal brace.
func Expand(arg string) ([]string, error) {
	p := &parser{arg: arg}

	s, err := p.parseSequence(false)
	if err != nil {
		return nil, err
	}

	out, err := s.variants()
	if err != nil {
		return nil, fmt.Errorf("host pattern %q: %w", arg, err)
	}
	return out, nil
}

// ExpandAll expands every argument in order and concatenates the results. The
// first malformed argument aborts the whole call: connecting to some of the
// intended hosts because a later pattern had a typo is worse than connecting to
// none.
func ExpandAll(args []string) ([]string, error) {
	var out []string
	for _, arg := range args {
		expanded, err := Expand(arg)
		if err != nil {
			return nil, err
		}
		out = append(out, expanded...)
		if len(out) > maxExpansions {
			return nil, fmt.Errorf("host arguments expand to more than %d entries", maxExpansions)
		}
	}
	return out, nil
}

// node is one element of a parsed pattern: either a literal run of characters or
// a brace group.
type node interface {
	// variants returns every string this node can produce, in bash order.
	variants() ([]string, error)
}

// literal is a run of characters with no expansion, escapes already resolved.
type literal string

func (l literal) variants() ([]string, error) { return []string{string(l)}, nil }

// alternatives is a brace group: every branch, in the order written.
type alternatives []seq

func (a alternatives) variants() ([]string, error) {
	var out []string
	for _, s := range a {
		vs, err := s.variants()
		if err != nil {
			return nil, err
		}
		out = append(out, vs...)
	}
	return out, nil
}

// seq is a concatenation of nodes; its variants are their cartesian product.
type seq []node

func (s seq) variants() ([]string, error) {
	out := []string{""}
	for _, n := range s {
		vs, err := n.variants()
		if err != nil {
			return nil, err
		}
		if len(out)*len(vs) > maxExpansions {
			return nil, fmt.Errorf("expands to more than %d entries", maxExpansions)
		}
		next := make([]string, 0, len(out)*len(vs))
		for _, prefix := range out {
			for _, v := range vs {
				next = append(next, prefix+v)
			}
		}
		out = next
	}
	return out, nil
}

// parser walks an argument once, left to right.
type parser struct {
	arg string
	pos int
}

func (p *parser) errAt(pos int, format string, args ...any) error {
	return &SyntaxError{Arg: p.arg, Pos: pos, Msg: fmt.Sprintf(format, args...)}
}

// parseSequence reads until the end of the argument, or - when inBrace is set -
// until the ',' or '}' that ends the current branch. The terminator itself is
// left for the caller to consume.
func (p *parser) parseSequence(inBrace bool) (seq, error) {
	var (
		s   seq
		buf strings.Builder
	)
	flush := func() {
		if buf.Len() > 0 {
			s = append(s, literal(buf.String()))
			buf.Reset()
		}
	}

	for p.pos < len(p.arg) {
		switch c := p.arg[p.pos]; c {
		case '\\':
			if p.pos+1 >= len(p.arg) {
				return nil, p.errAt(p.pos, "trailing backslash")
			}
			buf.WriteByte(p.arg[p.pos+1])
			p.pos += 2

		case '{':
			flush()
			n, err := p.parseBrace()
			if err != nil {
				return nil, err
			}
			s = append(s, n)

		case '}':
			if !inBrace {
				return nil, p.errAt(p.pos, "unmatched '}'; use \\} for a literal brace")
			}
			flush()
			return s, nil

		case ',':
			if !inBrace {
				// A comma outside braces is an ordinary character.
				buf.WriteByte(c)
				p.pos++
				continue
			}
			flush()
			return s, nil

		default:
			buf.WriteByte(c)
			p.pos++
		}
	}

	flush()
	return s, nil
}

// parseBrace consumes a complete brace group, starting at the opening brace.
func (p *parser) parseBrace() (node, error) {
	open := p.pos
	p.pos++ // consume '{'
	contentStart := p.pos

	var alts alternatives
	for {
		branch, err := p.parseSequence(true)
		if err != nil {
			return nil, err
		}
		alts = append(alts, branch)

		if p.pos >= len(p.arg) {
			return nil, p.errAt(open, "unmatched '{'; use \\{ for a literal brace")
		}
		if p.arg[p.pos] == ',' {
			p.pos++
			continue
		}

		// At the closing brace.
		content := p.arg[contentStart:p.pos]
		p.pos++

		if len(alts) > 1 {
			return alts, nil
		}

		// A single branch is only meaningful as a range: {1..9}, {a..f}.
		r, ok, err := parseRange(content)
		if err != nil {
			return nil, p.errAt(open, "%s", err)
		}
		if ok {
			return r, nil
		}
		return nil, p.errAt(open,
			"%s is neither alternatives ({a,b}) nor a range ({1..9}); use \\{ for a literal brace",
			strconv.Quote("{"+content+"}"))
	}
}

// parseRange interprets the inside of a brace group as a range. ok is false when
// the content is not shaped like a range at all; an error means it is shaped
// like one but is invalid, which must not be mistaken for a literal.
func parseRange(content string) (node, bool, error) {
	parts := strings.Split(content, "..")
	if len(parts) != 2 && len(parts) != 3 {
		return nil, false, nil
	}

	step := 1
	if len(parts) == 3 {
		n, err := strconv.Atoi(parts[2])
		if err != nil {
			return nil, false, fmt.Errorf("range step %q is not an integer", parts[2])
		}
		if n == 0 {
			return nil, false, fmt.Errorf("range step must not be zero")
		}
		// bash ignores the sign of the step and derives the direction from the
		// endpoints; do the same so {9..1..2} counts down rather than producing
		// nothing.
		step = n
		if step < 0 {
			step = -step
		}
	}

	if values, ok, err := numericRange(parts[0], parts[1], step); ok || err != nil {
		return values, ok, err
	}
	return alphaRange(parts[0], parts[1], step)
}

// numericRange builds an integer range, preserving zero padding when either
// endpoint is written with a leading zero, as bash does: {01..40} yields "01",
// not "1". Padding is applied to the digits; a negative value keeps its sign in
// front of the padded digits.
func numericRange(from, to string, step int) (node, bool, error) {
	start, err1 := strconv.Atoi(from)
	end, err2 := strconv.Atoi(to)
	if err1 != nil || err2 != nil {
		return nil, false, nil
	}

	width := 0
	if padded(from) || padded(to) {
		width = max(digitCount(from), digitCount(to))
	}

	if count := rangeLength(start, end, step); count > maxExpansions {
		return nil, false, fmt.Errorf("range {%s..%s} expands to %d entries, more than the limit of %d",
			from, to, count, maxExpansions)
	}

	var alts alternatives
	each(start, end, step, func(i int) {
		alts = append(alts, seq{literal(formatPadded(i, width))})
	})
	return alts, true, nil
}

// alphaRange builds a range over single letters: {a..f}, {Z..W}.
func alphaRange(from, to string, step int) (node, bool, error) {
	fromRunes, toRunes := []rune(from), []rune(to)
	if len(fromRunes) != 1 || len(toRunes) != 1 {
		return nil, false, fmt.Errorf(
			"range endpoints %q and %q must both be integers or both be single letters", from, to)
	}
	if !isLetter(fromRunes[0]) || !isLetter(toRunes[0]) {
		return nil, false, fmt.Errorf(
			"range endpoints %q and %q must both be integers or both be single letters", from, to)
	}

	var alts alternatives
	each(int(fromRunes[0]), int(toRunes[0]), step, func(i int) {
		alts = append(alts, seq{literal(string(rune(i)))})
	})
	return alts, true, nil
}

// each calls fn for every value from start to end inclusive, walking in whichever
// direction the endpoints imply. step is always positive.
func each(start, end, step int, fn func(int)) {
	if start <= end {
		for i := start; i <= end; i += step {
			fn(i)
		}
		return
	}
	for i := start; i >= end; i -= step {
		fn(i)
	}
}

// rangeLength counts what each would produce, without producing it.
func rangeLength(start, end, step int) int {
	span := end - start
	if span < 0 {
		span = -span
	}
	return span/step + 1
}

// padded reports whether a numeric endpoint was written with a leading zero,
// which is what turns on zero padding for the whole range.
func padded(s string) bool {
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	return len(s) > 1 && s[0] == '0'
}

// digitCount returns the number of digits in a numeric endpoint, ignoring a sign.
func digitCount(s string) int {
	s = strings.TrimPrefix(s, "-")
	s = strings.TrimPrefix(s, "+")
	return len(s)
}

// formatPadded renders i with at least width digits, keeping the sign outside
// the padding: -3 at width 3 becomes "-003".
func formatPadded(i, width int) string {
	if width == 0 {
		return strconv.Itoa(i)
	}
	if i < 0 {
		return "-" + fmt.Sprintf("%0*d", width, -i)
	}
	return fmt.Sprintf("%0*d", width, i)
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
