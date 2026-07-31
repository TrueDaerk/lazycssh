package ui

import (
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/term"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// fakeFleet is a Fleet backed by ssh.Fake sessions, so the panels can be driven
// through real state transitions without a network.
type fakeFleet struct {
	ids      []string
	sessions map[string]*ssh.Fake
}

func newFakeFleet(names ...string) *fakeFleet {
	f := &fakeFleet{sessions: make(map[string]*ssh.Fake, len(names))}
	for _, name := range names {
		f.ids = append(f.ids, name)
		f.sessions[name] = ssh.NewFake(name, hosts.Host{Alias: name, Addr: name}, nil)
	}
	return f
}

func (f *fakeFleet) IDs() []string { return append([]string(nil), f.ids...) }

func (f *fakeFleet) SendKey(id string, k term.KeyEvent) bool {
	s, ok := f.sessions[id]
	if !ok {
		return false
	}
	return s.SendKey(k)
}

func (f *fakeFleet) Session(id string) (ssh.Session, bool) {
	s, ok := f.sessions[id]
	if !ok {
		return nil, false
	}
	return s, true
}

func (f *fakeFleet) Counts() ssh.Counts {
	var c ssh.Counts
	for _, id := range f.ids {
		c.Total++
		switch f.sessions[id].State() {
		case ssh.StateConnected:
			c.Connected++
		case ssh.StateFailed:
			c.Failed++
		case ssh.StateClosed:
			c.Closed++
		default:
			c.Pending++
		}
	}
	return c
}

// syncFleet delivers the fleet event a state change would have produced, so
// the model's snapshot catches up with the fakes - the render path reads the
// snapshot, never the live sessions (issue #136).
func syncFleet(t *testing.T, a App) App {
	t.Helper()
	model, _ := a.Update(FleetUpdatedMsg{})
	return model.(App)
}

// connect drives a fake session to connected.
func (f *fakeFleet) connect(t *testing.T, id string) {
	t.Helper()
	if err := f.sessions[id].Start(t.Context()); err != nil {
		t.Fatalf("Start %s: %v", id, err)
	}
}

// fail drives a fake session to failed.
func (f *fakeFleet) fail(t *testing.T, id string) {
	t.Helper()
	f.connect(t, id)
	f.sessions[id].Disconnect(ssh.ErrDisconnected())
}

// statusApp builds an app with a live fleet, router and working set, showing
// the Status panel.
func statusApp(t *testing.T, names ...string) (App, *fakeFleet, *broadcast.Router, *workingset.Manager) {
	t.Helper()

	fleet := newFakeFleet(names...)
	ws := workingset.New(fleet.IDs())
	router, err := broadcast.NewRouter(ws)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	a := resize(t, NewApp(Config{
		SessionName: "prod-web",
		Fleet:       fleet,
		Targets:     router,
		WorkingSet:  ws,
		Panes:       fleet,
		Theme:       Options{Dark: true},
	}), 200, 60)

	return a, fleet, router, ws
}

// The acceptance criterion: the counts follow the fleet through its events -
// a state change plus its FleetUpdatedMsg is on screen, with no keypress.
func TestStatusPanelCountsAreLive(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03")

	if got := plain(a.View().Content); !strings.Contains(got, "hosts: 0/3 up") {
		t.Fatalf("before connecting:\n%s", got)
	}

	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")

	// The fleet event carries the change into the model; the panel renders
	// from that snapshot (issue #136).
	a = syncFleet(t, a)
	if got := plain(a.View().Content); !strings.Contains(got, "hosts: 2/3 up") {
		t.Fatalf("after connecting two hosts:\n%s", got)
	}

	fleet.fail(t, "web-03")
	a = syncFleet(t, a)
	view := plain(a.View().Content)
	if !strings.Contains(view, "hosts: 2/3 up") || !strings.Contains(view, "1 failed") {
		t.Fatalf("after a failure:\n%s", view)
	}
}

func TestFleetUpdateRedraws(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	before := a.View().Content

	fleet.connect(t, "web-01")
	model, cmd := a.Update(FleetUpdatedMsg{})
	if cmd != nil {
		t.Fatal("FleetUpdatedMsg produced a command")
	}
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if next.View().Content == before {
		t.Fatal("a connected host changed nothing on screen")
	}
}

func TestStatusPanelShowsTheBroadcastScope(t *testing.T) {
	a, _, router, ws := statusApp(t, "web-01", "web-02", "web-03", "web-04")

	if got := plain(a.View().Content); !strings.Contains(got, "BROADCAST all (4 hosts)") {
		t.Fatalf("default scope:\n%s", got)
	}

	if err := ws.ApplySpec("first 2", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, "BROADCAST set:first-2 (2 hosts)") {
		t.Fatalf("narrowed scope:\n%s", view)
	}
	if !strings.Contains(view, "set: first-2 (2/4 hosts)") {
		t.Fatalf("working set line:\n%s", view)
	}

	if err := router.SetMode(broadcast.ModeFleet); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	view = plain(a.View().Content)
	if !strings.Contains(view, "BROADCAST EVERY HOST (4 hosts)") {
		t.Fatalf("fleet scope:\n%s", view)
	}
	if !strings.Contains(view, "BROADCASTING TO EVERY HOST") {
		t.Fatalf("the fleet scope is not flagged:\n%s", view)
	}
}

func TestStatusPanelWithoutARouter(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	view := plain(a.View().Content)
	if !strings.Contains(view, "BROADCAST (not started)") {
		t.Fatalf("a run with no router invented a target count:\n%s", view)
	}
	if !strings.Contains(view, "session: (ad hoc)") {
		t.Fatalf("an ad hoc run does not say so:\n%s", view)
	}
}

// The acceptance criterion: an active insecure flag is rendered in a warning
// style and cannot be scrolled out of view.
func TestInsecureFlagIsAlwaysVisible(t *testing.T) {
	fleet := newFakeFleet("web-01")
	a := resize(t, NewApp(Config{
		Fleet:    fleet,
		Insecure: true,
		Logging:  true,
		Theme:    Options{Dark: true},
	}), 120, 40)

	// On the Status panel.
	view := plain(a.View().Content)
	for _, want := range []string{"HOST KEYS UNVERIFIED", "SESSION LOGGING ON"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the Status panel does not show %q:\n%s", want, view)
		}
	}

	// And on every other panel, because the status bar carries it too: the
	// user cannot scroll or switch away from a weakened default.
	for i := range Panels() {
		a = pressKey(t, a, string(rune('1'+i)))
		if !strings.Contains(plain(a.View().Content), "HOST KEYS UNVERIFIED") {
			t.Fatalf("panel %d hides the insecure flag:\n%s", i+1, plain(a.View().Content))
		}
	}

	// It is rendered in the warning style, not as plain text.
	styled := a.View().Content
	marker := a.Theme().StatusInsecure.Render("HOST KEYS UNVERIFIED")
	if !strings.Contains(styled, marker) {
		t.Fatal("the insecure flag is not rendered in the warning style")
	}
}

