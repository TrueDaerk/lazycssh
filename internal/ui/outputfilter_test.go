package ui

import (
	"slices"
	"strings"
	"testing"
)

// filterApp builds a four-host run with room for every pane and hands back the
// fleet to emit output on.
func filterApp(t *testing.T) (App, *fakeFleet) {
	t.Helper()
	a, _, _, _ := cmdApp(t, "web-01", "web-02", "db-01", "db-02")
	a = resize(t, a, 220, 70)
	return a, a.cfg.Fleet.(*fakeFleet)
}

// applyFilter opens the filter prompt, types a pattern and applies it.
func applyFilter(t *testing.T, a App, pattern string) App {
	t.Helper()
	a = pressKey(t, a, "f")
	if !a.FilterPromptOpen() {
		t.Fatal("f did not open the filter prompt")
	}
	for _, r := range pattern {
		a = pressKey(t, a, string(r))
	}
	a, _ = enter(t, a)
	if a.FilterPromptOpen() {
		t.Fatal("the filter prompt stayed open after enter")
	}
	if a.OutputFilter() != pattern {
		t.Fatalf("OutputFilter() = %q, want %q", a.OutputFilter(), pattern)
	}
	return a
}

func TestFilterShowsOnlyTheMatchingPanes(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("error: disk full\r\n")
	fleet.sessions["web-02"].Emit("all good\r\n")
	fleet.sessions["db-01"].Emit("ERROR: no space left\r\n")
	fleet.sessions["db-02"].Emit("all good\r\n")

	a = applyFilter(t, a, "error")

	want := []string{"web-01", "db-01"}
	if got := a.hostIDs(); !slices.Equal(got, want) {
		t.Fatalf("the grid shows %v, want %v", got, want)
	}
	// The layout re-tiles for the matches: two panes, not four.
	if got := a.Grid().PerPage; got != 2 {
		t.Fatalf("the grid tiled %d panes per page, want 2", got)
	}
}

func TestFilterMatchesCaseInsensitiveSubstrings(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("Connection REFUSED\r\n")
	fleet.sessions["web-02"].Emit("connected\r\n")

	// Uppercase pattern, mixed-case output, and a substring in the middle of a
	// word - all three match; a regexp metacharacter is a literal.
	a = applyFilter(t, a, "REFUSED")
	if got := a.hostIDs(); !slices.Equal(got, []string{"web-01"}) {
		t.Fatalf("the grid shows %v, want [web-01]", got)
	}

	a = applyFilter(t, a, "refus")
	if got := a.hostIDs(); !slices.Equal(got, []string{"web-01"}) {
		t.Fatalf("a lowercase substring shows %v, want [web-01]", got)
	}

	a = applyFilter(t, a, "conn.cted")
	if got := a.hostIDs(); len(got) != 0 {
		t.Fatalf("the pattern was treated as a regexp: %v", got)
	}
}

func TestFilterStatusBarSaysWhatIsHidden(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("error: disk full\r\n")
	fleet.sessions["web-02"].Emit("all good\r\n")

	a = applyFilter(t, a, "error")

	bar := plain(a.renderStatusBar())
	if !strings.Contains(bar, `filter: "error" (1/4)`) {
		t.Fatalf("the status bar does not carry the filter:\n%s", bar)
	}
	if !a.overflowFooterVisible() {
		t.Fatal("the overflow footer is not drawn while panes are filtered out")
	}
	if footer := plain(a.overflowFooter()); !strings.Contains(footer, "+3 hosts hidden by the filter") {
		t.Fatalf("the footer does not say what is hidden:\n%s", footer)
	}
}

// The filter is a view. Hiding a pane must not narrow what a keystroke reaches:
// the whole point of the tool is that the target count is trustworthy.
func TestFilterDoesNotChangeTheBroadcastSet(t *testing.T) {
	a, sender, router, _ := cmdApp(t, "web-01", "web-02", "db-01", "db-02")
	a = resize(t, a, 220, 70)
	fleet := a.cfg.Fleet.(*fakeFleet)
	for _, id := range fleet.IDs() {
		fleet.connect(t, id)
	}
	a = syncFleet(t, a)

	before := router.Count()
	if before != 4 {
		t.Fatalf("the run starts with %d targets, want 4", before)
	}
	fleet.sessions["web-01"].Emit("error: disk full\r\n")

	a = applyFilter(t, a, "error")

	if got := a.hostIDs(); len(got) != 1 {
		t.Fatalf("the filter shows %v, want one pane", got)
	}
	if got := router.Count(); got != before {
		t.Fatalf("the filter changed the broadcast set: %d targets, want %d", got, before)
	}
	if got := len(router.Targets()); got != before {
		t.Fatalf("the filter changed the targets: %d, want %d", got, before)
	}

	// And a command actually sent under the filter reaches every host, not
	// only the visible one.
	a = sendVia(t, a, "uptime")
	if got := sender.sent; !slices.Equal(got, []string{"uptime\n"}) {
		t.Fatalf("sent %q", got)
	}
	if got := router.Count(); got != before {
		t.Fatalf("after the send the broadcast set is %d, want %d", got, before)
	}
}

