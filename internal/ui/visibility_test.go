package ui

import (
	"strings"
	"testing"
)

// The acceptance criterion: 8 hosts, 2 down - ctrl+a shows 6 panes and
// broadcast all reaches 6.
func TestConnectedOnlyHidesTheDownHosts(t *testing.T) {
	a, fleet, router, _ := statusApp(t, "web-01", "web-02", "web-03")
	router.Attach(fleetSessions{fleet})
	fleet.connect(t, "web-01")
	fleet.connect(t, "web-03")
	fleet.fail(t, "web-02")

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
