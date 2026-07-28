package broadcast

import (
	"fmt"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// fleet builds n hosts named web-01 .. web-nn.
func fleet(n int) []string {
	hosts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		hosts = append(hosts, fmt.Sprintf("web-%02d", i))
	}
	return hosts
}

// router builds a router over n hosts, failing the test if construction does.
func router(t *testing.T, n int) (*Router, *workingset.Manager) {
	t.Helper()
	ws := workingset.New(fleet(n))
	r, err := NewRouter(ws)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	return r, ws
}

func TestNewRouterRejectsNilWorkingSet(t *testing.T) {
	if _, err := NewRouter(nil); err == nil {
		t.Fatal("NewRouter accepted a nil working set")
	}
}

// The decision this issue exists for: with a working set of 20 out of 40 hosts,
// `all` means 20. Reaching all 40 requires a different mode.
func TestAllMeansTheWorkingSet(t *testing.T) {
	r, ws := router(t, 40)
	if err := ws.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}

	if got := r.Count(); got != 20 {
		t.Fatalf("ModeAll reached %d hosts, want the 20 in the working set", got)
	}
	targets := r.Targets()
	if targets[0] != "web-01" || targets[19] != "web-20" {
		t.Fatalf("targets = %v", targets)
	}

	if err := r.SetMode(ModeFleet); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if got := r.Count(); got != 40 {
		t.Fatalf("ModeFleet reached %d hosts, want every one of the 40", got)
	}
}

func TestTargetsFollowTheWorkingSet(t *testing.T) {
	r, ws := router(t, 40)
	if err := ws.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	if !ws.Next() {
		t.Fatal("Next() did not move")
	}
	if got := r.Targets(); got[0] != "web-21" || len(got) != 20 {
		t.Fatalf("targets did not follow the paged working set: %v", got)
	}
}

func TestSelectedIntersectsTheWorkingSet(t *testing.T) {
	r, ws := router(t, 40)
	if err := ws.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	r.Select("web-01", "web-02", "web-30")
	if err := r.SetMode(ModeSelected); err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	got := r.Targets()
	if strings.Join(got, ",") != "web-01,web-02" {
		t.Fatalf("targets = %v, want the selection inside the working set", got)
	}
	if excluded := r.Excluded(); strings.Join(excluded, ",") != "web-30" {
		t.Fatalf("Excluded() = %v, want web-30", excluded)
	}
}

func TestSelectedIsEmptyWithoutASelection(t *testing.T) {
	r, _ := router(t, 5)
	if err := r.SetMode(ModeSelected); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if got := r.Count(); got != 0 {
		t.Fatalf("Count() = %d with nothing selected, want 0", got)
	}
}

func TestToggleAndClear(t *testing.T) {
	r, _ := router(t, 5)
	if !r.Toggle("web-02") {
		t.Fatal("Toggle did not select")
	}
	if !r.IsSelected("web-02") {
		t.Fatal("IsSelected missed a toggled host")
	}
	if r.Toggle("web-02") {
		t.Fatal("Toggle did not deselect")
	}

	r.Select("web-03", "web-01")
	if got := r.Selected(); strings.Join(got, ",") != "web-01,web-03" {
		t.Fatalf("Selected() = %v, want host order", got)
	}
	r.Deselect("web-01")
	if got := r.Selected(); strings.Join(got, ",") != "web-03" {
		t.Fatalf("Selected() = %v after Deselect", got)
	}
	r.ClearSelection()
	if got := r.Selected(); len(got) != 0 {
		t.Fatalf("Selected() = %v after ClearSelection", got)
	}
}

