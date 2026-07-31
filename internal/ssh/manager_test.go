package ssh

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// fakeFleet builds n hosts named srv1..srvN.
func fakeFleet(n int) []hosts.Host {
	fleet := make([]hosts.Host, n)
	for i := range fleet {
		alias := "srv" + strconv.Itoa(i+1)
		fleet[i] = hosts.Host{Alias: alias, Addr: alias + ".example.com", User: "deploy", Port: 22}
	}
	return fleet
}

// fakeFactory returns a factory producing fakes, and a lookup for the tests to
// drive them. script is called for each fake before it is returned.
func fakeFactory(script func(host hosts.Host, f *Fake)) (Factory, func(id string) *Fake) {
	var (
		mu    sync.Mutex
		fakes = map[string]*Fake{}
	)

	factory := func(req SessionRequest) Session {
		f := NewFake(req.ID, req.Host, req.Events)
		f.UseTerminal(req.Terminal)
		f.Banner = "welcome to " + req.Host.Alias + "\r\n"
		if script != nil {
			script(req.Host, f)
		}
		mu.Lock()
		fakes[req.ID] = f
		mu.Unlock()
		return f
	}

	return factory, func(id string) *Fake {
		mu.Lock()
		defer mu.Unlock()
		return fakes[id]
	}
}

func newTestManager(t *testing.T, fleet []hosts.Host, script func(hosts.Host, *Fake)) (*Manager, func(string) *Fake) {
	t.Helper()

	factory, lookup := fakeFactory(script)
	m, err := NewManager(ManagerConfig{Hosts: fleet, NewSession: factory})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.CloseAll() })
	return m, lookup
}

func TestManagerNeedsAFactory(t *testing.T) {
	if _, err := NewManager(ManagerConfig{Hosts: fakeFleet(1)}); err == nil {
		t.Error("NewManager without a factory returned no error")
	}
}

func TestManagerConnectsEveryHost(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(40), nil)

	if got := m.Len(); got != 40 {
		t.Fatalf("Len() = %d, want 40", got)
	}

	m.Start(t.Context())
	m.Wait()

	counts := m.Counts()
	if counts.Connected != 40 {
		t.Errorf("Counts() = %+v, want all 40 connected", counts)
	}
	if got, want := counts.String(), "40/40 up"; got != want {
		t.Errorf("Counts().String() = %q, want %q", got, want)
	}
}

func TestManagerKeepsHostOrder(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(5), nil)

	want := []string{"srv1", "srv2", "srv3", "srv4", "srv5"}
	got := m.IDs()
	if len(got) != len(want) {
		t.Fatalf("IDs() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("IDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestManagerDisambiguatesRepeatedAliases(t *testing.T) {
	fleet := []hosts.Host{
		{Alias: "srv1", Addr: "srv1.example.com"},
		{Alias: "srv1", Addr: "srv1.example.com"},
		{Alias: "srv1", Addr: "srv1.example.com"},
	}
	m, _ := newTestManager(t, fleet, nil)

	want := []string{"srv1", "srv1#2", "srv1#3"}
	got := m.IDs()
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("IDs()[%d] = %q, want %q: every session needs its own identity", i, got[i], want[i])
		}
	}
}

func TestManagerFailuresAreIsolated(t *testing.T) {
	fleet := fakeFleet(10)
	m, lookup := newTestManager(t, fleet, func(h hosts.Host, f *Fake) {
		switch h.Alias {
		case "srv3":
			f.DialErr = errors.New("connection refused")
		case "srv7":
			f.AuthErr = errors.New("permission denied")
		}
	})

	m.Start(t.Context())
	m.Wait()

	counts := m.Counts()
	if counts.Connected != 8 || counts.Failed != 2 {
		t.Errorf("Counts() = %+v, want 8 connected and 2 failed", counts)
	}

	failed := m.ByState(StateFailed)
	if len(failed) != 2 || failed[0] != "srv3" || failed[1] != "srv7" {
		t.Errorf("ByState(failed) = %q, want srv3 and srv7 in host order", failed)
	}

	// The failure is readable per host, which is what a pane shows.
	if err := lookup("srv3").Err(); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("srv3 Err() = %v, want the dial failure", err)
	}
	if lookup("srv1").State() != StateConnected {
		t.Error("a healthy host did not connect alongside the failing ones")
	}
}