func TestFlagsAreAbsentWhenDefaultsHold(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	view := plain(a.View().Content)
	for _, unwanted := range []string{"HOST KEYS UNVERIFIED", "SESSION LOGGING ON", "EVERY HOST"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("a default run rendered %q:\n%s", unwanted, view)
		}
	}
}

// A roomy sidebar previews the unselected panels: selecting another panel must
// not blank the ones that lost the selection (issue #186).
func TestUnselectedPanelsKeepAPreview(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")

	if !strings.Contains(plain(a.View().Content), "session: prod-web") {
		t.Fatalf("the selected panel is not open:\n%s", plain(a.View().Content))
	}

	a = pressKey(t, a, "2")
	view := plain(a.View().Content)
	if !strings.Contains(view, "1 web-01") {
		t.Fatalf("the selected panel says nothing:\n%s", view)
	}
	if !strings.Contains(view, "session: prod-web") {
		t.Fatalf("the unselected Status panel lost its preview:\n%s", view)
	}
}

// A sidebar too short even for the collapsed boxes falls back to bare titles:
// the preview is a bonus, never a squeeze.
func TestTightSidebarDropsThePreviews(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	// Height 10 leaves a 9-row sidebar: below the four collapsed boxes, so the
	// unselected panels are one-line titles with no body at all.
	a = resize(t, a, 120, 10)

	a = pressKey(t, a, "2")
	view := plain(a.View().Content)
	if strings.Contains(view, "session: prod-web") {
		t.Fatalf("a bare-title panel still shows content:\n%s", view)
	}
}

func TestPanelBodyForEveryPanel(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	for _, panel := range Panels() {
		if body := a.panelBody(panel, 30, 10); body == "" {
			t.Fatalf("panel %v renders nothing", panel)
		}
	}
}

// Without a transport the panels still render, which is what lets every other
// view be tested without one.
func TestStatusPanelWithoutAFleet(t *testing.T) {
	a := resize(t, NewApp(Config{
		Hosts: []string{"h1", "h2"},
		Theme: Options{Dark: true},
	}), 120, 40)

	if got := plain(a.View().Content); !strings.Contains(got, "hosts: 0/2 up") {
		t.Fatalf("without a fleet:\n%s", got)
	}
	if got := a.state("h1"); got != ssh.StatePending {
		t.Fatalf("state() = %v without a fleet", got)
	}
	if got := a.state("nope"); got != ssh.StatePending {
		t.Fatalf("state() = %v for an unknown host", got)
	}
}

func TestStateReadsTheFleet(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")

	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	if got := a.state("web-01"); got != ssh.StateConnected {
		t.Fatalf("state(web-01) = %v", got)
	}
	if got := a.state("web-02"); got != ssh.StatePending {
		t.Fatalf("state(web-02) = %v", got)
	}
	if got := a.state("web-99"); got != ssh.StatePending {
		t.Fatalf("state of a host that is not in the fleet = %v", got)
	}
}

