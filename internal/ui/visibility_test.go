package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// The acceptance criterion: 8 hosts, 2 down - ctrl+a shows 6 panes and
// broadcast all reaches 6.
func TestConnectedOnlyHidesTheDownHosts(t *testing.T) {
	a, fleet, router, _ := statusApp(t, "web-01", "web-02", "web-03")
	router.Attach(fleetSessions{fleet})
	fleet.connect(t, "web-01")
	fleet.connect(t, "web-03")
	fleet.fail(t, "web-02")
	a = syncFleet(t, a)

	a = pressKey(t, a, "ctrl+a")
	if !a.ConnectedOnly() {
		t.Fatal("ctrl+a did not switch the filter on")
	}
	if got := strings.Join(a.hostIDs(), ","); got != "web-01,web-03" {
		t.Fatalf("visible hosts = %q with the filter on", got)
	}
	if got := strings.Join(router.Targets(), ","); got != "web-01,web-03" {
		t.Fatalf("Targets() = %q; broadcast must follow the visible set", got)
	}

	a = pressKey(t, a, "ctrl+a")
	if a.ConnectedOnly() {
		t.Fatal("ctrl+a did not switch the filter off again")
	}
	if got := len(a.hostIDs()); got != 3 {
		t.Fatalf("%d hosts visible after clearing the filter, want 3", got)
	}
}

// A host reconnecting while the filter is on reappears without a keypress:
// the visible list is computed from live state, and the limit follows on the
// fleet event.
func TestReconnectingHostReappearsUnderTheFilter(t *testing.T) {
	a, fleet, router, _ := statusApp(t, "web-01", "web-02")
	router.Attach(fleetSessions{fleet})
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)

	a = pressKey(t, a, "ctrl+a")
	if got := strings.Join(a.hostIDs(), ","); got != "web-01" {
		t.Fatalf("visible hosts = %q before the reconnect", got)
	}

	fleet.connect(t, "web-02")
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if got := strings.Join(a.hostIDs(), ","); got != "web-01,web-02" {
		t.Fatalf("visible hosts = %q after the reconnect", got)
	}
	if got := strings.Join(router.Targets(), ","); got != "web-01,web-02" {
		t.Fatalf("Targets() = %q after the reconnect", got)
	}
}

// The narrowing must be unmissable for as long as it is in force.
func TestConnectedOnlyIsOnTheStatusBar(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	if got := plain(a.View().Content); !strings.Contains(got, "CONNECTED HOSTS ONLY") {
		t.Fatalf("the filter is not on the status bar:\n%s", got)
	}

	a = pressKey(t, a, "ctrl+a")
	if got := plain(a.View().Content); strings.Contains(got, "CONNECTED HOSTS ONLY") {
		t.Fatalf("the flag survived clearing the filter:\n%s", got)
	}
}

// Every host down under the filter must not read as an empty run.
func TestFilterHidingEveryPaneSaysSo(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")

	a = pressKey(t, a, "ctrl+a")
	if got := plain(a.View().Content); !strings.Contains(got, "no connected hosts") {
		t.Fatalf("an all-hidden grid does not say why:\n%s", got)
	}
}

// While typing into a pane, ctrl+a is readline start-of-line: it belongs to
// the host, and the filter must not flip.
func TestCtrlAIsForwardedWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	a = pressKey(t, a, "ctrl+a")

	if a.ConnectedOnly() {
		t.Fatal("ctrl+a flipped the filter while typing")
	}
	if got := fleet.sessions["web-01"].Written(); got != "\x01" {
		t.Fatalf("the host received %q, want the raw ctrl+a byte", got)
	}
}

// Toggling emits GridChangedMsg so the program can resize the PTYs.
func TestToggleAsksForAResize(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	fleet.connect(t, "web-01")

	model, cmd := a.Update(keyMsgFor(t, "ctrl+a"))
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("toggling produced no command")
	}
	if _, ok := cmd().(GridChangedMsg); !ok {
		t.Fatalf("toggling produced a %T", cmd())
	}
}

// The filter composes with sessions: it narrows the foreground session, not
// the fleet.
func TestFilterAppliesToTheForegroundSession(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "db-01")
	fleet.connect(t, "web-01")
	fleet.connect(t, "db-01")

	model, _ := a.Update(SessionOpenedMsg{Name: "front", Hosts: []string{"web-01", "web-02"}})
	a = model.(App)
	a = pressKey(t, a, "ctrl+a")

	if got := strings.Join(a.hostIDs(), ","); got != "web-01" {
		t.Fatalf("visible hosts = %q; db-01 is connected but not in the session", got)
	}
}

// splitApp is ten connected hosts with the router attached.
func splitApp(t *testing.T) (App, *fakeFleet, Selector) {
	t.Helper()
	names := make([]string, 10)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a, fleet, router, _ := statusApp(t, names...)
	router.Attach(fleetSessions{fleet})
	for _, id := range names {
		fleet.connect(t, id)
	}
	return a, fleet, router
}