// TestManagerSlowHostDoesNotDelayOthers is the isolation guarantee: one
// unreachable machine must not hold up thirty-nine healthy ones.
func TestManagerSlowHostDoesNotDelayOthers(t *testing.T) {
	fleet := fakeFleet(20)
	m, _ := newTestManager(t, fleet, func(h hosts.Host, f *Fake) {
		if h.Alias == "srv1" {
			f.ConnectDelay = 30 * time.Second // effectively hung
		}
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	m.Start(ctx)

	waitFor(t, "the other nineteen hosts to connect", func() bool {
		return m.Counts().Connected == 19
	})

	if got := m.Counts(); got.Pending != 1 {
		t.Errorf("Counts() = %+v, want the one hung host still pending", got)
	}
}

func TestManagerBoundsParallelDials(t *testing.T) {
	var inFlight, peak atomic.Int32

	factory := func(req SessionRequest) Session {
		f := NewFake(req.ID, req.Host, req.Events)
		f.ConnectDelay = 20 * time.Millisecond
		return f
	}

	// Wrap the factory so the test can watch concurrency through the fake's
	// connect delay.
	counting := func(req SessionRequest) Session {
		return &countingSession{Session: factory(req), inFlight: &inFlight, peak: &peak}
	}

	m, err := NewManager(ManagerConfig{
		Hosts:            fakeFleet(50),
		NewSession:       counting,
		MaxParallelDials: 4,
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	t.Cleanup(func() { m.CloseAll() })

	m.Start(t.Context())
	m.Wait()

	if got := peak.Load(); got > 4 {
		t.Errorf("peak concurrent dials = %d, want at most 4: fifty hosts must not open fifty sockets at once", got)
	}
	if got := peak.Load(); got < 2 {
		t.Errorf("peak concurrent dials = %d, want the dials to actually overlap", got)
	}
	if got := m.Counts().Connected; got != 50 {
		t.Errorf("connected = %d, want all 50 despite the bound", got)
	}
}

// countingSession records how many Starts run at once.
type countingSession struct {
	Session
	inFlight *atomic.Int32
	peak     *atomic.Int32
}

func (s *countingSession) Start(ctx context.Context) error {
	n := s.inFlight.Add(1)
	for {
		peak := s.peak.Load()
		if n <= peak || s.peak.CompareAndSwap(peak, n) {
			break
		}
	}
	defer s.inFlight.Add(-1)
	return s.Session.Start(ctx)
}

func TestManagerFansEventsIntoOneChannel(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(3), nil)

	m.Start(t.Context())
	m.Wait()

	lookup("srv2").Emit("something happened\r\n")

	seen := map[string]bool{}
	deadline := time.After(5 * time.Second)
	for len(seen) < 3 {
		select {
		case ev := <-m.Events():
			seen[ev.SessionID()] = true
		case <-deadline:
			t.Fatalf("only saw events from %v, want all three sessions on one channel", seen)
		}
	}
}

func TestManagerReconnect(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(3), nil)

	m.Start(t.Context())
	m.Wait()

	before, _ := m.Session("srv2")
	lookup("srv2").Disconnect(ErrDisconnected())

	if got := before.State(); got != StateFailed {
		t.Fatalf("State() = %s, want %s", got, StateFailed)
	}

	if err := m.Reconnect(t.Context(), "srv2"); err != nil {
		t.Fatalf("Reconnect: %v", err)
	}
	m.Wait()

	after, ok := m.Session("srv2")
	if !ok {
		t.Fatal("the session disappeared after Reconnect")
	}
	if after == before {
		t.Error("Reconnect reused the dead session")
	}
	if got := after.State(); got != StateConnected {
		t.Errorf("State() after Reconnect = %s, want %s", got, StateConnected)
	}
	if got := after.ID(); got != "srv2" {
		t.Errorf("ID() after Reconnect = %q, want it preserved so panes survive", got)
	}
	if got := after.Host().Alias; got != "srv2" {
		t.Errorf("Host() after Reconnect = %q, want the same host", got)
	}

	// Order is preserved, so the pane does not jump.
	if got := m.IDs(); got[1] != "srv2" {
		t.Errorf("IDs() = %q, want srv2 to stay in position 1", got)
	}
	if got := m.Counts(); got.Connected != 3 {
		t.Errorf("Counts() = %+v, want all three connected again", got)
	}
}

func TestManagerReconnectUnknownSession(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(1), nil)

	if err := m.Reconnect(t.Context(), "nope"); err == nil {
		t.Error("Reconnect of an unknown session returned no error")
	}
}

func TestManagerCloseOne(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(3), nil)
	m.Start(t.Context())
	m.Wait()

	if err := m.Close("srv2"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	counts := m.Counts()
	if counts.Connected != 2 || counts.Closed != 1 {
		t.Errorf("Counts() = %+v, want 2 connected and 1 closed: one dead host is one dead pane", counts)
	}

	if err := m.Close("nope"); err == nil {
		t.Error("Close of an unknown session returned no error")
	}
}

func TestManagerCloseAll(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(20), nil)
	m.Start(t.Context())
	m.Wait()

	if err := m.CloseAll(); err != nil {
		t.Fatalf("CloseAll: %v", err)
	}
	if got := m.Counts(); got.Closed != 20 {
		t.Errorf("Counts() = %+v, want all 20 closed", got)
	}
}

