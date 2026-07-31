package broadcast

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/term"
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
			"BROADCAST all (40 hosts)",
		},
		{
			"all, inside an ad hoc working set",
			func(_ *Router, ws *workingset.Manager) { _ = ws.ApplySpec("first 20", nil) },
			"BROADCAST set:first-20 (20 hosts)",
		},
		{
			"all, inside a named working set",
			func(_ *Router, ws *workingset.Manager) {
				_ = ws.ApplySpec("first 20", nil)
				_ = ws.Save("front-half")
			},
			"BROADCAST set:front-half (20 hosts)",
		},
		{
			"selected",
			func(r *Router, _ *workingset.Manager) {
				r.Select("web-01", "web-02", "web-03")
				_ = r.SetMode(ModeSelected)
			},
			"BROADCAST selected (3 hosts)",
		},
		{
			"single",
			func(r *Router, _ *workingset.Manager) {
				_ = r.SetMode(ModeSingle)
				r.SetFocus("web-07")
			},
			"BROADCAST single web-07 (1 host)",
		},
		{
			"single without a pane",
			func(r *Router, _ *workingset.Manager) { _ = r.SetMode(ModeSingle) },
			"BROADCAST single (no pane) (0 hosts)",
		},
		{
			"fleet",
			func(r *Router, ws *workingset.Manager) {
				_ = ws.ApplySpec("first 20", nil)
				_ = r.SetMode(ModeFleet)
			},
			"BROADCAST EVERY HOST (40 hosts)",
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
		want := fmt.Sprintf("(%d host", len(r.Targets()))
		if !strings.Contains(r.Describe(), want) {
			t.Fatalf("mode %v: Describe() = %q, targets = %v", m, r.Describe(), r.Targets())
		}
	}
}

// fakeSessions is a transport where each host is up or down and every write is
// recorded, so delivery can be tested without a network.
type fakeSessions struct {
	up      map[string]bool
	writes  map[string]string
	failing map[string]bool
	// writerless hosts report connected but have no writer - the race where
	// a session drops between Targets() and the write (issue #133).
	writerless map[string]bool
	// altScreen hosts run a full-screen app (vim); all and selected mode
	// must keep their keystrokes out of them.
	altScreen map[string]bool
}

func newFakeSessions(ids ...string) *fakeSessions {
	f := &fakeSessions{
		up:         make(map[string]bool),
		writes:     make(map[string]string),
		failing:    make(map[string]bool),
		writerless: make(map[string]bool),
		altScreen:  make(map[string]bool),
	}
	for _, id := range ids {
		f.up[id] = true
	}
	return f
}

func (f *fakeSessions) Connected(id string) bool { return f.up[id] }

func (f *fakeSessions) AltScreen(id string) bool { return f.altScreen[id] }

func (f *fakeSessions) SendKey(id string, k term.KeyEvent) bool {
	if !f.up[id] || f.writerless[id] || f.failing[id] {
		return false
	}
	f.writes[id] += "<" + k.String() + ">"
	return true
}

func (f *fakeSessions) Writer(id string) (io.Writer, bool) {
	if !f.up[id] || f.writerless[id] {
		return nil, false
	}
	return &fakeWriter{sessions: f, id: id}, true
}

// fakeWriter records what a host received, or fails if the host is marked
// failing - a broken pipe on a session that still reports as connected.
type fakeWriter struct {
	sessions *fakeSessions
	id       string
}

func (w *fakeWriter) Write(p []byte) (int, error) {
	if w.sessions.failing[w.id] {
		return 0, errors.New("broken pipe")
	}
	w.sessions.writes[w.id] += string(p)
	return len(p), nil
}

// The acceptance criterion: the count shown always equals the number of
// sessions that actually receive the next byte.
func TestTargetsExcludeHostsThatAreDown(t *testing.T) {
	r, _ := router(t, 8)
	sessions := newFakeSessions("web-01", "web-02", "web-03", "web-04",
		"web-05", "web-06", "web-07", "web-08")
	sessions.up["web-03"] = false
	r.Attach(sessions)

	if !r.Attached() {
		t.Fatal("Attached() = false after Attach")
	}
	if got := r.ScopeCount(); got != 8 {
		t.Fatalf("ScopeCount() = %d, want 8", got)
	}
	if got := r.Count(); got != 7 {
		t.Fatalf("Count() = %d, want 7", got)
	}
	if got := strings.Join(r.Unreachable(), ","); got != "web-03" {
		t.Fatalf("Unreachable() = %q", got)
	}
	if got := r.Describe(); got != "BROADCAST all (7/8 up)" {
		t.Fatalf("Describe() = %q", got)
	}
}

