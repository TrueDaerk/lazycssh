package ui

import (
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// gridApp is a sized app with the grid focused on its first pane.
func gridApp(t *testing.T) App {
	t.Helper()
	return focusGrid(t, resize(t, testApp(), 120, 40))
}

// keyMsgResult drives one key press and returns the message its command
// produces, or nil when there is no command.
func keyMsgResult(t *testing.T, a App, keystroke string) tea.Msg {
	t.Helper()
	model, cmd := a.Update(keyMsgFor(t, keystroke))
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	if cmd == nil {
		return nil
	}
	return cmd()
}

func TestReconnectKeyEmitsReconnectHostMsg(t *testing.T) {
	got := keyMsgResult(t, gridApp(t), "alt+r")
	msg, ok := got.(ReconnectHostMsg)
	if !ok {
		t.Fatalf("pressing r produced %T, want ReconnectHostMsg", got)
	}
	if msg.ID != "web-01" {
		t.Errorf("ReconnectHostMsg.ID = %q, want the focused host web-01", msg.ID)
	}
}

func TestCloseKeyEmitsCloseHostMsg(t *testing.T) {
	got := keyMsgResult(t, gridApp(t), "alt+x")
	msg, ok := got.(CloseHostMsg)
	if !ok {
		t.Fatalf("pressing x produced %T, want CloseHostMsg", got)
	}
	if msg.ID != "web-01" {
		t.Errorf("CloseHostMsg.ID = %q, want the focused host web-01", msg.ID)
	}
}

func TestReconnectAllKeyEmitsReconnectAllMsg(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03")
	fleet.fail(t, "web-01")                  // failed
	fleet.connect(t, "web-02")               // connected
	fleet.sessions["web-03"].Disconnect(nil) // closed, never connected
	a = syncFleet(t, a)

	got := keyMsgResult(t, a, "R")
	if _, ok := got.(ReconnectAllMsg); !ok {
		t.Fatalf("pressing R produced %T, want ReconnectAllMsg", got)
	}
}

func TestReconnectAllKeyReportsHowManyWillRetry(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03")
	fleet.fail(t, "web-01")
	fleet.sessions["web-02"].Disconnect(nil) // closed
	fleet.connect(t, "web-03")
	a = syncFleet(t, a)

	model, cmd := a.Update(keyMsgFor(t, "R"))
	next := model.(App)
	if cmd == nil {
		t.Fatal("pressing R produced no command")
	}
	if got := next.LastDelivery(); got != "reconnecting 2 hosts" {
		t.Errorf("LastDelivery() = %q, want a count of the 2 down hosts", got)
	}
}

func TestReconnectAllKeyIsANoOpWithNothingDown(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")
	a = syncFleet(t, a)
	before := a.LastDelivery()

	got := keyMsgResult(t, a, "R")
	if got != nil {
		t.Fatalf("pressing R with nothing down produced %T, want nothing", got)
	}

	model, _ := a.Update(keyMsgFor(t, "R"))
	next := model.(App)
	if got := next.LastDelivery(); got != before {
		t.Errorf("LastDelivery() changed to %q with nothing to reconnect, want no flicker", got)
	}
}

func TestReconnectKeyWithoutHostsEmitsNothing(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "tab")
	if got := keyMsgResult(t, a, "r"); got != nil {
		t.Fatalf("pressing r with no hosts produced %T, want nothing", got)
	}
}

// x is state-dependent: a live host's session is closed, a dead host's pane
// is removed.
func TestCloseKeyOnADeadHostEmitsRemove(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.fail(t, "web-01")
	a = syncFleet(t, a)
	a = focusGrid(t, a)

	got := keyMsgResult(t, a, "alt+x")
	msg, ok := got.(RemoveHostMsg)
	if !ok {
		t.Fatalf("pressing x on a failed host produced %T, want RemoveHostMsg", got)
	}
	if msg.ID != "web-01" {
		t.Errorf("RemoveHostMsg.ID = %q", msg.ID)
	}
}

func TestCloseKeyOnALiveHostEmitsClose(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = focusGrid(t, a)

	if _, ok := keyMsgResult(t, a, "alt+x").(CloseHostMsg); !ok {
		t.Fatal("pressing alt+x on a connected host did not emit CloseHostMsg")
	}
}