// applySplitSize types a size into the ctrl+s prompt and applies it.
func applySplitSize(t *testing.T, a App, size string) App {
	t.Helper()
	a = pressKey(t, a, "ctrl+s")
	a = typeInto(t, a, size)
	return pressKey(t, a, "enter")
}

// The acceptance criterion: 10 hosts, ctrl+s 5 - the grid shows the first 5
// and broadcast all reaches 5.
func TestSplitShowsTheFirstChunk(t *testing.T) {
	a, _, router := splitApp(t)

	a = applySplitSize(t, a, "5")
	if got := strings.Join(a.hostIDs(), ","); got != "web-01,web-02,web-03,web-04,web-05" {
		t.Fatalf("visible hosts = %q after splitting by 5", got)
	}
	if got := len(router.(*broadcast.Router).Targets()); got != 5 {
		t.Fatalf("broadcast reaches %d hosts, want the visible 5", got)
	}
	if got := plain(a.View().Content); !strings.Contains(got, "SPLIT 1/2 (5 hosts)") {
		t.Fatalf("the split is not on the status bar:\n%s", got)
	}
}

// ctrl+right shows the next chunk and wraps at the ends (issue #147): the
// last chunk leads back to the first, and stepping back from the first lands
// on the last. The status bar's SPLIT indicator says where the wrap landed.
func TestSplitStepsBetweenChunksAndWraps(t *testing.T) {
	a, _, _ := splitApp(t)
	a = applySplitSize(t, a, "5")

	a = pressKey(t, a, "ctrl+right")
	if got := strings.Join(a.hostIDs(), ","); got != "web-06,web-07,web-08,web-09,web-10" {
		t.Fatalf("visible hosts = %q after ctrl+right", got)
	}
	if got := plain(a.View().Content); !strings.Contains(got, "SPLIT 2/2") {
		t.Fatalf("the indicator did not follow:\n%s", got)
	}

	a = pressKey(t, a, "ctrl+right") // past the last chunk: wrap to the first
	if got := a.hostIDs()[0]; got != "web-01" {
		t.Fatalf("visible hosts start at %q after wrapping forward", got)
	}
	if got := plain(a.View().Content); !strings.Contains(got, "SPLIT 1/2") {
		t.Fatalf("the indicator did not follow the wrap:\n%s", got)
	}

	a = pressKey(t, a, "ctrl+left") // before the first chunk: wrap to the last
	if got := a.hostIDs()[0]; got != "web-06" {
		t.Fatalf("visible hosts start at %q after wrapping backward", got)
	}
}

// Empty input or 0 clears the split and restores the full grid and scope.
func TestSplitClears(t *testing.T) {
	a, _, router := splitApp(t)
	a = applySplitSize(t, a, "5")

	a = applySplitSize(t, a, "")
	if a.SplitSize() != 0 {
		t.Fatalf("SplitSize() = %d after an empty input", a.SplitSize())
	}
	if got := len(a.hostIDs()); got != 10 {
		t.Fatalf("%d hosts visible after clearing, want 10", got)
	}
	if got := len(router.(*broadcast.Router).Targets()); got != 10 {
		t.Fatalf("broadcast reaches %d hosts after clearing, want 10", got)
	}

	a = applySplitSize(t, a, "5")
	a = applySplitSize(t, a, "0")
	if a.SplitSize() != 0 {
		t.Fatalf("SplitSize() = %d after 0", a.SplitSize())
	}
}

func TestSplitEscKeepsTheSplit(t *testing.T) {
	a, _, _ := splitApp(t)
	a = applySplitSize(t, a, "5")

	a = pressKey(t, a, "ctrl+s")
	a = pressKey(t, a, "esc")
	if a.SplitSize() != 5 {
		t.Fatalf("SplitSize() = %d after esc, want the split kept", a.SplitSize())
	}
}

// The split composes with the connected-only filter: chunks are cut from the
// filtered list.
func TestSplitComposesWithConnectedOnly(t *testing.T) {
	a, fleet, _ := splitApp(t)
	fleet.sessions["web-02"].Disconnect(ssh.ErrDisconnected())
	a = syncFleet(t, a)

	a = pressKey(t, a, "ctrl+a")
	a = applySplitSize(t, a, "5")
	if got := strings.Join(a.hostIDs(), ","); got != "web-01,web-03,web-04,web-05,web-06" {
		t.Fatalf("visible hosts = %q with filter and split", got)
	}
	if got := plain(a.View().Content); !strings.Contains(got, "SPLIT 1/2") {
		t.Fatalf("chunk count ignores the filter:\n%s", got)
	}
}

