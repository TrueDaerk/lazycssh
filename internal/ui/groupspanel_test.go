package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// groupsApp builds an app on the Groups panel over a working set with two named
// sets defined.
func groupsApp(t *testing.T) (App, *workingset.Manager) {
	t.Helper()

	a, _, _, ws := statusApp(t, "web-01", "web-02", "db-01", "db-02")
	if err := ws.Define("front", workingset.Range{From: 1, To: 2}); err != nil {
		t.Fatalf("Define: %v", err)
	}
	if err := ws.Define("databases", workingset.Pattern{Glob: "db-*"}); err != nil {
		t.Fatalf("Define: %v", err)
	}
	return pressKey(t, a, "3"), ws
}

func TestGroupsPanelListsTheSets(t *testing.T) {
	a, _ := groupsApp(t)

	view := plain(a.groupsPanel(40, 20))
	for _, want := range []string{allHostsRow, "front (first-2)", "databases (db-*)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the Groups panel does not list %q:\n%s", want, view)
		}
	}
}

// The acceptance criterion: the active working set is unmistakably marked.
func TestActiveGroupIsMarked(t *testing.T) {
	a, ws := groupsApp(t)

	// Every host is the working set to begin with.
	line := activeLine(t, plain(a.groupsPanel(40, 20)))
	if !strings.Contains(line, allHostsRow) {
		t.Fatalf("the all-hosts row is not marked active:\n%s", line)
	}

	if err := ws.Activate("databases"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	line = activeLine(t, plain(a.groupsPanel(40, 20)))
	if !strings.Contains(line, "databases") {
		t.Fatalf("the active set is not marked:\n%s", line)
	}
	// The marker is a character, not only a colour, so it survives a terminal
	// without colour.
	mono := NewApp(Config{WorkingSet: ws, Theme: Options{NoColor: true}})
	mono = resize(t, mono, 120, 40)
	if !strings.Contains(plain(mono.groupsPanel(40, 20)), "▸ databases") {
		t.Fatalf("the active marker vanished without colour:\n%s", plain(mono.groupsPanel(40, 20)))
	}
}

// activeLine returns the line carrying the active marker.
func activeLine(t *testing.T, view string) string {
	t.Helper()
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "▸") {
			return line
		}
	}
	t.Fatalf("no row is marked active:\n%s", view)
	return ""
}

// The acceptance criterion: selecting a group makes it the working set in one
// keystroke.
func TestEnterActivatesTheSelectedGroup(t *testing.T) {
	a, ws := groupsApp(t)

	a = pressKey(t, a, "j") // onto "front"
	if !strings.Contains(a.SelectedGroup(), "front") {
		t.Fatalf("SelectedGroup() = %q", a.SelectedGroup())
	}

	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if ws.ActiveName() != "front" {
		t.Fatalf("ActiveName() = %q after enter", ws.ActiveName())
	}
	if got := ws.Count(); got != 2 {
		t.Fatalf("the working set holds %d hosts, want 2", got)
	}
}

func TestEnterOnAllHostsResets(t *testing.T) {
	a, ws := groupsApp(t)
	if err := ws.Activate("front"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // cursor is on all hosts
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if ws.ActiveName() != "" || ws.Count() != 4 {
		t.Fatalf("all hosts did not restore the full run: %q, %d hosts",
			ws.ActiveName(), ws.Count())
	}
}

// An ad hoc set - typed once, or produced by paging - is listed too, so the
// panel never hides where the user actually is.
func TestAdHocSetIsListed(t *testing.T) {
	a, ws := groupsApp(t)
	if err := ws.Apply(workingset.Range{From: 3, To: 4}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	view := plain(a.groupsPanel(40, 20))
	if !strings.Contains(view, "(unnamed) 3-4") {
		t.Fatalf("the ad hoc set is not listed:\n%s", view)
	}
	if !strings.Contains(activeLine(t, view), "unnamed") {
		t.Fatalf("the ad hoc set is not marked active:\n%s", view)
	}
}

func TestGroupsPanelShowsTheWorkingSetAndTheWindow(t *testing.T) {
	a, ws := groupsApp(t)
	if err := ws.Activate("front"); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	view := plain(a.groupsPanel(40, 20))
	if !strings.Contains(view, "front (2/4 hosts)") {
		t.Fatalf("the panel does not describe the working set:\n%s", view)
	}
}

// The chunk keys page the working set, which is not the same thing as paging
// the pane window.
func TestChunkKeysPageTheWorkingSet(t *testing.T) {
	a, ws := groupsApp(t)
	if err := ws.Apply(workingset.Range{From: 1, To: 2}); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	a = pressKey(t, a, "]")
	if got := strings.Join(ws.Members(), ","); got != "db-01,db-02" {
		t.Fatalf("members after the next chunk = %q", got)
	}

	a = pressKey(t, a, "[")
	if got := strings.Join(ws.Members(), ","); got != "web-01,web-02" {
		t.Fatalf("members after the previous chunk = %q", got)
	}
	_ = a
}

func TestGroupCursorMovesAndLeavesThePanelAtTheEnds(t *testing.T) {
	a, _ := groupsApp(t)

	a = pressKey(t, a, "j")
	if a.GroupCursor() != 1 {
		t.Fatalf("GroupCursor() = %d", a.GroupCursor())
	}

	// Off the top is the panel above.
	a = pressKey(t, a, "k")
	a = pressKey(t, a, "k")
	if a.Panel() != PanelHosts {
		t.Fatalf("Panel() = %v after moving off the top", a.Panel())
	}
}

func TestGroupsPanelWithoutAWorkingSet(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "3")

	if got := plain(a.groupsPanel(40, 20)); !strings.Contains(got, "no working sets yet") {
		t.Fatalf("groupsPanel() = %q", got)
	}
	if a.SelectedGroup() != "" {
		t.Fatalf("SelectedGroup() = %q", a.SelectedGroup())
	}
	// Enter and the chunk keys must not panic without a model behind them.
	a = pressKey(t, a, "]")
	a = pressKey(t, a, "[")
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
}
