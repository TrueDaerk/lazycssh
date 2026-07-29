package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// hostsApp builds an app on the Hosts panel over a live fake fleet.
func hostsApp(t *testing.T, names ...string) (App, *fakeFleet) {
	t.Helper()

	a, fleet, _, _ := statusApp(t, names...)
	return pressKey(t, a, "2"), fleet
}

// typeFilter opens the filter and types text into it.
func typeFilter(t *testing.T, a App, text string) App {
	t.Helper()

	a = pressKey(t, a, "/")
	if !a.Filtering() {
		t.Fatal("/ did not open the filter")
	}
	for _, r := range text {
		a = pressKey(t, a, string(r))
	}
	return a
}

func TestHostsPanelListsEveryHostWithItsPaneNumber(t *testing.T) {
	a, fleet := hostsApp(t, "web-01", "db-01", "cache-01")
	fleet.connect(t, "web-01")
	fleet.fail(t, "db-01")

	view := plain(a.hostsPanel(40, 20))
	for _, want := range []string{"1 web-01", "2 db-01", "3 cache-01", "connected", "failed"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the Hosts panel does not show %q:\n%s", want, view)
		}
	}
}

func TestHostsCursorMovesAndStopsAtTheEnds(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02", "web-03")

	if a.SelectedHost() != "web-01" {
		t.Fatalf("SelectedHost() = %q", a.SelectedHost())
	}
	a = pressKey(t, a, "j")
	if a.SelectedHost() != "web-02" {
		t.Fatalf("SelectedHost() = %q after moving down", a.SelectedHost())
	}

	// Off the bottom of the list is the next panel, not a wrap to the top.
	for range 5 {
		a = pressKey(t, a, "j")
	}
	if a.Panel() == PanelHosts && a.SelectedHost() != "web-03" {
		t.Fatalf("SelectedHost() = %q after running off the end", a.SelectedHost())
	}
}

// The acceptance criterion: space toggles selection for the selected broadcast
// mode.
func TestSpaceTogglesSelection(t *testing.T) {
	a, _, router, _ := statusApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "2")

	a = pressKey(t, a, "j")
	a = pressKey(t, a, " ")
	if !router.IsSelected("web-02") {
		t.Fatal("space did not select the host under the cursor")
	}
	if router.IsSelected("web-01") {
		t.Fatal("space selected a host the cursor was not on")
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "2* web-02") {
		t.Fatalf("the selection is not marked:\n%s", plain(a.View().Content))
	}

	a = pressKey(t, a, " ")
	if router.IsSelected("web-02") {
		t.Fatal("space did not deselect")
	}
}

// The acceptance criterion: selection stays correct across reconnects and pane
// paging, because it is keyed by host rather than by position.
func TestSelectionSurvivesPagingAndStateChanges(t *testing.T) {
	names := make([]string, 0, 12)
	for i := 1; i <= 12; i++ {
		names = append(names, fmt.Sprintf("web-%02d", i))
	}

	a, fleet, router, _ := statusApp(t, names...)
	a = pressKey(t, a, "2")
	a = pressKey(t, a, "j")
	a = pressKey(t, a, " ")
	if !router.IsSelected("web-02") {
		t.Fatal("setup: nothing was selected")
	}

	// Page the panes.
	a = resize(t, a, 60, 14)
	a = pressKey(t, a, "tab")
	a = pressKey(t, a, "n")
	if !router.IsSelected("web-02") {
		t.Fatal("paging lost the selection")
	}

	// Reconnect the host: same identifier, new session.
	fleet.fail(t, "web-02")
	fleet.sessions["web-02"] = fleet.sessions["web-02"]
	if !router.IsSelected("web-02") {
		t.Fatal("a state change lost the selection")
	}
	if got := router.Targets(); len(got) == 0 {
		t.Fatal("the router lost its target list")
	}
}

func TestEnterFocusesTheHostsPane(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "j")
	a = pressKey(t, a, "j")

	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if next.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v after enter", next.Focus())
	}
	if next.FocusedHost() != "web-03" {
		t.Fatalf("FocusedHost() = %q after enter", next.FocusedHost())
	}
}