// The fleet is the source of truth for the host list when there is one.
func TestHostListComesFromTheFleet(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01", "web-02", "web-03")
	if got := strings.Join(a.hostIDs(), ","); got != "web-01,web-02,web-03" {
		t.Fatalf("hostIDs() = %q", got)
	}
}

// One keystroke switches the broadcast scope, and the status bar says so
// immediately - switching to single is what a sudo prompt needs.
func TestBroadcastModeKeys(t *testing.T) {
	a, _, router, _ := statusApp(t, "web-01", "web-02", "web-03")

	tests := []struct {
		key  string
		want broadcast.Mode
	}{
		{"B", broadcast.ModeSelected},
		{"s", broadcast.ModeSingle},
		{"b", broadcast.ModeAll},
	}
	for _, tc := range tests {
		a = pressKey(t, a, tc.key)
		if router.Mode() != tc.want {
			t.Fatalf("%q switched to %v, want %v", tc.key, router.Mode(), tc.want)
		}
		if !strings.Contains(plain(a.View().Content), "BROADCAST "+scopeLabel(tc.want)) {
			t.Fatalf("%q did not reach the status bar:\n%s", tc.key, plain(a.View().Content))
		}
	}
}

// scopeLabel is how the status bar names a mode.
func scopeLabel(m broadcast.Mode) string {
	switch m {
	case broadcast.ModeSelected:
		return "selected"
	case broadcast.ModeSingle:
		return "single"
	case broadcast.ModeFleet:
		return "EVERY HOST"
	default:
		return "all"
	}
}

// Single mode sends to the pane the user is looking at.
func TestSingleModeFollowsTheFocusedPane(t *testing.T) {
	a, _, router, _ := statusApp(t, "web-01", "web-02", "web-03")

	a = focusGrid(t, a)
	a = pressKey(t, a, "alt+shift+right") // move to web-02
	a = pressKey(t, a, "ctrl+]")          // s is an app-level command
	a = pressKey(t, a, "s")

	if router.Mode() != broadcast.ModeSingle {
		t.Fatalf("Mode() = %v", router.Mode())
	}
	if router.Focus() != "web-02" {
		t.Fatalf("Focus() = %q, want the focused pane", router.Focus())
	}
	if got := strings.Join(router.Targets(), ","); got != "web-02" {
		t.Fatalf("Targets() = %q", got)
	}
}

// The mode that ignores the working set is not on a letter, so it cannot be
// reached by a fumbled keystroke.
func TestFleetModeNeedsTheChord(t *testing.T) {
	a, _, router, _ := statusApp(t, "web-01", "web-02")

	for _, k := range []string{"f", "F", "e"} {
		a = pressKey(t, a, k)
		if router.Mode() == broadcast.ModeFleet {
			t.Fatalf("%q reached the fleet mode", k)
		}
	}

	model, _ := a.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl | tea.ModAlt})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if router.Mode() != broadcast.ModeFleet {
		t.Fatalf("the chord did not reach the fleet mode: %v", router.Mode())
	}
	if !strings.Contains(plain(next.View().Content), "BROADCASTING TO EVERY HOST") {
		t.Fatalf("the fleet mode is not flagged:\n%s", plain(next.View().Content))
	}
}

func TestBroadcastKeysWithoutARouter(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	for _, k := range []string{"b", "B", "s"} {
		a = pressKey(t, a, k) // must not panic
	}
	if a.BroadcastMode() != broadcast.ModeAll {
		t.Fatalf("BroadcastMode() = %v without a router", a.BroadcastMode())
	}
}

// Connected and Writer make the fake fleet a broadcast.Sessions as well as a
// Fleet, so the router can be attached in tests exactly as it is in a run.
func (f *fakeFleet) Connected(id string) bool {
	s, ok := f.sessions[id]
	return ok && s.State() == ssh.StateConnected
}

func (f *fakeFleet) AltScreen(id string) bool {
	s, ok := f.sessions[id]
	return ok && s.State() == ssh.StateConnected && s.Terminal().IsAltScreen()
}

func (f *fakeFleet) Writer(id string) (io.Writer, bool) {
	s, ok := f.sessions[id]
	if !ok || s.State() != ssh.StateConnected {
		return nil, false
	}
	return s, true
}

// The Status panel's first line answers its question directly: where do my
// keys go right now.
func TestStatusPanelSaysWhereKeysGo(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")

	if got := plain(a.View().Content); !strings.Contains(got, "keys go to: lazycssh") {
		t.Fatalf("app level:\n%s", got)
	}

	a = focusGrid(t, a)
	if got := plain(a.statusPanel(60)); !strings.Contains(got, "keys go to: web-01") {
		t.Fatalf("while typing:\n%s", got)
	}

	a = pressKey(t, a, "ctrl+]")
	a = pressKey(t, a, "5")
	if got := plain(a.statusPanel(60)); !strings.Contains(got, "keys go to: the broadcast targets") {
		t.Fatalf("while broadcasting:\n%s", got)
	}
}
