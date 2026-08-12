package program

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// fakeFactory produces connecting fakes and remembers them by identifier.
func fakeFactory() (ssh.Factory, func(id string) *ssh.Fake) {
	var (
		mu    sync.Mutex
		fakes = map[string]*ssh.Fake{}
	)
	factory := func(req ssh.SessionRequest) ssh.Session {
		f := ssh.NewFake(req.ID, req.Host, req.Events)
		f.UseTerminal(req.Terminal)
		mu.Lock()
		fakes[req.ID] = f
		mu.Unlock()
		return f
	}
	return factory, func(id string) *ssh.Fake {
		mu.Lock()
		defer mu.Unlock()
		return fakes[id]
	}
}

// testModel builds a run over fakes: no network, no user ~/.ssh/config, a
// session store in a temp directory.
func testModel(t *testing.T, patterns ...string) (*Model, func(id string) *ssh.Fake) {
	t.Helper()

	factory, lookup := fakeFactory()
	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	m, err := Build(t.Context(), Config{
		Patterns:   patterns,
		Store:      store,
		NewSession: factory,
		Resolver:   &hosts.Resolver{DefaultUser: "deploy"},
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(func() { m.Manager().CloseAll() })
	return m, lookup
}

// drive sends one message through Update and returns the resulting command.
func drive(t *testing.T, m *Model, msg tea.Msg) tea.Cmd {
	t.Helper()
	model, cmd := m.Update(msg)
	if model != m {
		t.Fatalf("Update returned a different model: %T", model)
	}
	return cmd
}

// settle drives a message and every command it spawns to completion - the
// synchronous stand-in for the runtime draining the async work Update started
// (issue #225). Not for messages that re-arm the pump: that command blocks.
func settle(t *testing.T, m *Model, msg tea.Msg) {
	t.Helper()
	runCmd(t, m, drive(t, m, msg))
}

// runCmd executes one command tree and feeds its messages back into Update.
func runCmd(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	if msg == nil {
		return
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			runCmd(t, m, c)
		}
		return
	}
	runCmd(t, m, drive(t, m, msg))
}

func TestInitDialsTheFleet(t *testing.T) {
	m, _ := testModel(t, "srv{1..3}")
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init returned no command; the pump is never armed")
	}
	m.Manager().Wait()

	if got := m.Manager().Counts(); got.Connected != 3 {
		t.Fatalf("Counts() = %+v, want 3 connected", got)
	}
}

func TestPumpConvertsTransportEvents(t *testing.T) {
	m, lookup := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()
	drain(t, m) // the connect events from Start

	lookup("srv1").Emit("hello\r\n")
	msg := m.pump()()
	ev, ok := msg.(fleetEventMsg)
	if !ok {
		t.Fatalf("pump produced %T, want fleetEventMsg", msg)
	}
	if ev.fleetUpdated {
		t.Error("an output event was reported as a fleet update")
	}
	if got := ev.outputs; len(got) != 1 || got[0] != "srv1" {
		t.Errorf("outputs = %v, want [srv1]", got)
	}

	// A fleet event re-arms the pump.
	if cmd := drive(t, m, ev); cmd == nil {
		t.Fatal("delivering a fleet event returned no command; the pump is dead")
	}
}

// TestPumpCoalescesOutputBursts is issue #272: a host echoing a held key
// queued one message per chunk, and the next keystroke waited behind all of
// them. One pump call must take the whole burst.
func TestPumpCoalescesOutputBursts(t *testing.T) {
	m, lookup := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()
	drain(t, m)

	for range 50 {
		lookup("srv1").Emit("x\r\n")
	}
	msg := m.pump()()
	ev, ok := msg.(fleetEventMsg)
	if !ok {
		t.Fatalf("pump produced %T, want fleetEventMsg", msg)
	}
	if got := ev.outputs; len(got) != 1 || got[0] != "srv1" {
		t.Fatalf("outputs = %v, want [srv1]: 50 chunks must coalesce into one hint", got)
	}
	// Nothing is left over: the next pump has to block for new output, so
	// there is no backlog for a keystroke to queue behind.
	select {
	case ev := <-m.Manager().Events():
		t.Fatalf("event left queued after the pump drained: %#v", ev)
	default:
	}
}

