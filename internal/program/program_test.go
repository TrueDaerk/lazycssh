package program

import (
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
	out, ok := ev.inner.(ui.SessionOutputMsg)
	if !ok {
		t.Fatalf("output arrived as %T, want ui.SessionOutputMsg", ev.inner)
	}
	if out.ID != "srv1" {
		t.Errorf("SessionOutputMsg.ID = %q, want srv1", out.ID)
	}

	// A fleet event re-arms the pump.
	if cmd := drive(t, m, ev); cmd == nil {
		t.Fatal("delivering a fleet event returned no command; the pump is dead")
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