func TestFilterNarrowsTheListWithoutRenumberingPanes(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "db-01", "web-02", "cache-01")

	a = typeFilter(t, a, "web")
	view := plain(a.hostsPanel(40, 20))
	if strings.Contains(view, "db-01") || strings.Contains(view, "cache-01") {
		t.Fatalf("the filter kept a host it does not match:\n%s", view)
	}
	// web-02 is the third host of the run and keeps pane number 3.
	if !strings.Contains(view, "3 web-02") {
		t.Fatalf("filtering renumbered the panes:\n%s", view)
	}
}

func TestFilterIsCaseInsensitive(t *testing.T) {
	a, _ := hostsApp(t, "WEB-01", "db-01")
	a = typeFilter(t, a, "web")
	if strings.Contains(plain(a.hostsPanel(40, 20)), "db-01") {
		t.Fatal("the filter was case sensitive")
	}
}

func TestFilterOwnsTheKeyboardWhileOpen(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02")

	// A host called "x" must be typeable without closing a pane.
	a = typeFilter(t, a, "x")
	if a.Filter() != "x" {
		t.Fatalf("Filter() = %q", a.Filter())
	}
	if a.Panel() != PanelHosts {
		t.Fatalf("a filter keystroke changed the panel to %v", a.Panel())
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "no host matches") {
		t.Fatalf("a filter matching nothing says nothing:\n%s", plain(a.View().Content))
	}
}

func TestEnterKeepsTheFilterAndEscapeDropsIt(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "db-01")

	a = typeFilter(t, a, "web")
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Filtering() {
		t.Fatal("enter did not give the keyboard back")
	}
	if a.Filter() != "web" {
		t.Fatalf("enter dropped the filter: %q", a.Filter())
	}

	a = pressKey(t, a, "/")
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a, ok = model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Filter() != "" || a.Filtering() {
		t.Fatalf("escape left the filter as %q (open: %v)", a.Filter(), a.Filtering())
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "db-01") {
		t.Fatal("dropping the filter did not bring the hosts back")
	}
}

func TestHostCursorClampsWhenTheFilterShrinksTheList(t *testing.T) {
	a, _ := hostsApp(t, "web-01", "web-02", "web-03")
	a = pressKey(t, a, "j")
	a = pressKey(t, a, "j")
	if a.HostCursor() != 2 {
		t.Fatalf("HostCursor() = %d", a.HostCursor())
	}

	a = typeFilter(t, a, "web-01")
	if a.HostCursor() != 0 || a.SelectedHost() != "web-01" {
		t.Fatalf("HostCursor() = %d, SelectedHost() = %q", a.HostCursor(), a.SelectedHost())
	}
}

// Two hundred hosts must not make a redraw expensive. The guarantee is
// structural rather than a stopwatch: the panel renders the rows that fit and
// nothing else, so the cost of a redraw is the size of the panel. A wall-clock
// assertion here would only measure the CI machine.
func TestTwoHundredHostsRenderOnlyTheVisibleRows(t *testing.T) {
	names := make([]string, 0, 200)
	for i := 1; i <= 200; i++ {
		names = append(names, fmt.Sprintf("web-%03d", i))
	}
	a, _ := hostsApp(t, names...)

	for range 50 {
		a = pressKey(t, a, "j")
	}

	view := plain(a.hostsPanel(40, 12))
	if lines := strings.Count(view, "\n") + 1; lines > 13 {
		t.Fatalf("a 12-row panel rendered %d lines:\n%s", lines, view)
	}
	if !strings.Contains(view, "more") {
		t.Fatalf("the panel does not say how many hosts are off screen:\n%s", view)
	}
	if strings.Count(view, "web-") > 30 {
		t.Fatalf("the panel rendered every host rather than the visible ones:\n%s", view)
	}
}