// TestPumpKeepsEveryHost checks the deduplication is per host: a batch that
// dropped a host would leave that pane stale until its next chunk.
func TestPumpKeepsEveryHost(t *testing.T) {
	m, lookup := testModel(t, "srv1", "srv2", "srv3")
	m.Init()
	m.Manager().Wait()
	drain(t, m)

	for range 5 {
		for _, id := range []string{"srv1", "srv2", "srv3"} {
			lookup(id).Emit("x\r\n")
		}
	}
	ev, ok := m.pump()().(fleetEventMsg)
	if !ok {
		t.Fatal("pump produced no fleet event")
	}
	got := append([]string(nil), ev.outputs...)
	slices.Sort(got)
	want := []string{"srv1", "srv2", "srv3"}
	if !slices.Equal(got, want) {
		t.Errorf("outputs = %v, want %v", got, want)
	}
}

// TestPumpDeliversStateEventsBehindOutput proves a lifecycle event queued
// behind a burst still reaches the UI - once for the batch, which is all
// FleetUpdatedMsg needs: it makes the UI re-read the whole fleet.
func TestPumpDeliversStateEventsBehindOutput(t *testing.T) {
	m, lookup := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()
	drain(t, m)

	for range 20 {
		lookup("srv1").Emit("x\r\n")
	}
	lookup("srv1").Disconnect(ssh.ErrDisconnected())

	ev, ok := m.pump()().(fleetEventMsg)
	if !ok {
		t.Fatal("pump produced no fleet event")
	}
	if !ev.fleetUpdated {
		t.Error("the state event behind the output burst was lost")
	}
	if got := ev.outputs; len(got) != 1 || got[0] != "srv1" {
		t.Errorf("outputs = %v, want [srv1]", got)
	}

	// The batch reaches the UI: the fleet snapshot must show the failure.
	if cmd := drive(t, m, ev); cmd == nil {
		t.Fatal("delivering the batch returned no command")
	}
	if got := m.mgr.Counts().Failed; got != 1 {
		t.Errorf("Counts().Failed = %d, want 1", got)
	}
}

// drain consumes whatever the transport has reported so far.
func drain(t *testing.T, m *Model) {
	t.Helper()
	for {
		select {
		case <-m.Manager().Events():
		default:
			return
		}
	}
}

func TestReconnectRequestReachesTheManager(t *testing.T) {
	m, lookup := testModel(t, "srv1", "srv2")
	m.Init()
	m.Manager().Wait()

	lookup("srv1").Disconnect(ssh.ErrDisconnected())

	cmd := drive(t, m, ui.ReconnectHostMsg{ID: "srv1"})
	if cmd == nil {
		t.Fatal("a reconnect request produced no command")
	}
	cmd()
	m.Manager().Wait()

	if !m.Manager().Connected("srv1") {
		t.Error("srv1 did not reconnect")
	}
	if !m.Manager().Connected("srv2") {
		t.Error("reconnecting srv1 disturbed srv2")
	}
}

// The clone flow (issue #253): a second, independent session to the same
// resolved host as an existing one, dialled through the same Add path a
// runtime connect uses, so its identifier disambiguation is exactly the
// manager's existing srv1/srv1#2 scheme.
func TestCloneRequestAddsASecondSessionToTheSameHost(t *testing.T) {
	m, _ := testModel(t, "srv1", "srv2")
	m.Init()
	m.Manager().Wait()

	settle(t, m, ui.CloneHostMsg{ID: "srv1"})
	// The clone dials in its own goroutine, like every other session: without
	// waiting for it the assertion below races the dial.
	m.Manager().Wait()

	got := m.Manager().IDs()
	if len(got) != 3 || got[0] != "srv1" || got[1] != "srv2" || got[2] != "srv1#2" {
		t.Fatalf("IDs() = %v, want srv1, srv2, srv1#2", got)
	}
	if !m.Manager().Connected("srv1#2") {
		t.Error("the cloned session did not connect")
	}

	original, ok := m.Manager().Session("srv1")
	if !ok {
		t.Fatal("srv1 disappeared after cloning it")
	}
	clone, ok := m.Manager().Session("srv1#2")
	if !ok {
		t.Fatal("srv1#2 was not added")
	}
	if clone.Host().Addr != original.Host().Addr || clone.Host().User != original.Host().User ||
		clone.Host().Port != original.Host().Port {
		t.Errorf("clone Host() = %+v, want the same Addr/User/Port as %+v", clone.Host(), original.Host())
	}
	if clone.ID() == original.ID() {
		t.Error("the clone shares its identifier with the original")
	}
	if m.ws.Count() != 3 {
		t.Errorf("working set Count() = %d, want 3 after the clone was added", m.ws.Count())
	}
}