func TestManagerResizeReachesEverySessionOnce(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(10), nil)
	m.Start(t.Context())
	m.Wait()

	if err := m.Resize(132, 50); err != nil {
		t.Fatalf("Resize: %v", err)
	}

	for _, id := range m.IDs() {
		f := lookup(id)
		if w, h := f.Size(); w != 132 || h != 50 {
			t.Errorf("%s size = %dx%d, want 132x50", id, w, h)
		}
		if got := f.Resizes(); got != 1 {
			t.Errorf("%s was resized %d times, want exactly 1", id, got)
		}
	}
}

func TestManagerResizeReportsAFailureButStillReachesEveryone(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(5), nil)
	m.Start(t.Context())
	m.Wait()

	// An invalid size fails on every session; the point is that the call does
	// not stop at the first one.
	if err := m.Resize(0, 0); err == nil {
		t.Error("Resize with an invalid size returned no error")
	}
	if got := lookup("srv5").Resizes(); got != 0 {
		t.Errorf("srv5 recorded %d resizes for a rejected size", got)
	}
}

func TestManagerStartHonoursContextCancellation(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(50), func(_ hosts.Host, f *Fake) {
		f.ConnectDelay = 50 * time.Millisecond
	})

	ctx, cancel := context.WithCancel(t.Context())
	m.Start(ctx)
	cancel()
	m.Wait()

	// Cancelling must not hang, and must not leave the fleet claiming success.
	if got := m.Counts(); got.Connected == 50 {
		t.Error("every host connected although the context was cancelled immediately")
	}
}

func TestManagerIsConcurrencySafe(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(10), nil)
	m.Start(t.Context())

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = m.Counts()
				_ = m.IDs()
				_ = m.SortedIDs()
				_ = m.ByState(StateConnected)
				m.Session("srv1")
				if j%20 == 0 {
					m.Reconnect(context.Background(), "srv1")
				}
			}
		}(i)
	}
	wg.Wait()
	m.Wait()
}

