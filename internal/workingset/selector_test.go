package workingset

import (
	"fmt"
	"strings"
	"testing"
)

// fleet builds n hosts named web-01 .. web-nn, which is the shape the working
// set exists for.
func fleet(n int) []string {
	hosts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		hosts = append(hosts, fmt.Sprintf("web-%02d", i))
	}
	return hosts
}

func TestSelectorSelect(t *testing.T) {
	hosts := []string{"web-01", "web-02", "db-01", "db-02", "cache-01"}

	tests := []struct {
		name string
		sel  Selector
		want []string
	}{
		{"all", All{}, hosts},
		{"range inside", Range{From: 2, To: 4}, []string{"web-02", "db-01", "db-02"}},
		{"range clamped to end", Range{From: 4, To: 99}, []string{"db-02", "cache-01"}},
		{"range past end", Range{From: 9, To: 12}, nil},
		{"range single", Range{From: 1, To: 1}, []string{"web-01"}},
		{"pattern", Pattern{Glob: "web-*"}, []string{"web-01", "web-02"}},
		{"pattern exact", Pattern{Glob: "db-01"}, []string{"db-01"}},
		{"pattern no match", Pattern{Glob: "app-*"}, nil},
		{"manual keeps host order", NewManual([]string{"cache-01", "web-01"}),
			[]string{"web-01", "cache-01"}},
		{"manual ignores unknown ids", NewManual([]string{"gone", "db-02"}), []string{"db-02"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.sel.Select(hosts)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("Select() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAllSelectCopiesInput(t *testing.T) {
	hosts := []string{"a", "b"}
	got := All{}.Select(hosts)
	got[0] = "mutated"
	if hosts[0] != "a" {
		t.Fatalf("Select aliased its input: %v", hosts)
	}
}

func TestNewManualCopiesInput(t *testing.T) {
	ids := []string{"a", "b"}
	m := NewManual(ids)
	ids[0] = "c"
	if got := m.Select([]string{"a", "b", "c"}); strings.Join(got, ",") != "a,b" {
		t.Fatalf("Manual followed a later mutation: %v", got)
	}
}

func TestNewRangeRejectsImpossibleBounds(t *testing.T) {
	tests := []struct {
		name     string
		from, to int
	}{
		{"zero start", 0, 5},
		{"negative start", -3, 5},
		{"end before start", 10, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewRange(tc.from, tc.to); err == nil {
				t.Fatalf("NewRange(%d, %d) accepted impossible bounds", tc.from, tc.to)
			}
		})
	}
}

func TestNewPatternRejectsMalformedGlob(t *testing.T) {
	for _, glob := range []string{"", "web-[01"} {
		if _, err := NewPattern(glob); err == nil {
			t.Fatalf("NewPattern(%q) accepted a malformed glob", glob)
		}
	}
}

func TestParseSelector(t *testing.T) {
	selection := []string{"db-01", "db-02"}

	tests := []struct {
		name     string
		spec     string
		wantKind Kind
		wantStr  string
	}{
		{"all", "all", KindAll, "all"},
		{"star", "*", KindAll, "all"},
		{"uppercase all", "ALL", KindAll, "all"},
		{"count", "20", KindRange, "first-20"},
		{"first n", "first 20", KindRange, "first-20"},
		{"first n uppercase", "First 5", KindRange, "first-5"},
		{"range", "21-40", KindRange, "21-40"},
		{"padded spec", "  21-40  ", KindRange, "21-40"},
		{"pattern", "web-*", KindPattern, "web-*"},
		{"hostname with dash is a pattern", "web-01", KindPattern, "web-01"},
		{"selection", "selection", KindManual, "selection(2)"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := ParseSelector(tc.spec, selection)
			if err != nil {
				t.Fatalf("ParseSelector(%q): %v", tc.spec, err)
			}
			if sel.Kind() != tc.wantKind {
				t.Fatalf("kind = %v, want %v", sel.Kind(), tc.wantKind)
			}
			if sel.String() != tc.wantStr {
				t.Fatalf("String() = %q, want %q", sel.String(), tc.wantStr)
			}
		})
	}
}

func TestParseSelectorErrors(t *testing.T) {
	tests := []struct {
		name      string
		spec      string
		selection []string
	}{
		{"empty", "", nil},
		{"blank", "   ", nil},
		{"zero count", "0", nil},
		{"first zero", "first 0", nil},
		{"first not a number", "first ten", nil},
		{"reversed range", "40-21", nil},
		{"selection with nothing selected", "selection", nil},
		{"malformed glob", "web-[01", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ParseSelector(tc.spec, tc.selection); err == nil {
				t.Fatalf("ParseSelector(%q) accepted an invalid definition", tc.spec)
			}
		})
	}
}

func TestRangeSize(t *testing.T) {
	tests := []struct {
		r    Range
		want int
	}{
		{Range{From: 1, To: 20}, 20},
		{Range{From: 21, To: 40}, 20},
		{Range{From: 5, To: 5}, 1},
		{Range{From: 5, To: 1}, 0},
	}
	for _, tc := range tests {
		if got := tc.r.Size(); got != tc.want {
			t.Fatalf("%v.Size() = %d, want %d", tc.r, got, tc.want)
		}
	}
}

func TestKindString(t *testing.T) {
	tests := []struct {
		k    Kind
		want string
	}{
		{KindAll, "all"},
		{KindRange, "range"},
		{KindPattern, "pattern"},
		{KindManual, "manual"},
		{Kind(42), "unknown(42)"},
	}
	for _, tc := range tests {
		if got := tc.k.String(); got != tc.want {
			t.Fatalf("Kind(%d).String() = %q, want %q", tc.k, got, tc.want)
		}
	}
}