func TestCloneRequestForAnUnknownSessionIsANoOp(t *testing.T) {
	m, _ := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	settle(t, m, ui.CloneHostMsg{ID: "does-not-exist"})

	if got := m.Manager().Len(); got != 1 {
		t.Fatalf("Len() = %d, want 1: an unknown session must not be clonable", got)
	}
}

func TestReconnectAllRequestReachesTheManager(t *testing.T) {
	m, lookup := testModel(t, "srv1", "srv2", "srv3")
	m.Init()
	m.Manager().Wait()

	lookup("srv1").Disconnect(ssh.ErrDisconnected()) // failed
	lookup("srv2").Disconnect(nil)                   // closed
	// srv3 stays connected.

	cmd := drive(t, m, ui.ReconnectAllMsg{})
	if cmd == nil {
		t.Fatal("a reconnect-all request produced no command")
	}
	cmd()
	m.Manager().Wait()

	if !m.Manager().Connected("srv1") {
		t.Error("srv1 (failed) did not reconnect")
	}
	if !m.Manager().Connected("srv2") {
		t.Error("srv2 (closed) did not reconnect")
	}
	if !m.Manager().Connected("srv3") {
		t.Error("reconnecting the others disturbed srv3")
	}
}

func TestCloseRequestReachesTheManager(t *testing.T) {
	m, _ := testModel(t, "srv1", "srv2")
	m.Init()
	m.Manager().Wait()

	cmd := drive(t, m, ui.CloseHostMsg{ID: "srv2"})
	if cmd == nil {
		t.Fatal("a close request produced no command")
	}
	cmd()

	if m.Manager().Connected("srv2") {
		t.Error("srv2 is still connected after the close request")
	}
	if !m.Manager().Connected("srv1") {
		t.Error("closing srv2 disturbed srv1")
	}
}