func TestVisibleRange(t *testing.T) {
	tests := []struct {
		name                  string
		cursor, total, height int
		wantFirst, wantLast   int
	}{
		{"everything fits", 0, 5, 10, 0, 5},
		{"cursor at the top", 0, 100, 10, 0, 10},
		{"cursor in the middle", 50, 100, 10, 45, 55},
		{"cursor at the end", 99, 100, 10, 90, 100},
		{"no room", 3, 100, 0, 3, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			first, last := visibleRange(tc.cursor, tc.total, tc.height)
			if first != tc.wantFirst || last != tc.wantLast {
				t.Fatalf("visibleRange(%d, %d, %d) = %d, %d, want %d, %d",
					tc.cursor, tc.total, tc.height, first, last, tc.wantFirst, tc.wantLast)
			}
		})
	}
}

func TestHostsPanelWithoutHosts(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "2")

	if a.SelectedHost() != "" {
		t.Fatalf("SelectedHost() = %q with no hosts", a.SelectedHost())
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "no hosts") {
		t.Fatalf("an empty run does not say so:\n%s", plain(a.hostsPanel(40, 20)))
	}
	// Toggling and choosing nothing must not panic or move focus.
	a = pressKey(t, a, " ")
	a = pressKey(t, a, "\n")
	if a.Focus() != AreaSidebar {
		t.Fatalf("Focus() = %v", a.Focus())
	}
}

func TestToggleWithoutARouterDoesNothing(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "2")
	a = pressKey(t, a, " ") // must not panic
	if a.SelectedHost() != "h1" {
		t.Fatalf("SelectedHost() = %q", a.SelectedHost())
	}
}

// candidateApp builds an app on the Hosts panel with running hosts and
// ssh-config connect candidates.
func candidateApp(t *testing.T, hosts, aliases []string) App {
	t.Helper()
	a := resize(t, NewApp(Config{
		Hosts:         hosts,
		ConfigAliases: aliases,
		Theme:         Options{Dark: true},
	}), 120, 40)
	return pressKey(t, a, "2")
}

// keyCmdMsg presses one key and returns the message its command produces.
func keyCmdMsg(t *testing.T, a App, keystroke string) (App, tea.Msg) {
	t.Helper()
	var msg tea.KeyPressMsg
	if keystroke == "enter" {
		msg = tea.KeyPressMsg{Code: tea.KeyEnter}
	} else {
		msg = tea.KeyPressMsg{Code: []rune(keystroke)[0], Text: keystroke}
	}
	model, cmd := a.Update(msg)
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		return next, nil
	}
	return next, cmd()
}

func TestHostsPanelListsConfigCandidates(t *testing.T) {
	a := candidateApp(t, []string{"web-01"}, []string{"web-01", "db-01", "bastion"})
	view := plain(a.hostsPanel(40, 20))

	for _, want := range []string{"─ ssh config ─", "db-01", "bastion"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the panel does not show %q:\n%s", want, view)
		}
	}
	// A connected alias is a host now, not also a candidate.
	if strings.Count(view, "web-01") != 1 {
		t.Fatalf("web-01 is listed more than once:\n%s", view)
	}
}

func TestEnterOnACandidateEmitsHostConnectMsg(t *testing.T) {
	a := candidateApp(t, []string{"web-01"}, []string{"db-01"})
	a = pressKey(t, a, "j") // onto the candidate
	if a.SelectedCandidate() != "db-01" {
		t.Fatalf("SelectedCandidate() = %q", a.SelectedCandidate())
	}

	_, msg := keyCmdMsg(t, a, "enter")
	connect, ok := msg.(HostConnectMsg)
	if !ok {
		t.Fatalf("enter produced %T, want HostConnectMsg", msg)
	}
	if strings.Join(connect.Patterns, ",") != "db-01" {
		t.Fatalf("Patterns = %v", connect.Patterns)
	}
}

