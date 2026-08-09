package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// soloApp is one open session ("solo") over two connected hosts, on the
// Sessions panel.
func soloApp(t *testing.T) (App, *fakeFleet) {
	t.Helper()
	fleet := newFakeFleet("web-01", "web-02")
	a := resize(t, NewApp(Config{
		SessionName: "solo",
		Fleet:       fleet,
		Panes:       fleet,
		Theme:       Options{Dark: true},
	}), 120, 40)
	fleet.connect(t, "web-01")
	fleet.connect(t, "web-02")
	return pressKey(t, a, "3"), fleet
}

// msgsFrom runs a command tree - batches included - and returns every message
// it produces.
func msgsFrom(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, msgsFrom(t, c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// removedHosts filters the RemoveHostMsg identifiers out of a message list.
func removedHosts(msgs []tea.Msg) []string {
	var out []string
	for _, m := range msgs {
		if rm, ok := m.(RemoveHostMsg); ok {
			out = append(out, rm.ID)
		}
	}
	return out
}

// The acceptance criterion: ctrl+c on N machines is never sent without the
// user answering for it, and no is a full stop.
func TestXAsksBeforeEndingASession(t *testing.T) {
	a, fleet := soloApp(t)

	a = pressKey(t, a, "x")
	if a.EndSessionPending() != "solo" {
		t.Fatalf("EndSessionPending() = %q after x", a.EndSessionPending())
	}
	if view := plain(a.View().Content); !strings.Contains(view, `end "solo"?`) {
		t.Fatalf("the modal does not ask:\n%s", view)
	}

	a = pressKey(t, a, "n")
	if a.EndSessionPending() != "" {
		t.Fatal("answering no left the question open")
	}
	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("answering no still sent %q", got)
	}
}

// The acceptance criterion: y sends exactly ctrl+c then ctrl+d to every
// connected host of that session, and the session stays listed as ending
// until the shells actually exit.
func TestYSendsInterruptAndEOF(t *testing.T) {
	a, fleet := soloApp(t)

	a = pressKey(t, a, "x")
	a = pressKey(t, a, "y")

	for _, id := range []string{"web-01", "web-02"} {
		if got := fleet.sessions[id].Written(); got != "\x03\x04" {
			t.Fatalf("%s received %q, want ctrl+c then ctrl+d", id, got)
		}
	}
	if got := strings.Join(a.OpenSessionNames(), ","); got != "solo" {
		t.Fatalf("open sessions = %q; the session must wait for its shells", got)
	}
	if !strings.Contains(plain(a.panelBody(PanelSessions, 60, 20, true)), "(ending)") {
		t.Fatalf("the ending state is not visible:\n%s", plain(a.panelBody(PanelSessions, 60, 20, true)))
	}
}

// Once the last shell exits, the ended session leaves the list and its hosts
// leave the run.
func TestEndedSessionLeavesWhenItsShellsExit(t *testing.T) {
	a, fleet := soloApp(t)
	a = pressKey(t, a, "x")
	a = pressKey(t, a, "y")

	fleet.sessions["web-01"].Disconnect(nil)
	fleet.sessions["web-02"].Disconnect(nil)
	model, cmd := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if len(a.OpenSessionNames()) != 0 {
		t.Fatalf("open sessions = %v after every shell exited", a.OpenSessionNames())
	}
	if a.ActiveSession() != "" {
		t.Fatalf("ActiveSession() = %q with nothing open", a.ActiveSession())
	}
	if got := strings.Join(removedHosts(msgsFrom(t, cmd)), ","); got != "web-01,web-02" {
		t.Fatalf("removed hosts = %q, want both", got)
	}
}

// The acceptance criterion: ctrl+d broadcast across the whole session ends it
// without another keypress - all hosts closed is the signal, x not required.
func TestFullyClosedSessionEndsItself(t *testing.T) {
	a, fleet := soloApp(t)

	fleet.sessions["web-01"].Disconnect(nil)
	fleet.sessions["web-02"].Disconnect(nil)
	model, cmd := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if len(a.OpenSessionNames()) != 0 {
		t.Fatalf("open sessions = %v after every host closed", a.OpenSessionNames())
	}
	if got := strings.Join(removedHosts(msgsFrom(t, cmd)), ","); got != "web-01,web-02" {
		t.Fatalf("removed hosts = %q, want both", got)
	}
}

// A session whose hosts merely all failed is an outage, not a shutdown: it
// stays listed so the user can see it and reconnect.
func TestFailedSessionStaysListed(t *testing.T) {
	a, fleet := soloApp(t)

	fleet.fail(t, "web-01")
	fleet.fail(t, "web-02")
	model, cmd := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if got := strings.Join(a.OpenSessionNames(), ","); got != "solo" {
		t.Fatalf("open sessions = %q; a failed fleet must not vanish", got)
	}
	if got := removedHosts(msgsFrom(t, cmd)); len(got) != 0 {
		t.Fatalf("failed hosts were removed: %v", got)
	}
}

// A session the user asked to end does not wait for a host that died
// mid-shutdown: ending plus every host done is enough.
func TestEndingSessionEndsOnFailedHostsToo(t *testing.T) {
	a, fleet := soloApp(t)
	a = pressKey(t, a, "x")
	a = pressKey(t, a, "y")

	fleet.sessions["web-01"].Disconnect(nil)
	fleet.sessions["web-02"].Disconnect(ssh.ErrDisconnected())
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if len(a.OpenSessionNames()) != 0 {
		t.Fatalf("open sessions = %v; an ending session must not wait for a dead host", a.OpenSessionNames())
	}
}

// A cleanly closed shell leaves the run even when another open session still
// lists the host: the shell is just as gone there (issue #146). The surviving
// sessions themselves stay open for their live hosts.
func TestClosedSharedHostLeavesEverySession(t *testing.T) {
	a, fleet := openTwo(t) // prod-web holds all three hosts, front web-01/02, back db-01
	fleet.connect(t, "db-01")

	fleet.sessions["web-01"].Disconnect(nil)
	fleet.sessions["web-02"].Disconnect(nil)
	model, cmd := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if got := strings.Join(a.OpenSessionNames(), ","); got != "prod-web,back" {
		t.Fatalf("open sessions = %q; only front is fully closed", got)
	}
	if got := strings.Join(removedHosts(msgsFrom(t, cmd)), ","); got != "web-01,web-02" {
		t.Fatalf("removed hosts = %q, want the closed shells to leave the run", got)
	}
}

// x targets the session's hosts exactly, whatever the broadcast mode is:
// hosts of other sessions receive nothing.
func TestEndingTargetsOnlyThatSession(t *testing.T) {
	a, fleet := openTwo(t)
	for _, id := range []string{"web-01", "web-02", "db-01"} {
		fleet.connect(t, id)
	}

	a = pressKey(t, a, "j") // onto "front"
	a = pressKey(t, a, "x")
	if a.EndSessionPending() != "front" {
		t.Fatalf("EndSessionPending() = %q", a.EndSessionPending())
	}
	a = pressKey(t, a, "y")

	if got := fleet.sessions["web-01"].Written(); got != "\x03\x04" {
		t.Fatalf("web-01 received %q", got)
	}
	if got := fleet.sessions["db-01"].Written(); got != "" {
		t.Fatalf("db-01 is not in the session and received %q", got)
	}
	_ = a
}

// Ending the foreground session falls back to another open one.
func TestEndingTheForegroundFallsBack(t *testing.T) {
	a, fleet := openTwo(t)
	fleet.connect(t, "db-01")

	if a.ActiveSession() != "back" {
		t.Fatalf("setup: ActiveSession() = %q", a.ActiveSession())
	}
	fleet.sessions["db-01"].Disconnect(nil)
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if a.ActiveSession() == "back" || a.ActiveSession() == "" {
		t.Fatalf("ActiveSession() = %q after back ended", a.ActiveSession())
	}
}

// The bug of issue #146: a host that failed from the start must not keep the
// session - and with it the program - alive once every other host was logged
// out. One clean logout plus everything done ends the session, failed
// remainder included.
func TestMixedClosedAndFailedSessionEnds(t *testing.T) {
	a, fleet := soloApp(t)

	fleet.sessions["web-01"].Disconnect(nil)
	fleet.sessions["web-02"].Disconnect(ssh.ErrDisconnected())
	model, cmd := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if len(a.OpenSessionNames()) != 0 {
		t.Fatalf("open sessions = %v; a failed host kept the session alive", a.OpenSessionNames())
	}
	if got := strings.Join(removedHosts(msgsFrom(t, cmd)), ","); got != "web-01,web-02" {
		t.Fatalf("removed hosts = %q, want both", got)
	}
}

// A clean logout auto-closes its pane: the host leaves the run on its own,
// no x required, while the rest of the session stays untouched.
func TestCleanLogoutClosesItsPane(t *testing.T) {
	a, fleet := soloApp(t)

	fleet.sessions["web-01"].Disconnect(nil)
	model, cmd := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if got := strings.Join(removedHosts(msgsFrom(t, cmd)), ","); got != "web-01" {
		t.Fatalf("removed hosts = %q, want just the logged-out one", got)
	}
	if got := strings.Join(a.OpenSessionNames(), ","); got != "solo" {
		t.Fatalf("open sessions = %q; the session still has a live host", got)
	}
}

// The logout is remembered after the closed pane left: when the remaining
// host later fails, the session ends instead of lingering as a fake outage.
func TestRememberedLogoutEndsAFailedRemainder(t *testing.T) {
	a, fleet := soloApp(t)

	// web-01 logs out; its pane auto-closes and the program removes it.
	fleet.sessions["web-01"].Disconnect(nil)
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)
	fleet.ids = []string{"web-02"}
	delete(fleet.sessions, "web-01")
	model, _ = a.Update(HostsChangedMsg{Hosts: fleet.IDs()})
	a = model.(App)

	// Now the survivor dies. No closed host is visible any more, but the
	// session saw a logout, so it is over - not an outage.
	fleet.sessions["web-02"].Disconnect(ssh.ErrDisconnected())
	model, cmd := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	if len(a.OpenSessionNames()) != 0 {
		t.Fatalf("open sessions = %v; the remembered logout must end the session", a.OpenSessionNames())
	}
	if got := strings.Join(removedHosts(msgsFrom(t, cmd)), ","); got != "web-02" {
		t.Fatalf("removed hosts = %q, want the failed remainder", got)
	}
}