// A keystroke meant for one vim must not reach twenty of them: all and
// selected mode exclude hosts whose remote app is on the alternate screen.
func TestTargetsExcludeAltScreenHosts(t *testing.T) {
	tests := []struct {
		name     string
		mode     Mode
		selected []string
		want     string
	}{
		{"all skips the vim host", ModeAll, nil, "web-01,web-03"},
		{"selected skips the vim host", ModeSelected, []string{"web-01", "web-02"}, "web-01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := router(t, 3)
			sessions := newFakeSessions("web-01", "web-02", "web-03")
			sessions.altScreen["web-02"] = true
			r.Attach(sessions)

			if err := r.SetMode(tt.mode); err != nil {
				t.Fatalf("SetMode: %v", err)
			}
			for _, id := range tt.selected {
				r.Toggle(id)
			}

			if got := strings.Join(r.Targets(), ","); got != tt.want {
				t.Fatalf("Targets() = %q, want %q", got, tt.want)
			}
			if got := strings.Join(r.AltScreenSkipped(), ","); got != "web-02" {
				t.Fatalf("AltScreenSkipped() = %q, want %q", got, "web-02")
			}
		})
	}
}

// Single mode is how one talks to the full-screen app, and fleet mode is the
// explicit every-host escape hatch: neither excludes alt-screen hosts.
func TestSingleAndFleetStillReachAltScreenHosts(t *testing.T) {
	for _, mode := range []Mode{ModeSingle, ModeFleet} {
		r, _ := router(t, 3)
		sessions := newFakeSessions("web-01", "web-02", "web-03")
		sessions.altScreen["web-02"] = true
		r.Attach(sessions)

		if err := r.SetMode(mode); err != nil {
			t.Fatalf("SetMode: %v", err)
		}
		r.SetFocus("web-02")

		targets := strings.Join(r.Targets(), ",")
		if !strings.Contains(targets, "web-02") {
			t.Fatalf("mode %v: Targets() = %q, web-02 missing", mode, targets)
		}
		if got := r.AltScreenSkipped(); got != nil {
			t.Fatalf("mode %v: AltScreenSkipped() = %v, want nil", mode, got)
		}
	}
}

// The exclusion is visible where the target count is read: the status label.
func TestDescribeNamesTheAltScreenSkip(t *testing.T) {
	r, _ := router(t, 3)
	sessions := newFakeSessions("web-01", "web-02", "web-03")
	sessions.altScreen["web-02"] = true
	r.Attach(sessions)

	if got := r.Describe(); got != "BROADCAST all (2/3 up, 1 alt-screen skipped)" {
		t.Fatalf("Describe() = %q", got)
	}

	sessions.altScreen["web-02"] = false
	if got := r.Describe(); got != "BROADCAST all (3/3 up)" {
		t.Fatalf("Describe() after the app quit = %q", got)
	}
}