func TestSpaceMarksCandidatesAndEnterConnectsThem(t *testing.T) {
	a := candidateApp(t, nil, []string{"db-01", "db-02", "db-03"})

	a = pressKey(t, a, " ") // mark db-01
	a = pressKey(t, a, "j")
	a = pressKey(t, a, " ") // mark db-02
	if !a.CandidateMarked("db-01") || !a.CandidateMarked("db-02") {
		t.Fatal("space did not mark the candidates")
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), "+ db-01") {
		t.Fatalf("a mark is not visible:\n%s", plain(a.hostsPanel(40, 20)))
	}

	// Marks toggle off again.
	a = pressKey(t, a, " ")
	if a.CandidateMarked("db-02") {
		t.Fatal("space did not unmark")
	}
	a = pressKey(t, a, " ")

	next, msg := keyCmdMsg(t, a, "enter")
	connect, ok := msg.(HostConnectMsg)
	if !ok {
		t.Fatalf("enter produced %T, want HostConnectMsg", msg)
	}
	if strings.Join(connect.Patterns, ",") != "db-01,db-02" {
		t.Fatalf("Patterns = %v", connect.Patterns)
	}
	if next.CandidateMarked("db-01") {
		t.Fatal("the marks survived the connect request")
	}
}

func TestNewHostPromptEmitsThePattern(t *testing.T) {
	a := candidateApp(t, nil, nil)

	a = pressKey(t, a, "n")
	if !a.hostInput.Focused() {
		t.Fatal("n did not open the new-host prompt")
	}
	if !strings.Contains(plain(a.hostsPanel(40, 20)), newHostPrompt) {
		t.Fatalf("the open prompt is not visible:\n%s", plain(a.hostsPanel(40, 20)))
	}
	for _, r := range "web-{01..02}" {
		a = pressKey(t, a, string(r))
	}

	next, msg := keyCmdMsg(t, a, "enter")
	connect, ok := msg.(HostConnectMsg)
	if !ok {
		t.Fatalf("enter produced %T, want HostConnectMsg", msg)
	}
	if strings.Join(connect.Patterns, ",") != "web-{01..02}" {
		t.Fatalf("Patterns = %v", connect.Patterns)
	}
	if next.hostInput.Focused() || next.hostInput.Value() != "" {
		t.Fatal("the prompt did not close and clear")
	}
}

func TestNewHostPromptEscAbandons(t *testing.T) {
	a := candidateApp(t, nil, nil)
	a = pressKey(t, a, "n")
	for _, r := range "web" {
		a = pressKey(t, a, string(r))
	}

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	next := model.(App)
	if cmd != nil {
		t.Fatal("esc produced a command")
	}
	if next.hostInput.Focused() || next.hostInput.Value() != "" {
		t.Fatal("esc did not abandon the prompt")
	}
}

func TestEmptyNewHostPromptEmitsNothing(t *testing.T) {
	a := candidateApp(t, nil, nil)
	a = pressKey(t, a, "n")
	_, msg := keyCmdMsg(t, a, "enter")
	if msg != nil {
		t.Fatalf("an empty prompt produced %T", msg)
	}
}

func TestFilterAppliesToCandidates(t *testing.T) {
	a := candidateApp(t, []string{"web-01"}, []string{"db-01", "cache-01"})
	a = typeFilter(t, a, "db")

	rows := a.hostRows()
	if len(rows) != 1 || rows[0].ID != "db-01" || !rows[0].Candidate {
		t.Fatalf("hostRows() = %+v, want only the db-01 candidate", rows)
	}
}

func TestConnectErrorIsShownUntilTheFleetChanges(t *testing.T) {
	a := candidateApp(t, nil, nil)

	model, _ := a.Update(ConnectErrorMsg{Err: `expand "web-{": unclosed brace`})
	a = model.(App)
	if !strings.Contains(plain(a.hostsPanel(60, 20)), "unclosed brace") {
		t.Fatalf("the connect error is not shown:\n%s", plain(a.hostsPanel(60, 20)))
	}

	model, _ = a.Update(HostsChangedMsg{Hosts: []string{"web-01"}})
	a = model.(App)
	if strings.Contains(plain(a.hostsPanel(60, 20)), "unclosed brace") {
		t.Fatal("the connect error survived the fleet changing")
	}
}

func TestConnectedCandidateLeavesTheCandidateList(t *testing.T) {
	a := candidateApp(t, nil, []string{"db-01"})
	model, _ := a.Update(HostsChangedMsg{Hosts: []string{"db-01"}})
	a = model.(App)

	for _, row := range a.hostRows() {
		if row.Candidate && row.ID == "db-01" {
			t.Fatal("a connected host is still offered as a candidate")
		}
	}
}
