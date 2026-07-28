// Package workingset models the current subject of work: which hosts the user
// is operating on right now.
//
// This is deliberately not the same thing as the render window. The window says
// how many panes fit on the screen; the working set says which hosts a command
// is about. Confusing the two is how a command reaches twice as many machines as
// intended, so they are separate types in separate packages.
package workingset

import (
	"fmt"
	"path"
	"strconv"
	"strings"
)

// Kind identifies how a [Selector] picks its members. The UI uses it to decide
// what can be paged and how a set is labelled.
type Kind int

const (
	// KindAll selects every host.
	KindAll Kind = iota
	// KindRange selects a contiguous slice of the host list by position.
	KindRange
	// KindPattern selects by glob against the host identifier.
	KindPattern
	// KindManual selects an explicit list of identifiers.
	KindManual
)

// String returns the lowercase name used in labels and error messages.
func (k Kind) String() string {
	switch k {
	case KindAll:
		return "all"
	case KindRange:
		return "range"
	case KindPattern:
		return "pattern"
	case KindManual:
		return "manual"
	default:
		return "unknown(" + strconv.Itoa(int(k)) + ")"
	}
}

// Selector picks a subset out of the full, ordered host list.
//
// A selector is a definition, not a snapshot: it is re-evaluated against the
// current host list every time, so a set defined as "web-*" keeps meaning that
// after hosts are added or removed.
type Selector interface {
	// Select returns the members, in the order they appear in all.
	Select(all []string) []string
	// Kind reports which selector this is.
	Kind() Kind
	// String renders the definition the way the status bar shows it.
	String() string
}

// All selects every host.
type All struct{}

// Select returns a copy of the whole host list.
func (All) Select(all []string) []string { return append([]string(nil), all...) }

// Kind reports [KindAll].
func (All) Kind() Kind { return KindAll }

// String renders the definition.
func (All) String() string { return "all" }

// Range selects a contiguous slice of the host list by 1-based, inclusive
// position, which is how the user counts hosts: "21-40" means the twenty-first
// through the fortieth.
//
// The bounds may point outside the current host list; they are clamped on every
// evaluation rather than at construction, because the host list can shrink.
type Range struct {
	From int
	To   int
}

// NewRange builds a range, rejecting bounds that can never select anything.
func NewRange(from, to int) (Range, error) {
	if from < 1 {
		return Range{}, fmt.Errorf("working set range: start %d is below 1", from)
	}
	if to < from {
		return Range{}, fmt.Errorf("working set range: end %d is before start %d", to, from)
	}
	return Range{From: from, To: to}, nil
}

// bounds clamps the range onto a host list of length n. It returns from > to
// when the range selects nothing.
func (r Range) bounds(n int) (from, to int) {
	from, to = r.From, r.To
	if from < 1 {
		from = 1
	}
	if to > n {
		to = n
	}
	return from, to
}

// Select returns the hosts inside the range.
func (r Range) Select(all []string) []string {
	from, to := r.bounds(len(all))
	if from > to || from > len(all) {
		return nil
	}
	return append([]string(nil), all[from-1:to]...)
}

// Kind reports [KindRange].
func (Range) Kind() Kind { return KindRange }

// Size is the number of positions the range spans, regardless of how many hosts
// currently exist. Paging keeps this constant, so "the next 20" stays 20 even
// when the last page is short.
func (r Range) Size() int {
	if r.To < r.From {
		return 0
	}
	return r.To - r.From + 1
}

// String renders the definition.
func (r Range) String() string {
	if r.From == 1 {
		return "first-" + strconv.Itoa(r.Size())
	}
	return strconv.Itoa(r.From) + "-" + strconv.Itoa(r.To)
}

// Pattern selects hosts whose identifier matches a shell glob.
type Pattern struct {
	Glob string
}