func TestDescribeWithoutATransportReportsTheScopeOnly(t *testing.T) {
	r, ws := router(t, 8)
	if got := r.Describe(); got != "BROADCAST all (8 hosts)" {
		t.Fatalf("Describe() = %q", got)
	}
	if got := r.Unreachable(); got != nil {
		t.Fatalf("Unreachable() = %v without a transport", got)
	}

	if err := ws.ApplySpec("first 1", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	if got := r.Describe(); got != "BROADCAST set:first-1 (1 host)" {
		t.Fatalf("Describe() = %q", got)
	}
}

func TestSendReachesExactlyTheTargets(t *testing.T) {
	r, ws := router(t, 4)
	sessions := newFakeSessions("web-01", "web-02", "web-03", "web-04")
	sessions.up["web-04"] = false
	r.Attach(sessions)

	if err := ws.ApplySpec("first 3", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}

	d, err := r.Send([]byte("uptime\n"))
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.Delivered != 3 || d.Targets != 3 || d.Scope != 3 {
		t.Fatalf("Delivery = %+v", d)
	}
	for _, id := range []string{"web-01", "web-02", "web-03"} {
		if sessions.writes[id] != "uptime\n" {
			t.Fatalf("%s received %q", id, sessions.writes[id])
		}
	}
	if sessions.writes["web-04"] != "" {
		t.Fatalf("a host outside the working set received %q", sessions.writes["web-04"])
	}
}

// A command that reaches fewer hosts than it addressed says so: quietly
// reaching seven of forty machines is the failure this tool exists to surface.
func TestSendReportsWhoMissedIt(t *testing.T) {
	r, _ := router(t, 3)
	sessions := newFakeSessions("web-01", "web-02", "web-03")
	sessions.up["web-02"] = false
	sessions.failing["web-03"] = true
	r.Attach(sessions)

	d, err := r.Send([]byte("uptime\n"))
	if err == nil {
		t.Fatal("Send hid a write failure")
	}
	if d.Scope != 3 || d.Targets != 2 || d.Delivered != 1 {
		t.Fatalf("Delivery = %+v", d)
	}
	if strings.Join(d.Failed, ",") != "web-03" {
		t.Fatalf("Failed = %v", d.Failed)
	}
	if got := d.String(); got != "sent to 1/3 hosts (2 did not receive it)" {
		t.Fatalf("String() = %q", got)
	}

	// The one host that was up still got it: a broken pipe on one machine is
	// one dead pane, not a command the fleet half missed.
	if sessions.writes["web-01"] != "uptime\n" {
		t.Fatalf("web-01 received %q", sessions.writes["web-01"])
	}
}

func TestSendWithoutATransport(t *testing.T) {
	r, _ := router(t, 3)
	d, err := r.Send([]byte("uptime\n"))
	if err == nil {
		t.Fatal("Send succeeded with no transport attached")
	}
	if d.Delivered != 0 {
		t.Fatalf("Delivery = %+v", d)
	}
}

func TestDeliveryString(t *testing.T) {
	tests := []struct {
		d    Delivery
		want string
	}{
		{Delivery{Scope: 40, Delivered: 40}, "sent to 40/40 hosts"},
		{Delivery{Scope: 1, Delivered: 1}, "sent to 1/1 host"},
		{Delivery{Scope: 40, Delivered: 7}, "sent to 7/40 hosts (33 did not receive it)"},
	}
	for _, tc := range tests {
		if got := tc.d.String(); got != tc.want {
			t.Fatalf("String() = %q, want %q", got, tc.want)
		}
	}
}

// Single mode is what a sudo prompt needs: exactly one host, and it is still
// filtered by whether that host can take input.
func TestSingleModeRespectsLiveness(t *testing.T) {
	r, _ := router(t, 3)
	sessions := newFakeSessions("web-01", "web-02", "web-03")
	r.Attach(sessions)

	if err := r.SetMode(ModeSingle); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	r.SetFocus("web-02")
	if got := r.Describe(); got != "BROADCAST single web-02 (1/1 up)" {
		t.Fatalf("Describe() = %q", got)
	}

	sessions.up["web-02"] = false
	if got := r.Count(); got != 0 {
		t.Fatalf("Count() = %d for a focused host that is down", got)
	}
	if got := r.Describe(); got != "BROADCAST single web-02 (0/1 up)" {
		t.Fatalf("Describe() = %q", got)
	}
}

func TestSelectionSetOperations(t *testing.T) {
	r, _ := router(t, 5)
	sessions := newFakeSessions("web-01", "web-02", "web-03", "web-04", "web-05")
	sessions.up["web-02"] = false
	sessions.up["web-04"] = false
	r.Attach(sessions)

	if got := r.SelectAll(); got != 5 {
		t.Fatalf("SelectAll() = %d", got)
	}
	if got := r.SelectionCount(); got != 5 {
		t.Fatalf("SelectionCount() = %d", got)
	}

	r.ClearSelection()
	if got := r.SelectConnected(); got != 3 {
		t.Fatalf("SelectConnected() = %d", got)
	}
	if got := strings.Join(r.Selected(), ","); got != "web-01,web-03,web-05" {
		t.Fatalf("Selected() = %q", got)
	}

	if got := r.InvertSelection(); got != 2 {
		t.Fatalf("InvertSelection() = %d", got)
	}
	if got := strings.Join(r.Selected(), ","); got != "web-02,web-04" {
		t.Fatalf("Selected() = %q after inverting", got)
	}

	r.ClearSelection()
	if got := r.SelectDisconnected(); got != 2 {
		t.Fatalf("SelectDisconnected() = %d", got)
	}
	if got := strings.Join(r.Selected(), ","); got != "web-02,web-04" {
		t.Fatalf("Selected() = %q", got)
	}
}

// A pattern selects across the whole run, not only the working set: a selection
// is a statement about machines, and `web-*` must not mean different things at
// different times.
func TestSelectMatchingIgnoresTheWorkingSet(t *testing.T) {
	r, ws := router(t, 40)
	if err := ws.ApplySpec("first 5", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}

	n, err := r.SelectMatching("web-1*")
	if err != nil {
		t.Fatalf("SelectMatching: %v", err)
	}
	if n != 10 {
		t.Fatalf("SelectMatching() matched %d, want 10", n)
	}

	// Only the ones inside the working set are targets, and the panel reports
	// the rest as excluded rather than dropping them from the selection.
	if err := r.SetMode(ModeSelected); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if got := r.Count(); got != 0 {
		t.Fatalf("Count() = %d, want none inside first 5", got)
	}
	if got := len(r.Excluded()); got != 10 {
		t.Fatalf("Excluded() = %d, want 10", got)
	}

	n, err = r.DeselectMatching("web-1*")
	if err != nil {
		t.Fatalf("DeselectMatching: %v", err)
	}
	if n != 10 || r.SelectionCount() != 0 {
		t.Fatalf("DeselectMatching() = %d, %d left selected", n, r.SelectionCount())
	}
}

func TestSelectMatchingRejectsAMalformedGlob(t *testing.T) {
	r, _ := router(t, 3)
	if _, err := r.SelectMatching("web-[01"); err == nil {
		t.Fatal("SelectMatching accepted a malformed glob")
	}
	if _, err := r.DeselectMatching("web-[01"); err == nil {
		t.Fatal("DeselectMatching accepted a malformed glob")
	}
}

// Without a transport nothing is known about liveness, so nothing is selected
// rather than everything.
func TestLivenessSelectionWithoutATransport(t *testing.T) {
	r, _ := router(t, 3)

	if got := r.SelectConnected(); got != 0 {
		t.Fatalf("SelectConnected() = %d without a transport", got)
	}
	if got := r.SelectDisconnected(); got != 0 {
		t.Fatalf("SelectDisconnected() = %d without a transport", got)
	}
}

func TestSelectWhereWithoutAPredicate(t *testing.T) {
	r, _ := router(t, 3)
	r.Select("web-01")
	if got := r.SelectWhere(nil); got != 1 {
		t.Fatalf("SelectWhere(nil) = %d", got)
	}
}

func TestForgetDropsSelectionAndFocus(t *testing.T) {
	ws := workingset.New([]string{"h1", "h2", "h3"})
	r, err := NewRouter(ws)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	r.Toggle("h1")
	r.Toggle("h2")
	r.SetFocus("h2")

	r.Forget("h2")
	if r.IsSelected("h2") {
		t.Fatal("the forgotten host is still selected")
	}
	if r.SelectionCount() != 1 {
		t.Fatalf("SelectionCount() = %d, want 1", r.SelectionCount())
	}
	if r.Focus() != "" {
		t.Fatalf("Focus() = %q after forgetting the focused host", r.Focus())
	}

	// Forgetting a host that was never known changes nothing.
	r.Forget("nope")
	if !r.IsSelected("h1") || r.SelectionCount() != 1 {
		t.Fatal("forgetting an unknown host disturbed the selection")
	}
}

// The visibility limit: a keystroke never reaches a pane the user cannot see.
// It bounds all and selected mode; fleet mode stays the explicit escape hatch.
func TestLimitBoundsAllAndSelected(t *testing.T) {
	r, _ := router(t, 4)

	r.SetLimit([]string{"web-01", "web-02"})
	if got := len(r.Targets()); got != 2 {
		t.Fatalf("ModeAll reached %d hosts under a limit of 2", got)
	}

	r.Select("web-01", "web-03")
	if err := r.SetMode(ModeSelected); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if got := r.Targets(); len(got) != 1 || got[0] != "web-01" {
		t.Fatalf("ModeSelected targets = %v; web-03 is selected but not visible", got)
	}

	if err := r.SetMode(ModeFleet); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	if got := len(r.Targets()); got != 4 {
		t.Fatalf("ModeFleet reached %d hosts; the escape hatch must ignore the limit", got)
	}
}

func TestNilLimitLiftsTheLimit(t *testing.T) {
	r, _ := router(t, 3)

	r.SetLimit([]string{"web-01"})
	if !r.Limited() || len(r.Targets()) != 1 {
		t.Fatalf("limit not in force: limited=%v targets=%v", r.Limited(), r.Targets())
	}

	r.SetLimit(nil)
	if r.Limited() || len(r.Targets()) != 3 {
		t.Fatalf("nil did not lift the limit: limited=%v targets=%v", r.Limited(), r.Targets())
	}

	// An empty, non-nil limit means "nothing is visible", not "no limit".
	r.SetLimit([]string{})
	if !r.Limited() || len(r.Targets()) != 0 {
		t.Fatalf("an empty limit was not enforced: limited=%v targets=%v", r.Limited(), r.Targets())
	}
}

// Issue #133: a target whose writer vanished between Targets() and the write
// is a failed delivery that errors, not a silent success - the count the user
// read must never claim more than the hosts that got the bytes.
func TestSendReportsALostWriter(t *testing.T) {
	r, _ := router(t, 2)
	sessions := newFakeSessions("web-01", "web-02")
	sessions.writerless["web-02"] = true
	r.Attach(sessions)

	d, err := r.Send([]byte("x"))
	if err == nil {
		t.Fatal("Send hid the lost writer")
	}
	if d.Delivered != 1 {
		t.Fatalf("Delivery = %+v", d)
	}
	if strings.Join(d.Failed, ",") != "web-02" {
		t.Fatalf("Failed = %v", d.Failed)
	}
	if sessions.writes["web-01"] != "x" {
		t.Fatalf("web-01 received %q", sessions.writes["web-01"])
	}
}

// The issue-191 behavior: the alt-screen exclusion protects the one stray
// vim among shells. When every reachable host in scope is on the alternate
// screen — a broadcast opened those editors — there is nothing to protect,
// and the keystrokes must flow to all of them.
func TestUniformAltScreenScopeIsNotExcluded(t *testing.T) {
	tests := []struct {
		name     string
		mode     Mode
		selected []string
		want     string
	}{
		{"all reaches the editors it opened", ModeAll, nil, "web-01,web-02,web-03"},
		{"selected reaches its editors too", ModeSelected, []string{"web-01", "web-02"}, "web-01,web-02"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := router(t, 3)
			sessions := newFakeSessions("web-01", "web-02", "web-03")
			for _, id := range []string{"web-01", "web-02", "web-03"} {
				sessions.altScreen[id] = true
			}
			r.Attach(sessions)

			if err := r.SetMode(tt.mode); err != nil {
				t.Fatalf("SetMode: %v", err)
			}
			for _, id := range tt.selected {
				r.Toggle(id)
			}

			if got := strings.Join(r.Targets(), ","); got != tt.want {
				t.Fatalf("Targets() = %q, want %q", got, tt.want)
			}
			if got := r.AltScreenSkipped(); got != nil {
				t.Fatalf("AltScreenSkipped() = %v, want nil - nothing is skipped", got)
			}
		})
	}
}

