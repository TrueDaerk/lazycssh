package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// diffApp builds an app with a router and fake sessions, sends command over
// the command line, and hands back the model plus the fleet to emit output on.
func diffApp(t *testing.T, command string, names ...string) (App, *fakeFleet) {
	t.Helper()
	a, _, _, _ := cmdApp(t, names...)
	// The broadcast limit follows the visible page; the diff marks follow the
	// targets. Room for every pane keeps every host in the window.
	a = resize(t, a, 220, 70)
	fleet := a.cfg.Fleet.(*fakeFleet)
	a = typeCommand(t, a, command)
	a, _ = enter(t, a)
	return a, fleet
}

// selectDiffPanel jumps to the Output diff panel, which is what triggers the
// recompute.
func selectDiffPanel(t *testing.T, a App) App {
	t.Helper()
	a = pressKey(t, a, "6")
	if a.Panel() != PanelDiff {
		t.Fatalf("Panel() = %v after 6, want PanelDiff", a.Panel())
	}
	return a
}

func TestDiffPanelSaysNothingToCompareBeforeASend(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	body := plain(a.panelBody(PanelDiff, 60, 10, false))
	if !strings.Contains(body, "nothing to compare yet") {
		t.Fatalf("empty diff panel says:\n%s", body)
	}
}

func TestDiffPanelGroupsHostsByOutput(t *testing.T) {
	a, fleet := diffApp(t, "uptime", "web-01", "web-02", "db-01")
	fleet.sessions["web-01"].Emit("load 0.1\r\n")
	fleet.sessions["web-02"].Emit("load 0.1\r\n")
	fleet.sessions["db-01"].Emit("load 9.9\r\n")

	a = selectDiffPanel(t, a)
	body := plain(a.panelBody(PanelDiff, 60, 10, true))
	if !strings.Contains(body, "2 hosts · load 0.1") {
		t.Fatalf("majority row missing:\n%s", body)
	}
	if !strings.Contains(body, "1 host · load 9.9") {
		t.Fatalf("outlier row missing:\n%s", body)
	}
	if got := a.DiffVariants(); len(got) != 2 {
		t.Fatalf("got %d variants, want 2", len(got))
	}
}

func TestDiffIgnoresOutputFromBeforeTheSend(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01", "web-02")
	a = resize(t, a, 220, 70)
	f := a.cfg.Fleet.(*fakeFleet)
	// One host was chatty before the command went out; the comparison window
	// starts at the send, so the old noise must not split the groups.
	f.sessions["web-01"].Emit("old noise\r\nmore noise\r\n")

	a = typeCommand(t, a, "uptime")
	a, _ = enter(t, a)
	f.sessions["web-01"].Emit("load 0.1\r\n")
	f.sessions["web-02"].Emit("load 0.1\r\n")

	a = selectDiffPanel(t, a)
	if got := a.DiffVariants(); len(got) != 1 {
		t.Fatalf("got %d variants, want 1 - pre-send output leaked into the diff:\n%+v", len(got), got)
	}
}

func TestDiffNormalizesTheHostsOwnName(t *testing.T) {
	a, fleet := diffApp(t, "hostname", "web-01", "web-02")
	fleet.sessions["web-01"].Emit("web-01\r\n")
	fleet.sessions["web-02"].Emit("web-02\r\n")

	a = selectDiffPanel(t, a)
	if got := a.DiffVariants(); len(got) != 1 {
		t.Fatalf("got %d variants, want 1 - hostnames were not normalized:\n%+v", len(got), got)
	}
}