func TestSelectWorkingSet(t *testing.T) {
	r, ws := router(t, 40)
	if err := ws.ApplySpec("first 3", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	r.SelectWorkingSet()
	if got := r.Selected(); strings.Join(got, ",") != "web-01,web-02,web-03" {
		t.Fatalf("Selected() = %v", got)
	}
}

// Single mode is how a sudo password is answered: exactly one host, the one the
// user is looking at, even when it is outside the working set.
func TestSingleTargetsTheFocusedPane(t *testing.T) {
	r, ws := router(t, 40)
	if err := ws.ApplySpec("first 20", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	if err := r.SetMode(ModeSingle); err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	if got := r.Targets(); got != nil {
		t.Fatalf("Targets() = %v with no focus, want none", got)
	}

	r.SetFocus("web-30")
	if got := r.Targets(); strings.Join(got, ",") != "web-30" {
		t.Fatalf("Targets() = %v, want the focused host", got)
	}
	if got := r.Focus(); got != "web-30" {
		t.Fatalf("Focus() = %q", got)
	}

	r.SetFocus("web-99")
	if got := r.Targets(); got != nil {
		t.Fatalf("Targets() = %v for a host that is not in the run", got)
	}
}

func TestSetModeRejectsUnknownMode(t *testing.T) {
	r, _ := router(t, 3)
	if err := r.SetMode(Mode(42)); err == nil {
		t.Fatal("SetMode accepted an unknown mode")
	}
	if r.Mode() != ModeAll {
		t.Fatalf("a rejected mode changed the router: %v", r.Mode())
	}
}

func TestParseMode(t *testing.T) {
	tests := []struct {
		in   string
		want Mode
	}{
		{"all", ModeAll},
		{"selected", ModeSelected},
		{"single", ModeSingle},
		{"fleet", ModeFleet},
	}
	for _, tc := range tests {
		got, err := ParseMode(tc.in)
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseMode(%q) = %v, want %v", tc.in, got, tc.want)
		}
		if got.String() != tc.in {
			t.Fatalf("String() = %q, want %q", got.String(), tc.in)
		}
	}
	if _, err := ParseMode("everything"); err == nil {
		t.Fatal("ParseMode accepted an unknown name")
	}
	if got := Mode(42).String(); got != "unknown(42)" {
		t.Fatalf("Mode(42).String() = %q", got)
	}
}

// The acceptance criterion: the status bar can never be read as "all 40" while
// only 20 hosts will receive input, or the reverse.
func TestDescribe(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*Router, *workingset.Manager)
		want  string
	}{
		{
			"all hosts, no working set",
			func(*Router, *workingset.Manager) {},
			"BROADCAST all (40/40 hosts)",
		},
		{
			"all, inside an ad hoc working set",
			func(_ *Router, ws *workingset.Manager) { _ = ws.ApplySpec("first 20", nil) },
			"BROADCAST set:first-20 (20/40 hosts)",
		},
		{
			"all, inside a named working set",
			func(_ *Router, ws *workingset.Manager) {
				_ = ws.ApplySpec("first 20", nil)
				_ = ws.Save("front-half")
			},
			"BROADCAST set:front-half (20/40 hosts)",
		},
		{
			"selected",
			func(r *Router, _ *workingset.Manager) {
				r.Select("web-01", "web-02", "web-03")
				_ = r.SetMode(ModeSelected)
			},
			"BROADCAST selected (3/40 hosts)",
		},
		{
			"single",
			func(r *Router, _ *workingset.Manager) {
				_ = r.SetMode(ModeSingle)
				r.SetFocus("web-07")
			},
			"BROADCAST single web-07 (1/40 hosts)",
		},
		{
			"single without a pane",
			func(r *Router, _ *workingset.Manager) { _ = r.SetMode(ModeSingle) },
			"BROADCAST single (no pane) (0/40 hosts)",
		},
		{
			"fleet",
			func(r *Router, ws *workingset.Manager) {
				_ = ws.ApplySpec("first 20", nil)
				_ = r.SetMode(ModeFleet)
			},
			"BROADCAST EVERY HOST (40/40 hosts)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r, ws := router(t, 40)
			tc.setup(r, ws)
			if got := r.Describe(); got != tc.want {
				t.Fatalf("Describe() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWarningOnlyForFleet(t *testing.T) {
	r, _ := router(t, 3)
	for _, m := range []Mode{ModeAll, ModeSelected, ModeSingle} {
		if err := r.SetMode(m); err != nil {
			t.Fatalf("SetMode(%v): %v", m, err)
		}
		if r.Warning() {
			t.Fatalf("mode %v raised the broadcast warning", m)
		}
	}
	if err := r.SetMode(ModeFleet); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if !r.Warning() {
		t.Fatal("ModeFleet did not raise the broadcast warning")
	}
}

// Describe and Targets must never disagree: every rendered count is the number
// of hosts that actually receive input.
func TestDescribeCountMatchesTargets(t *testing.T) {
	r, ws := router(t, 40)
	if err := ws.ApplySpec("21-40", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	r.Select("web-01", "web-25", "web-26")
	r.SetFocus("web-25")

	for _, m := range []Mode{ModeAll, ModeSelected, ModeSingle, ModeFleet} {
		if err := r.SetMode(m); err != nil {
			t.Fatalf("SetMode(%v): %v", m, err)
		}
		want := fmt.Sprintf("(%d/40 hosts)", len(r.Targets()))
		if !strings.Contains(r.Describe(), want) {
			t.Fatalf("mode %v: Describe() = %q, targets = %v", m, r.Describe(), r.Targets())
		}
	}
}