// A selected sub-scope can be uniformly on the alt screen while the rest of
// the working set is not: the mix is judged against the scope, not the fleet.
func TestUniformityIsJudgedAgainstTheScope(t *testing.T) {
	r, _ := router(t, 3)
	sessions := newFakeSessions("web-01", "web-02", "web-03")
	sessions.altScreen["web-01"] = true
	sessions.altScreen["web-02"] = true
	r.Attach(sessions)

	if err := r.SetMode(ModeSelected); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	r.Toggle("web-01")
	r.Toggle("web-02")

	if got := strings.Join(r.Targets(), ","); got != "web-01,web-02" {
		t.Fatalf("Targets() = %q, want both selected editors", got)
	}
}

// A down host does not make an all-alt-screen scope count as mixed: only
// hosts that can take input weigh in.
func TestDownHostsDoNotBreakUniformity(t *testing.T) {
	r, _ := router(t, 3)
	sessions := newFakeSessions("web-01", "web-02", "web-03")
	sessions.altScreen["web-01"] = true
	sessions.altScreen["web-02"] = true
	sessions.up["web-03"] = false
	r.Attach(sessions)

	if got := strings.Join(r.Targets(), ","); got != "web-01,web-02" {
		t.Fatalf("Targets() = %q, want the two editors", got)
	}
	if got := r.AltScreenSkipped(); got != nil {
		t.Fatalf("AltScreenSkipped() = %v, want nil", got)
	}
}

// One shell among the editors restores the protection: the shell is the only
// target and the editors are named as skipped.
func TestOneShellAmongEditorsRestoresTheExclusion(t *testing.T) {
	r, _ := router(t, 3)
	sessions := newFakeSessions("web-01", "web-02", "web-03")
	sessions.altScreen["web-01"] = true
	sessions.altScreen["web-03"] = true
	r.Attach(sessions)

	if got := strings.Join(r.Targets(), ","); got != "web-02" {
		t.Fatalf("Targets() = %q, want just the shell", got)
	}
	if got := strings.Join(r.AltScreenSkipped(), ","); got != "web-01,web-03" {
		t.Fatalf("AltScreenSkipped() = %q, want the editors", got)
	}
}