// A host that left the run takes its scroll offset with it.
func TestRemovedHostScrollOffsetIsPruned(t *testing.T) {
	a, _ := scrollApp(t, 50)
	a = pressKey(t, a, "shift+home") // scroll to the top, recording an offset
	if len(a.scroll) == 0 {
		t.Fatal("setup: no scroll offset recorded")
	}

	model, _ := a.Update(HostsChangedMsg{Hosts: nil})
	next := model.(App)
	if len(next.scroll) != 0 {
		t.Fatalf("scroll offsets survived the host leaving: %v", next.scroll)
	}
}

// spyFleet counts every read that touches cross-goroutine session state -
// IDs, Counts, State, Err, LastExit. Session lookups themselves are free:
// the render path may still fetch a session to reach its internally
// synchronized scrollback.
type spyFleet struct {
	*fakeFleet
	reads atomic.Int64
}

func (f *spyFleet) IDs() []string {
	f.reads.Add(1)
	return f.fakeFleet.IDs()
}

func (f *spyFleet) Counts() ssh.Counts {
	f.reads.Add(1)
	return f.fakeFleet.Counts()
}

func (f *spyFleet) Session(id string) (ssh.Session, bool) {
	s, ok := f.fakeFleet.Session(id)
	if !ok {
		return nil, false
	}
	return spySession{Session: s, reads: &f.reads}, true
}

type spySession struct {
	ssh.Session
	reads *atomic.Int64
}

func (s spySession) State() ssh.State {
	s.reads.Add(1)
	return s.Session.State()
}

func (s spySession) Err() error {
	s.reads.Add(1)
	return s.Session.Err()
}

func (s spySession) LastExit() (int, uint64) {
	s.reads.Add(1)
	return s.Session.LastExit()
}

// The acceptance criterion of issue #136: no live session-state read is
// reachable from View. State renders from the model's snapshot, which only
// Update refreshes - so rendering any number of frames after one Update
// touches the fleet's state exactly zero times.
func TestViewReadsNoLiveSessionState(t *testing.T) {
	fleet := newFakeFleet("web-01", "web-02", "web-03")
	spy := &spyFleet{fakeFleet: fleet}
	a := resize(t, NewApp(Config{
		Fleet:  spy,
		Sender: &fakeSender{delivery: broadcast.Delivery{Mode: broadcast.ModeAll, Scope: 3, Targets: 3, Delivered: 3}},
		Theme:  Options{Dark: true},
	}), 200, 60)

	fleet.connect(t, "web-01")
	fleet.sessions["web-01"].Emit("hello\n")
	// The exit indicator is per command, so the failure needs a command to
	// belong to; the send is what opens the window the marker answers.
	a = sendVia(t, a, "deploy")
	fleet.sessions["web-01"].ReportExit(1)
	fleet.fail(t, "web-02")
	a = syncFleet(t, a)

	before := spy.reads.Load()
	for range 5 {
		if a.View().Content == "" {
			t.Fatal("View rendered nothing")
		}
	}
	if got := spy.reads.Load(); got != before {
		t.Fatalf("View performed %d live fleet reads", got-before)
	}

	// And the snapshot it rendered from was correct.
	view := plain(a.View().Content)
	for _, want := range []string{"web-01 connected", "exit 1", "web-02 failed", "hello"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the snapshot missed %q:\n%s", want, view)
		}
	}
}

// The other acceptance criterion: sessions flipping state and writing output
// at full speed while frames render must not race the model. Run under
// -race, which CI does; the visible list follows the flips through the
// snapshot.
func TestRenderSurvivesConcurrentStateFlips(t *testing.T) {
	t.Parallel()
	a, fleet, router, _ := statusApp(t, "web-01", "web-02", "web-03")
	router.Attach(fleetSessions{fleet})

	ctx := t.Context()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, s := range fleet.sessions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = s.Start(ctx)
				s.Emit("tick\n")
				s.ReportExit(i % 2)
				s.Disconnect(ssh.ErrDisconnected())
				// Yield so the flips interleave with renders instead of
				// monopolizing the scheduler; under -race an unyielding
				// loop here made this test dominate the whole package
				// (issue #230).
				runtime.Gosched()
			}
		}()
	}

	// 50 renders are plenty for the race detector to see every unsafe
	// interleaving; 500 only multiplied the wall time (issue #230).
	for range 50 {
		a = syncFleet(t, a)
		if a.View().Content == "" {
			t.Fatal("View rendered nothing")
		}
	}
	close(stop)
	wg.Wait()
}