// A run that held hosts and lost the last one stays open on the neutral start
// view (issue #168): the TUI is the hub the next group is opened from, and
// quitting is what q and ctrl+q are for.
func TestRunThatEmptiedStaysOpen(t *testing.T) {
	a, fleet := soloApp(t)

	fleet.sessions["web-01"].Disconnect(nil)
	fleet.sessions["web-02"].Disconnect(nil)
	model, _ := a.Update(FleetUpdatedMsg{})
	a = model.(App)

	fleet.ids = nil
	fleet.sessions = map[string]*ssh.Fake{}
	model, cmd := a.Update(HostsChangedMsg{Hosts: nil})
	for _, msg := range msgsFrom(t, cmd) {
		if msg == tea.Quit() {
			t.Fatal("the emptied run quit the program")
		}
	}
	a = model.(App)
	if a.Focus() == AreaGrid {
		t.Fatal("focus stayed on the grid with no panes to focus")
	}
	if got := len(a.OpenSessionNames()); got != 0 {
		t.Fatalf("open sessions = %d, want none", got)
	}

	// The neutral view must be a real restart: a host connecting afterwards
	// begins a fresh run whose emptying is judged anew.
	if a.hadHosts {
		t.Fatal("hadHosts survived the emptied run")
	}
}

// A run that starts empty is waiting on the sessions picker, not done: no
// quit.
func TestEmptyStartDoesNotQuit(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)

	_, cmd := a.Update(HostsChangedMsg{Hosts: nil})
	for _, msg := range msgsFrom(t, cmd) {
		if msg == tea.Quit() {
			t.Fatal("an empty start quit the program")
		}
	}
}