func TestDiffANewSendOpensANewWindow(t *testing.T) {
	a, fleet := diffApp(t, "uptime", "web-01", "web-02")
	fleet.sessions["web-01"].Emit("load 0.1\r\n")
	fleet.sessions["web-02"].Emit("load 9.9\r\n")
	a = selectDiffPanel(t, a)
	if got := a.DiffVariants(); len(got) != 2 {
		t.Fatalf("setup: got %d variants, want 2", len(got))
	}

	a = typeCommand(t, a, "date")
	a, _ = enter(t, a)
	fleet.sessions["web-01"].Emit("Sat Aug 9\r\n")
	fleet.sessions["web-02"].Emit("Sat Aug 9\r\n")

	a = selectDiffPanel(t, a)
	if got := a.DiffVariants(); len(got) != 1 {
		t.Fatalf("got %d variants after the second send, want 1:\n%+v", len(got), got)
	}
}

func TestDiffPreviewShowsTheVariantWhole(t *testing.T) {
	a, fleet := diffApp(t, "uptime", "web-01", "web-02", "db-01")
	fleet.sessions["web-01"].Emit("load 0.1\r\n")
	fleet.sessions["web-02"].Emit("load 0.1\r\n")
	fleet.sessions["db-01"].Emit("load 9.9\r\n")

	a = selectDiffPanel(t, a)
	_, body := a.panelPreview(PanelDiff, 60, 20)
	text := plain(body)
	for _, want := range []string{"uptime", "web-01 web-02", "2 of 3 compared", "load 0.1"} {
		if !strings.Contains(text, want) {
			t.Fatalf("preview misses %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "load 9.9") {
		t.Fatalf("preview of the majority shows the outlier's output:\n%s", text)
	}
}

func TestDiffEnterSelectsTheVariantsHosts(t *testing.T) {
	a, fleet := diffApp(t, "uptime", "web-01", "web-02", "db-01")
	fleet.sessions["web-01"].Emit("load 0.1\r\n")
	fleet.sessions["web-02"].Emit("load 0.1\r\n")
	fleet.sessions["db-01"].Emit("load 9.9\r\n")

	a = selectDiffPanel(t, a)
	a = pressKey(t, a, "down") // onto the outlier
	model, cmd := a.Update(keyMsgFor(t, "enter"))
	a = model.(App)
	if cmd == nil {
		t.Fatal("enter on a variant produced no command")
	}
	msg := cmd()
	sel, ok := msg.(DiffSelectMsg)
	if !ok {
		t.Fatalf("enter produced a %T, want DiffSelectMsg", msg)
	}
	if len(sel.Hosts) != 1 || sel.Hosts[0] != "db-01" {
		t.Fatalf("DiffSelectMsg.Hosts = %v, want [db-01]", sel.Hosts)
	}

	model, _ = a.Update(msg)
	a = model.(App)
	router := a.cfg.Targets
	if !router.IsSelected("db-01") {
		t.Fatal("the outlier is not selected after enter")
	}
	if router.IsSelected("web-01") || router.IsSelected("web-02") {
		t.Fatal("the majority hosts are selected too")
	}
	if !strings.Contains(a.LastDelivery(), "selected this variant's 1 host") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
}

func TestDiffSelectWithoutARouterSaysSo(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	model, _ := a.Update(DiffSelectMsg{Hosts: []string{"web-01"}})
	a = model.(App)
	if !strings.Contains(a.LastDelivery(), "no host selection yet") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
}

func TestDiffUnselectedPanelKeepsTheLastGroups(t *testing.T) {
	a, fleet := diffApp(t, "uptime", "web-01", "web-02")
	fleet.sessions["web-01"].Emit("load 0.1\r\n")
	fleet.sessions["web-02"].Emit("load 9.9\r\n")
	a = selectDiffPanel(t, a)
	if got := a.DiffVariants(); len(got) != 2 {
		t.Fatalf("setup: got %d variants, want 2", len(got))
	}

	// Leaving the panel stops the recompute; the rows it was last handed stay.
	a = pressKey(t, a, "1")
	fleet.sessions["web-02"].Emit("even more\r\n")
	var m tea.Model = a
	m, _ = a.Update(SessionOutputMsg{ID: "web-02"})
	a = m.(App)
	if got := a.DiffVariants(); len(got) != 2 {
		t.Fatalf("unselected panel recomputed anyway: %d variants", len(got))
	}
}