func TestSortedIDs(t *testing.T) {
	fleet := []hosts.Host{
		{Alias: "web-2"}, {Alias: "db-1"}, {Alias: "web-1"},
	}
	m, _ := newTestManager(t, fleet, nil)

	got := m.SortedIDs()
	want := []string{"db-1", "web-1", "web-2"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SortedIDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// The typed order is still available and unchanged.
	if m.IDs()[0] != "web-2" {
		t.Errorf("IDs() = %q, want the order the user typed", m.IDs())
	}
}

// The broadcast router asks the manager who can take a byte. A session that is
// dialling, failed or closed is not a writer: writing into a dead session would
// report success to a user who is about to assume the command ran.
func TestManagerConnectedAndWriter(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(2), nil)

	for _, id := range []string{"srv1", "srv2"} {
		if m.Connected(id) {
			t.Fatalf("%s reported as connected before it was started", id)
		}
		if _, ok := m.Writer(id); ok {
			t.Fatalf("%s handed out a writer before it was started", id)
		}
	}

	if err := lookup("srv1").Start(t.Context()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !m.Connected("srv1") {
		t.Fatal("a connected session is not reported as connected")
	}
	w, ok := m.Writer("srv1")
	if !ok {
		t.Fatal("a connected session handed out no writer")
	}
	if _, err := w.Write([]byte("uptime\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := lookup("srv1").Written(); got != "uptime\n" {
		t.Fatalf("the session received %q", got)
	}

	lookup("srv1").Disconnect(ErrDisconnected())
	if m.Connected("srv1") {
		t.Fatal("a failed session is still reported as connected")
	}
	if _, ok := m.Writer("srv1"); ok {
		t.Fatal("a failed session still hands out a writer")
	}

	if m.Connected("srv99") {
		t.Fatal("a host that is not in the fleet reported as connected")
	}
	if _, ok := m.Writer("srv99"); ok {
		t.Fatal("a host that is not in the fleet handed out a writer")
	}
}

func TestManagerAddDialsOneMoreHost(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(2), nil)
	m.Start(t.Context())
	m.Wait()

	host := hosts.Host{Alias: "extra", Addr: "extra.example.com", User: "deploy", Port: 22}
	id := m.Add(t.Context(), host)
	m.Wait()

	if id != "extra" {
		t.Errorf("Add() = %q, want %q", id, "extra")
	}
	want := []string{"srv1", "srv2", "extra"}
	got := m.IDs()
	if len(got) != len(want) {
		t.Fatalf("IDs() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("IDs()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	if !m.Connected("extra") {
		t.Error("the added host did not connect")
	}
	if got := m.Counts(); got.Connected != 3 {
		t.Errorf("Counts() = %+v, want 3 connected", got)
	}
}

func TestManagerAddDisambiguatesAgainstTheRun(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(1), nil)
	m.Start(t.Context())
	m.Wait()

	id := m.Add(t.Context(), fakeFleet(1)[0])
	m.Wait()

	if id != "srv1#2" {
		t.Errorf("Add() = %q, want %q", id, "srv1#2")
	}
	if !m.Connected(id) {
		t.Error("the added duplicate did not connect under its own identity")
	}
}

func TestRemoveTakesTheSessionOutOfTheFleet(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(3), nil)
	m.Start(t.Context())
	m.Wait()

	if err := m.Remove("srv2"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	got := m.IDs()
	if len(got) != 2 || got[0] != "srv1" || got[1] != "srv3" {
		t.Fatalf("IDs() = %v after removing the middle host", got)
	}
	if _, ok := m.Session("srv2"); ok {
		t.Fatal("the removed session is still looked up")
	}
	if m.Counts().Total != 2 {
		t.Fatalf("Counts().Total = %d", m.Counts().Total)
	}
}

func TestRemoveClosesTheSession(t *testing.T) {
	m, lookup := newTestManager(t, fakeFleet(1), nil)
	m.Start(t.Context())
	m.Wait()

	if err := m.Remove("srv1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if state := lookup("srv1").State(); state != StateClosed {
		t.Fatalf("State() = %v after Remove, want closed", state)
	}
}

func TestRemoveUnknownID(t *testing.T) {
	m, _ := newTestManager(t, fakeFleet(1), nil)
	if err := m.Remove("nope"); err == nil {
		t.Fatal("removing an unknown id did not error")
	}
}