func TestClearingTheFilterRestoresTheGrid(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["db-02"].Emit("error: disk full\r\n")

	// The focus starts on the first pane, which the filter hides.
	if got := a.FocusedHost(); got != "web-01" {
		t.Fatalf("the run starts focused on %q, want web-01", got)
	}

	a = applyFilter(t, a, "error")
	if got := a.FocusedHost(); got != "db-02" {
		t.Fatalf("the focus stayed on a hidden pane: %q", got)
	}

	// Enter on an empty prompt is the promised way out.
	a = applyFilter(t, a, "")
	if got := len(a.hostIDs()); got != 4 {
		t.Fatalf("clearing the filter left %d panes, want 4", got)
	}
	if got := a.Grid().PerPage; got != 4 {
		t.Fatalf("the grid tiled %d panes per page after clearing, want 4", got)
	}
	if got := a.FocusedHost(); got != "db-02" {
		t.Fatalf("clearing the filter moved the focus to %q, want db-02", got)
	}
}

func TestEscClearsTheFilter(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("error: disk full\r\n")
	a = applyFilter(t, a, "error")

	a = pressKey(t, a, "f")
	a = pressKey(t, a, "x")
	a = pressKey(t, a, "esc")

	if a.FilterPromptOpen() {
		t.Fatal("esc left the filter prompt open")
	}
	if a.OutputFilter() != "" {
		t.Fatalf("esc left the filter %q in force", a.OutputFilter())
	}
	if got := len(a.hostIDs()); got != 4 {
		t.Fatalf("esc left %d panes on screen, want 4", got)
	}
}

// The prompt owns the keyboard while it is open: a pattern holding a "b" must
// not switch the broadcast mode.
func TestFilterPromptOwnsTheKeyboard(t *testing.T) {
	a, _, router, _ := cmdApp(t, "web-01", "web-02")
	mode := router.Mode()

	a = pressKey(t, a, "f")
	a = pressKey(t, a, "b")
	if router.Mode() != mode {
		t.Fatalf("a typed b switched the mode to %v", router.Mode())
	}
	a, _ = enter(t, a)
	if a.OutputFilter() != "b" {
		t.Fatalf("OutputFilter() = %q, want b", a.OutputFilter())
	}
}

// New output re-evaluates the matches: a host that starts printing the pattern
// joins the filtered grid without a keypress.
func TestFilterFollowsNewOutput(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("error: disk full\r\n")
	a = applyFilter(t, a, "error")
	if got := a.hostIDs(); !slices.Equal(got, []string{"web-01"}) {
		t.Fatalf("the grid shows %v, want [web-01]", got)
	}

	fleet.sessions["db-01"].Emit("error: no route to host\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "db-01"})
	a = model.(App)

	if got := a.hostIDs(); !slices.Equal(got, []string{"web-01", "db-01"}) {
		t.Fatalf("new output did not re-evaluate the filter: %v", got)
	}
	if got := a.Grid().PerPage; got != 2 {
		t.Fatalf("the grid tiled %d panes per page, want 2", got)
	}
}

// The window is the output since the last send, the same one the Output diff
// compares: an error from before the command must not keep a pane on screen.
func TestFilterMatchesTheOutputSinceTheLastCommand(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("error: from an hour ago\r\n")
	fleet.sessions["web-02"].Emit("quiet\r\n")

	a = applyFilter(t, a, "error")
	if got := a.hostIDs(); !slices.Equal(got, []string{"web-01"}) {
		t.Fatalf("before the send the grid shows %v, want [web-01]", got)
	}

	// The send opens a new window; nobody has answered in it yet.
	a = sendVia(t, a, "uptime")
	if got := a.hostIDs(); len(got) != 0 {
		t.Fatalf("stale output survived the send: %v", got)
	}

	fleet.sessions["db-01"].Emit("error: no space left\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "db-01"})
	a = model.(App)
	if got := a.hostIDs(); !slices.Equal(got, []string{"db-01"}) {
		t.Fatalf("the answer to the command shows %v, want [db-01]", got)
	}
}

// A host that leaves the run while a filter is in force must not leave the
// focus pointing at nothing.
func TestFilterKeepsTheFocusOnAVisiblePane(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("error: disk full\r\n")
	fleet.sessions["db-01"].Emit("error: no space left\r\n")
	a = applyFilter(t, a, "error")

	// Move onto the second match, then let its output scroll out of the
	// matching window by starting a new command window.
	a = pressKey(t, a, "alt+shift+right")
	if got := a.FocusedHost(); got != "db-01" {
		t.Fatalf("the focus is on %q, want db-01", got)
	}
	// The pane chord entered the pane's terminal; the command line is an
	// app-level key, so step back out before typing one.
	a = pressKey(t, a, "ctrl+]")

	a = sendVia(t, a, "uptime")
	fleet.sessions["web-02"].Emit("error: refused\r\n")
	model, _ := a.Update(SessionOutputMsg{ID: "web-02"})
	a = model.(App)

	if got := a.hostIDs(); !slices.Equal(got, []string{"web-02"}) {
		t.Fatalf("the grid shows %v, want [web-02]", got)
	}
	if got := a.FocusedHost(); got != "web-02" {
		t.Fatalf("the focus is on %q, want the one visible pane web-02", got)
	}
}

func TestFilterMatchesEveryHostWhenItIsOff(t *testing.T) {
	a, fleet := filterApp(t)
	fleet.sessions["web-01"].Emit("anything\r\n")
	if !a.FilterMatches("db-02") {
		t.Fatal("a silent host does not match with the filter off")
	}
	if a.filterHidden() != 0 {
		t.Fatalf("filterHidden() = %d with the filter off", a.filterHidden())
	}
	if label := a.filterLabel(); label != "" {
		t.Fatalf("filterLabel() = %q with the filter off", label)
	}
}
