package ui

import (
	"strings"
	"testing"
)

// The header names the pane: number, host, connection state.
func TestPaneHeaderShowsNumberNameAndState(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")

	if got := plain(a.paneHeader(0, 40, false)); !strings.HasPrefix(got, "1 web-01 pending") || !strings.HasSuffix(got, paneCloseButton) {
		t.Fatalf("paneHeader = %q", got)
	}
	if got := plain(a.paneHeader(1, 40, false)); !strings.HasPrefix(got, "2 web-02 pending") {
		t.Fatalf("paneHeader = %q", got)
	}

	// The acceptance criterion: state changes are reflected immediately. No
	// message is processed between connect and render; the header reads live
	// state.
	fleet.connect(t, "web-01")
	if got := plain(a.paneHeader(0, 40, false)); !strings.HasPrefix(got, "1 web-01 connected") {
		t.Fatalf("after connecting: %q", got)
	}
	fleet.fail(t, "web-02")
	if got := plain(a.paneHeader(1, 40, false)); !strings.HasPrefix(got, "2 web-02 failed") {
		t.Fatalf("after failing: %q", got)
	}
}

// The acceptance criterion: at the minimum pane size the header truncates the
// host name from the left, so the distinguishing suffix survives.
func TestPaneHeaderTruncatesTheNameFromTheLeft(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-production-eu-central-1a-40.example.com")

	// MinPaneWidth minus the border is the narrowest header there is.
	got := plain(a.paneHeader(0, MinPaneWidth-2, false))
	if l := len([]rune(got)); l > MinPaneWidth-2 {
		t.Fatalf("header is %d columns at the minimum size: %q", l, got)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("a too-long name is not marked as truncated: %q", got)
	}
	if !strings.Contains(got, "ple.com") {
		t.Fatalf("the distinguishing suffix did not survive: %q", got)
	}
	if !strings.HasPrefix(got, "1 ") {
		t.Fatalf("the pane number is gone: %q", got)
	}
}

// When the width cannot hold a readable name next to the state, the state goes
// first: an unreadable name helps no one.
func TestPaneHeaderDropsTheStateBeforeTheName(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-production-01")

	got := plain(a.paneHeader(0, 12, false))
	if strings.Contains(got, "pending") {
		t.Fatalf("the state crowded out the name: %q", got)
	}
	if !strings.Contains(got, "…") || !strings.Contains(got, "01") {
		t.Fatalf("the name suffix did not survive: %q", got)
	}
}

func TestPaneHeaderDegenerateCases(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	if got := a.paneHeader(-1, 40, false); got != "" {
		t.Fatalf("negative index rendered %q", got)
	}
	if got := a.paneHeader(5, 40, false); got != "" {
		t.Fatalf("out-of-range index rendered %q", got)
	}
	if got := a.paneHeader(0, 0, false); got != "" {
		t.Fatalf("zero width rendered %q", got)
	}
}

// The header reaches the frame: the grid shows the state next to each host.
func TestPaneHeaderReachesTheFrame(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")

	if view := plain(a.View().Content); !strings.Contains(view, "web-01 connected") {
		t.Fatalf("the frame does not carry the header:\n%s", view)
	}
}

func TestTruncateLeft(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"short", 10, "short"},
		{"exactly-10", 10, "exactly-10"},
		{"a-very-long-hostname", 8, "…ostname"},
		{"abc", 2, "…c"},
		{"abc", 1, "c"},
		{"abc", 0, ""},
	}
	for _, tc := range tests {
		if got := truncateLeft(tc.in, tc.width); got != tc.want {
			t.Fatalf("truncateLeft(%q, %d) = %q, want %q", tc.in, tc.width, got, tc.want)
		}
	}
}
