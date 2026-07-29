package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func boxTheme() Theme { return NewTheme(Options{Dark: true}) }

// The title sits inside the top border line, lazygit style.
func TestTitledBoxEmbedsTheTitle(t *testing.T) {
	box := plain(titledBox(boxTheme(), false, 24, 5, "Hosts [2]", "web-01"))
	lines := strings.Split(box, "\n")

	if len(lines) != 5 {
		t.Fatalf("box has %d lines, want 5:\n%s", len(lines), box)
	}
	if !strings.Contains(lines[0], "Hosts [2]") {
		t.Fatalf("the title is not in the top line: %q", lines[0])
	}
	if !strings.HasPrefix(lines[0], "╭") || !strings.HasSuffix(lines[0], "╮") {
		t.Fatalf("the top line has no corners: %q", lines[0])
	}
	if !strings.Contains(box, "web-01") {
		t.Fatalf("the content is missing:\n%s", box)
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰") {
		t.Fatalf("the bottom border is missing: %q", lines[len(lines)-1])
	}
}

// The focused box uses the thick border set, so focus survives NoColor.
func TestTitledBoxFocusIsThick(t *testing.T) {
	box := plain(titledBox(NewTheme(Options{NoColor: true}), true, 24, 5, "Hosts [2]", ""))
	if !strings.HasPrefix(box, "┏") {
		t.Fatalf("the focused box does not use the thick border:\n%s", box)
	}
}

// Every line of the box is exactly as wide as asked, so stacked boxes align.
func TestTitledBoxWidthIsExact(t *testing.T) {
	box := plain(titledBox(boxTheme(), false, 24, 5, "Hosts [2]", "web-01"))
	for i, line := range strings.Split(box, "\n") {
		if w := lipgloss.Width(line); w != 24 {
			t.Fatalf("line %d is %d wide, want 24: %q", i, w, line)
		}
	}
}

// A long title cannot push the corner off the edge of the box.
func TestTitledBoxClipsTheTitle(t *testing.T) {
	box := plain(titledBox(boxTheme(), false, 12, 3, "a very long panel title", ""))
	top := strings.Split(box, "\n")[0]
	if w := lipgloss.Width(top); w != 12 {
		t.Fatalf("the top line is %d wide, want 12: %q", w, top)
	}
	if !strings.HasSuffix(top, "╮") {
		t.Fatalf("the corner was pushed off: %q", top)
	}
}

// Squeezed boxes degrade honestly rather than drawing nonsense.
func TestTitledBoxDegradesAtSmallHeights(t *testing.T) {
	th := boxTheme()

	two := plain(titledBox(th, false, 20, 2, "Hosts [2]", "body"))
	if lines := strings.Split(two, "\n"); len(lines) != 2 || !strings.HasPrefix(lines[1], "╰") {
		t.Fatalf("a two-row box is not title plus bottom edge:\n%s", two)
	}

	one := plain(titledBox(th, false, 20, 1, "Hosts [2]", "body"))
	if strings.Contains(one, "\n") || !strings.Contains(one, "Hosts [2]") {
		t.Fatalf("a one-row box is not the bare title: %q", one)
	}

	if got := titledBox(th, false, 20, 0, "Hosts [2]", "body"); got != "" {
		t.Fatalf("a zero-height box rendered %q", got)
	}
	if got := titledBox(th, false, 0, 5, "Hosts [2]", "body"); got != "" {
		t.Fatalf("a zero-width box rendered %q", got)
	}
}

func TestSidebarHeights(t *testing.T) {
	tests := []struct {
		name                    string
		total, panels, selected int
		want                    []int
	}{
		{"roomy", 40, 5, 1, []int{3, 28, 3, 3, 3}},
		{"exact fit", 15, 5, 0, []int{3, 3, 3, 3, 3}},
		{"tight collapses to titles", 12, 5, 2, []int{1, 1, 8, 1, 1}},
		{"tiny leaves only the selected", 4, 5, 4, []int{0, 0, 0, 0, 4}},
		{"zero height", 0, 5, 0, []int{0, 0, 0, 0, 0}},
		{"selected clamped", 10, 3, 9, []int{3, 3, 4}},
		{"one panel", 7, 1, 0, []int{7}},
	}
	for _, tc := range tests {
		got := SidebarHeights(tc.total, tc.panels, tc.selected)
		if len(got) != len(tc.want) {
			t.Fatalf("%s: %d heights, want %d", tc.name, len(got), len(tc.want))
		}
		sum := 0
		for i, h := range got {
			if h < 0 {
				t.Fatalf("%s: height %d is negative", tc.name, i)
			}
			if h != tc.want[i] {
				t.Fatalf("%s: heights = %v, want %v", tc.name, got, tc.want)
			}
			sum += h
		}
		if tc.total > 0 && sum != tc.total {
			t.Fatalf("%s: heights sum to %d, want %d", tc.name, sum, tc.total)
		}
	}
}

func TestSidebarHeightsNoPanels(t *testing.T) {
	if got := SidebarHeights(20, 0, 0); got != nil {
		t.Fatalf("SidebarHeights with no panels = %v, want nil", got)
	}
}
