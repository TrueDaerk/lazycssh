package program

import (
	"strconv"
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
		f.UseScrollback(req.Scrollback)
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

func TestSessionLaunchAddsTheSavedHosts(t *testing.T) {
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

	drive(t, m, ui.SessionLaunchMsg{Name: "web", Merge: true})
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

func TestSessionLaunchOfAMissingNameLeavesTheRunAlone(t *testing.T) {
	m, _ := testModel(t, "srv1")
	m.Init()
	m.Manager().Wait()

	drive(t, m, ui.SessionLaunchMsg{Name: "no-such"})
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

func TestViewIsFullScreen(t *testing.T) {
	m, _ := testModel(t, "srv"+strconv.Itoa(1))
	drive(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
	if v := m.View(); !v.AltScreen {
		t.Fatal("the view does not request the alternate screen")
	}
}
