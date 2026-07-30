package ui

import (
	"fmt"
	"strings"
	"testing"
)

// The header carries the exit code: "ok" after a success, "exit N" after a
// failure, nothing while nothing has been reported.
func TestPaneHeaderShowsTheExitCode(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")

	if got := plain(a.paneHeader(0, 40, false)); strings.Contains(got, "exit") || strings.Contains(got, "ok") {
		t.Fatalf("an exit status was invented: %q", got)
	}

	fleet.sessions["web-01"].ReportExit(0)
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, " ok") {
		t.Fatalf("a success is not shown: %q", got)
	}

	fleet.sessions["web-01"].ReportExit(2)
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, " exit 2") {
		t.Fatalf("a failure is not shown: %q", got)
	}
}

// When the header is too narrow for everything, the state goes before the exit
// code: a failure must outlive the state label.
func TestPaneHeaderKeepsTheFailureOverTheState(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-production-01")
	fleet.connect(t, "web-production-01")
	fleet.sessions["web-production-01"].ReportExit(1)
	a = syncFleet(t, a)

	// A width the header cannot fit everything into: the old pane floor,
	// narrower than any pane the grid tiles today.
	got := plain(a.paneHeader(0, 22, false))
	if strings.Contains(got, "connected") {
		t.Fatalf("the state crowded out the failure: %q", got)
	}
	if !strings.Contains(got, "exit 1") {
		t.Fatalf("the failure is gone: %q", got)
	}
}

// The failing pane's frame turns the danger colour, so it reads at a glance
// across the grid - and the header still says it in text, because colour alone
// carries no meaning.
func TestFailingPaneIsVisuallyDistinct(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	// Two panes at the 45x16 floor need more room than statusApp's default.
	a = resize(t, a, 200, 60)
	fleet.sessions["web-02"].ReportExit(1)
	a = syncFleet(t, a)

	healthy := a.theme.PaneFrame(false, false).GetBorderTopForeground()
	failing := a.theme.PaneFrame(false, true).GetBorderTopForeground()
	if healthy == failing {
		t.Fatal("a failing pane's border is not distinct")
	}

	view := a.View().Content
	if !strings.Contains(plain(view), "exit 1") {
		t.Fatalf("the failure is not stated in text:\n%s", plain(view))
	}
}

// The status bar summary: the acceptance criterion's "3 hosts failed".
func TestStatusBarCountsFailedHosts(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a, fleet, _, _ := statusApp(t, names...)

	if strings.Contains(plain(a.View().Content), "failed") {
		t.Fatalf("a clean run reports failures:\n%s", plain(a.View().Content))
	}

	for _, id := range []string{"web-05", "web-12", "web-17"} {
		fleet.sessions[id].ReportExit(1)
	}
	a = syncFleet(t, a)
	if view := plain(a.View().Content); !strings.Contains(view, "3 hosts failed") {
		t.Fatalf("the status bar does not count the failures:\n%s", view)
	}
}

// The acceptance criterion: a command failing on 3 of 20 hosts makes those 3
// findable in one keystroke each, from anywhere, wrapping around.
func TestJumpToNextFailure(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a, fleet, _, _ := statusApp(t, names...)
	for _, id := range []string{"web-05", "web-12", "web-17"} {
		fleet.sessions[id].ReportExit(1)
	}
	a = syncFleet(t, a)

	for _, want := range []int{4, 11, 16, 4} {
		// The jump lands in the pane's terminal; ! is an app-level command,
		// so each jump starts from the app level.
		if a.Focus() == AreaGrid {
			a = pressKey(t, a, "ctrl+]")
		}
		a = pressKey(t, a, "!")
		if a.paneIndex != want {
			t.Fatalf("the jump landed on pane %d, want %d", a.paneIndex, want)
		}
		if a.focus != AreaGrid {
			t.Fatal("the jump did not focus the grid")
		}
		if got := a.Page(); got != a.grid().Page(want) {
			t.Fatalf("the jump left the window on page %d, the pane is on %d", got, a.grid().Page(want))
		}
	}
}

func TestJumpWithNothingFailing(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01", "web-02")
	before := a.paneIndex
	a = pressKey(t, a, "!")
	if a.paneIndex != before {
		t.Fatalf("the jump moved with nothing failing: %d", a.paneIndex)
	}
}

func TestExitHelpersWithoutAFleet(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	if _, ok := a.lastExit("h1"); ok {
		t.Fatal("an exit status was invented without a transport")
	}
	if a.exitLabel("h1") != "" {
		t.Fatal("an exit label was invented without a transport")
	}
	if a.failureSummary() != "" {
		t.Fatal("a failure summary was invented without a transport")
	}
}