// A chunk past the end - hosts left the run - clamps to the last one instead
// of rendering an empty grid.
func TestSplitChunkClampsWhenHostsLeave(t *testing.T) {
	a, fleet, _ := splitApp(t)
	a = applySplitSize(t, a, "4") // chunks: 4, 4, 2
	a = pressKey(t, a, "ctrl+right")
	a = pressKey(t, a, "ctrl+right") // chunk 3: web-09, web-10

	fleet.ids = fleet.ids[:4]
	model, _ := a.Update(HostsChangedMsg{Hosts: fleet.IDs()})
	a = model.(App)

	if got := len(a.hostIDs()); got == 0 {
		t.Fatal("a vanished chunk rendered an empty grid")
	}
}

// While typing, ctrl+s is flow control for the host, not a prompt.
func TestCtrlSIsForwardedWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	a = pressKey(t, a, "ctrl+s")

	if a.splitInput.Focused() {
		t.Fatal("ctrl+s opened the split prompt while typing")
	}
	if got := fleet.sessions["web-01"].Written(); got != "\x13" {
		t.Fatalf("the host received %q, want the raw ctrl+s byte", got)
	}
}

// flakySession wraps a fake session and, once armed, answers a bounded number
// of State() queries with StateConnected before flipping to StateClosed - the
// shape of a session goroutine disconnecting between two reads of the same
// render.
type flakySession struct {
	ssh.Session
	fleet *flakyFleet
}

func (s flakySession) State() ssh.State {
	s.fleet.queries++
	if s.fleet.queries > s.fleet.flipAfter {
		return ssh.StateClosed
	}
	return ssh.StateConnected
}

// flakyFleet is a fakeFleet whose named host changes state mid-render once
// armed. Counting starts at arm time so the test controls exactly which
// hostIDs() computation sees the shrink.
type flakyFleet struct {
	*fakeFleet
	flaky     string
	armed     bool
	queries   int
	flipAfter int
}

func (f *flakyFleet) Session(id string) (ssh.Session, bool) {
	s, ok := f.fakeFleet.Session(id)
	if !ok || id != f.flaky || !f.armed {
		return s, ok
	}
	return flakySession{Session: s, fleet: f}, true
}

// The regression for issue #135: with the connected-only filter on, a host
// disconnecting between two hostIDs() computations of the same View call
// shrank the list under an index that was guarded against the longer one,
// and renderPane panicked with index out of range. Since issue #136 the
// render path reads only the model's snapshot, so the flip cannot even be
// observed mid-frame - the flaky fleet stays as the tripwire proving it.
func TestViewSurvivesHostListShrinkingMidRender(t *testing.T) {
	a, fleet, router, _ := statusApp(t, "web-01", "web-02", "web-03")
	router.Attach(fleetSessions{fleet})
	for _, id := range fleet.IDs() {
		fleet.connect(t, id)
	}
	a = syncFleet(t, a)

	flaky := &flakyFleet{fakeFleet: fleet, flaky: "web-03", flipAfter: 2}
	a.cfg.Fleet = flaky

	a = pressKey(t, a, "ctrl+a") // connected-only: the list follows liveness
	a.fullScreen = true
	a.paneIndex = 2 // the pane whose host is about to vanish

	flaky.armed = true
	if got := a.View().Content; got == "" {
		t.Fatal("View rendered nothing")
	}
}

// The acceptance criterion of issue #147: navigation is page-major inside the
// chunk, then the chunk, wrapping at both ends. Three hosts split by 2 on a
// one-pane terminal: chunk 1 holds two pages, chunk 2 one.
func TestStepViewPagesInsideTheChunkThenSteps(t *testing.T) {
	a := resize(t, fleetApp(t, 3), 60, 14)
	a = pressKey(t, a, "ctrl+]")
	a = applySplitSize(t, a, "2")

	if got := a.FocusedHost(); got != "web-01" {
		t.Fatalf("setup: FocusedHost() = %q", got)
	}

	a = pressKey(t, a, "ctrl+right") // page 2 of chunk 1
	if got := a.FocusedHost(); got != "web-02" {
		t.Fatalf("FocusedHost() = %q, want the chunk's second page", got)
	}

	a = pressKey(t, a, "ctrl+right") // chunk 2, page 1
	if got := a.FocusedHost(); got != "web-03" {
		t.Fatalf("FocusedHost() = %q, want the next chunk", got)
	}

	a = pressKey(t, a, "ctrl+right") // past the end: wrap to chunk 1, page 1
	if got := a.FocusedHost(); got != "web-01" {
		t.Fatalf("FocusedHost() = %q after wrapping forward", got)
	}

	a = pressKey(t, a, "ctrl+left") // before the start: last chunk, last page
	if got := a.FocusedHost(); got != "web-03" {
		t.Fatalf("FocusedHost() = %q after wrapping backward", got)
	}

	a = pressKey(t, a, "ctrl+left") // back into chunk 1, on its last page
	if got := a.FocusedHost(); got != "web-02" {
		t.Fatalf("FocusedHost() = %q stepping back into the first chunk", got)
	}
}