func TestGroupOpenAddsTheSavedHosts(t *testing.T) {
	m, _ := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	if err := m.store.Save(&sessions.Session{
		Version: sessions.FormatVersion,
		Name:    "web",
		Hosts:   []sessions.HostEntry{{Pattern: "web{1..2}"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	settle(t, m, ui.GroupOpenMsg{Name: "web"})
	m.Manager().Wait()

	want := []string{"srv1", "web1", "web2"}
	got := m.Manager().IDs()
	if len(got) != len(want) {
		t.Fatalf("IDs() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("IDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if got := m.ws.Total(); got != 3 {
		t.Errorf("the working set covers %d hosts, want 3", got)
	}
}

func TestGroupOpenOfAMissingNameLeavesTheRunAlone(t *testing.T) {
	m, _ := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	settle(t, m, ui.GroupOpenMsg{Name: "no-such"})
	m.Manager().Wait()

	if got := m.Manager().Len(); got != 1 {
		t.Fatalf("Len() = %d, want the run untouched", got)
	}
}

func TestWindowSizeResizesTheRemotePTYs(t *testing.T) {
	m, lookup := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})

	w, h := lookup("srv1").Size()
	if w <= 0 || h <= 0 {
		t.Fatalf("the remote PTY was never resized: %dx%d", w, h)
	}
	if w >= 120 || h >= 40 {
		t.Errorf("the PTY got the whole terminal (%dx%d); borders and sidebar were not subtracted", w, h)
	}
}

// No remote may believe it has more room than its pane draws: the tiling
// gives the leftover columns and rows to the leftmost and topmost cells, so
// the size every host is told is the smallest pane's. A host told otherwise
// wraps where the pane does not and puts its cursor off the pane (issue #292).
func TestRemotePTYsFitTheSmallestPane(t *testing.T) {
	m, lookup := testModel(t, "srv1", "srv2", "srv3", "srv4")
	m.Init()
	m.Manager().Wait()

	// An odd width and height leave a remainder for the tiling to spread.
	drive(t, m, tea.WindowSizeMsg{Width: 201, Height: 61})

	grid := m.app.Grid()
	if len(grid.Cells) < 2 {
		t.Fatalf("setup: the grid has %d cells", len(grid.Cells))
	}
	uneven := false
	for _, cell := range grid.Cells[1:] {
		if cell.Width != grid.Cells[0].Width || cell.Height != grid.Cells[0].Height {
			uneven = true
		}
	}
	if !uneven {
		t.Skip("this terminal size tiles evenly; nothing to assert")
	}

	w, h := lookup("srv1").Size()
	for _, cell := range grid.Cells {
		if w > cell.Width-2 || h > cell.Height-3 {
			t.Fatalf("the remotes were told %dx%d, but a pane draws %dx%d",
				w, h, cell.Width-2, cell.Height-3)
		}
	}
}

// keyPress synthesises the two chords this file needs: the modifiers are
// spelled out rather than parsed, because a program-level test only has to
// press keys, not model a keyboard.
func keyPress(chord string) tea.KeyPressMsg {
	switch chord {
	case "alt+shift+left":
		return tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModAlt | tea.ModShift}
	case "alt++":
		return tea.KeyPressMsg{Code: '+', Mod: tea.ModAlt}
	default:
		panic("keyPress cannot synthesise " + chord)
	}
}

// The pane size follows the screen mode as well as the terminal (issue #219):
// half mode gives the focused pane more room, and the remote has to be told.
func TestScreenModeResizesTheRemotePTYs(t *testing.T) {
	m, lookup := testModel(t, "srv1", "srv2", "srv3", "srv4")
	m.Init()
	m.Manager().Wait()

	drive(t, m, tea.WindowSizeMsg{Width: 200, Height: 60})
	tiled, tiledHeight := lookup("srv1").Size()

	// Entering a pane, then cycling to half mode: both are focus-or-mode
	// changes with no window size message behind them.
	drive(t, m, keyPress("alt+shift+left"))
	drive(t, m, keyPress("alt++"))

	w, h := lookup("srv1").Size()
	if w <= tiled && h <= tiledHeight {
		t.Fatalf("half mode left the remote at %dx%d, it was %dx%d", w, h, tiled, tiledHeight)
	}
}

func TestViewIsFullScreen(t *testing.T) {
	m, _ := testModel(t, "srv"+strconv.Itoa(1))
	drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if v := m.View(); !v.AltScreen {
		t.Fatal("the view does not request the alternate screen")
	}
}

func TestEmptyRunGrowsByGroupOpen(t *testing.T) {
	m, _ := testModel(t)
	m.Init()
	m.Manager().Wait()

	if got := m.Manager().Len(); got != 0 {
		t.Fatalf("Len() = %d, want an empty run", got)
	}

	if err := m.store.Save(&sessions.Session{
		Version: sessions.FormatVersion,
		Name:    "web",
		Hosts:   []sessions.HostEntry{{Pattern: "web{1..3}"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	settle(t, m, ui.GroupOpenMsg{Name: "web"})
	m.Manager().Wait()

	if got := m.Manager().Counts(); got.Connected != 3 {
		t.Fatalf("Counts() = %+v, want 3 connected after launching into an empty run", got)
	}
	if got := m.ws.Total(); got != 3 {
		t.Errorf("the working set covers %d hosts, want 3", got)
	}
}

func TestHostConnectRequestAddsHosts(t *testing.T) {
	m, _ := testModel(t)
	if m.Manager().Len() != 0 {
		t.Fatalf("setup: Len() = %d", m.Manager().Len())
	}

	settle(t, m, ui.HostConnectMsg{Patterns: []string{"web-{01..02}"}})
	got := m.Manager().SortedIDs()
	if len(got) != 2 || got[0] != "web-01" || got[1] != "web-02" {
		t.Fatalf("IDs() = %v after connecting web-{01..02}", got)
	}
}

// Enter twice on the same candidate must not mint a duplicate web-01-2.
func TestHostConnectRequestSkipsRunningHosts(t *testing.T) {
	m, _ := testModel(t, "web-01")

	settle(t, m, ui.HostConnectMsg{Patterns: []string{"web-01", "db-01"}})
	settle(t, m, ui.HostConnectMsg{Patterns: []string{"web-01"}})

	got := m.Manager().SortedIDs()
	if len(got) != 2 || got[0] != "db-01" || got[1] != "web-01" {
		t.Fatalf("IDs() = %v, want db-01 and web-01 once each", got)
	}
}

func TestHostConnectRequestReportsResolveErrors(t *testing.T) {
	m, _ := testModel(t)

	settle(t, m, ui.HostConnectMsg{Patterns: []string{"web-{01"}})
	if m.Manager().Len() != 0 {
		t.Fatalf("Len() = %d after a failed resolve", m.Manager().Len())
	}
	if m.app.ConnectError() == "" {
		t.Fatal("the resolve error did not reach the UI")
	}
}

func TestRemoveHostRequestDropsThePane(t *testing.T) {
	m, _ := testModel(t, "web-01", "web-02")
	m.Init()
	m.Manager().Wait()

	settle(t, m, ui.RemoveHostMsg{ID: "web-01"})
	got := m.Manager().IDs()
	if len(got) != 1 || got[0] != "web-02" {
		t.Fatalf("IDs() = %v after removing web-01", got)
	}
	if m.ws.Count() != 1 {
		t.Fatalf("working set Count() = %d after the removal", m.ws.Count())
	}
}

// The drift bug: a run started with a pattern and extended at runtime must
// save both. The program tracks the live pattern list and hands it to the UI
// with every change.
func TestRunPatternsFollowRuntimeConnects(t *testing.T) {
	m, _ := testModel(t, "web-{01..02}")

	settle(t, m, ui.HostConnectMsg{Patterns: []string{"db-01"}})
	if got := strings.Join(m.patterns, ","); got != "web-{01..02},db-01" {
		t.Fatalf("patterns = %q after a runtime connect", got)
	}

	// Connecting the same pattern again does not duplicate it.
	settle(t, m, ui.HostConnectMsg{Patterns: []string{"cache-01", "db-01"}})
	if got := strings.Join(m.patterns, ","); got != "web-{01..02},db-01,cache-01" {
		t.Fatalf("patterns = %q after a repeated connect", got)
	}

	// Removing a host drops its exact pattern; a brace pattern stays.
	settle(t, m, ui.RemoveHostMsg{ID: "db-01"})
	settle(t, m, ui.RemoveHostMsg{ID: "web-01"})
	if got := strings.Join(m.patterns, ","); got != "web-{01..02},cache-01" {
		t.Fatalf("patterns = %q after removals", got)
	}
}

// Opening a group twice foregrounds its session instead of dialling twice.
func TestReopeningAGroupDoesNotDuplicateHosts(t *testing.T) {
	m, _ := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	if err := m.store.Save(&sessions.Session{
		Version: sessions.FormatVersion,
		Name:    "web",
		Hosts:   []sessions.HostEntry{{Pattern: "web{1..2}"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	settle(t, m, ui.GroupOpenMsg{Name: "web"})
	m.Manager().Wait()
	settle(t, m, ui.GroupOpenMsg{Name: "web"})
	m.Manager().Wait()

	if got := m.Manager().Len(); got != 3 {
		t.Fatalf("Len() = %d; reopening minted duplicate sessions", got)
	}
	if got := m.app.ActiveSession(); got != "web" {
		t.Fatalf("ActiveSession() = %q after opening the group", got)
	}
	if got := strings.Join(m.app.OpenSessionNames(), ","); got != "adhoc,web" {
		t.Fatalf("open sessions = %q", got)
	}
}

// The store read and the resolution run in the returned Cmd, not in Update
// (issue #225): until the command runs, the fleet is untouched.
func TestGroupOpenResolvesInTheCommandNotInUpdate(t *testing.T) {
	m, _ := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	if err := m.store.Save(&sessions.Session{
		Version: sessions.FormatVersion,
		Name:    "web",
		Hosts:   []sessions.HostEntry{{Pattern: "web{1..2}"}},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cmd := drive(t, m, ui.GroupOpenMsg{Name: "web"})
	if cmd == nil {
		t.Fatal("opening a group produced no command")
	}
	if got := m.Manager().Len(); got != 1 {
		t.Fatalf("Update touched the fleet: Len() = %d, want 1", got)
	}

	runCmd(t, m, cmd)
	m.Manager().Wait()
	if got := m.Manager().Len(); got != 3 {
		t.Fatalf("Len() = %d after the command ran, want 3", got)
	}
}