// NewPattern builds a pattern, rejecting a malformed glob rather than letting it
// silently match nothing.
func NewPattern(glob string) (Pattern, error) {
	if glob == "" {
		return Pattern{}, fmt.Errorf("working set pattern: empty pattern")
	}
	if _, err := path.Match(glob, ""); err != nil {
		return Pattern{}, fmt.Errorf("working set pattern %q: %w", glob, err)
	}
	return Pattern{Glob: glob}, nil
}

// Select returns the matching hosts in host order.
func (p Pattern) Select(all []string) []string {
	var out []string
	for _, id := range all {
		// The pattern was validated by NewPattern, so a match error here can
		// only come from a hand-built Pattern; treat it as no match.
		if ok, err := path.Match(p.Glob, id); err == nil && ok {
			out = append(out, id)
		}
	}
	return out
}

// Kind reports [KindPattern].
func (Pattern) Kind() Kind { return KindPattern }

// String renders the definition.
func (p Pattern) String() string { return p.Glob }

// Manual selects an explicit set of identifiers, which is what the user's
// space-toggled selection becomes.
type Manual struct {
	IDs []string
}

// NewManual copies the identifiers so a later mutation of the caller's slice
// cannot change the set behind its back.
func NewManual(ids []string) Manual {
	return Manual{IDs: append([]string(nil), ids...)}
}

// Select returns the members in host order, ignoring identifiers that no longer
// exist.
func (m Manual) Select(all []string) []string {
	want := make(map[string]struct{}, len(m.IDs))
	for _, id := range m.IDs {
		want[id] = struct{}{}
	}
	var out []string
	for _, id := range all {
		if _, ok := want[id]; ok {
			out = append(out, id)
		}
	}
	return out
}

// Kind reports [KindManual].
func (Manual) Kind() Kind { return KindManual }

// String renders the definition.
func (m Manual) String() string { return "selection(" + strconv.Itoa(len(m.IDs)) + ")" }

// ParseSelector reads a selector from the spec the user types: "all",
// "first 20", "20", "21-40", "web-*", or "selection" for the current manual
// selection.
//
// selection is the currently toggled host set; it is only used by the
// "selection" spec.
func ParseSelector(spec string, selection []string) (Selector, error) {
	s := strings.TrimSpace(spec)
	if s == "" {
		return nil, fmt.Errorf("working set: empty definition")
	}

	switch strings.ToLower(s) {
	case "all", "*":
		return All{}, nil
	case "selection", "selected":
		if len(selection) == 0 {
			return nil, fmt.Errorf("working set: nothing is selected")
		}
		return NewManual(selection), nil
	}

	if rest, ok := cutPrefixFold(s, "first "); ok {
		n, err := parseCount(rest)
		if err != nil {
			return nil, err
		}
		return NewRange(1, n)
	}

	if from, to, ok := strings.Cut(s, "-"); ok && isNumeric(from) && isNumeric(to) {
		a, err := parseCount(from)
		if err != nil {
			return nil, err
		}
		b, err := parseCount(to)
		if err != nil {
			return nil, err
		}
		return NewRange(a, b)
	}

	if isNumeric(s) {
		n, err := parseCount(s)
		if err != nil {
			return nil, err
		}
		return NewRange(1, n)
	}

	return NewPattern(s)
}

// cutPrefixFold strips a case-insensitive prefix.
func cutPrefixFold(s, prefix string) (string, bool) {
	if len(s) < len(prefix) || !strings.EqualFold(s[:len(prefix)], prefix) {
		return s, false
	}
	return s[len(prefix):], true
}

// parseCount reads a positive host count or position.
func parseCount(s string) (int, error) {
	s = strings.TrimSpace(s)
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("working set: %q is not a number", s)
	}
	if n < 1 {
		return 0, fmt.Errorf("working set: %d is not a valid position", n)
	}
	return n, nil
}

// isNumeric reports whether s is a plain decimal number, so "21-40" is read as a
// range while "web-01" is read as a pattern.
func isNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
