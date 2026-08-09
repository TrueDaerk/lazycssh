package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// exitApp builds an app whose command line really sends over a fleet of fakes:
// everything the per-command exit indicator needs to be driven end to end.
func exitApp(t *testing.T, names ...string) (App, *fakeFleet, *broadcast.Router) {
	t.Helper()

	fleet := newFakeFleet(names...)
	ws := workingset.New(fleet.IDs())
	router, err := broadcast.NewRouter(ws)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	sender := &fakeSender{delivery: broadcast.Delivery{
		Mode: broadcast.ModeAll, Scope: len(names), Targets: len(names), Delivered: len(names),
	}}

	a := resize(t, NewApp(Config{
		SessionName: "prod-web",
		Fleet:       fleet,
		Targets:     router,
		WorkingSet:  ws,
		Sender:      sender,
		Panes:       fleet,
		Theme:       Options{Dark: true},
	}), 200, 60)

	return a, fleet, router
}

// armHook drives fakes to where a hooked shell stands right after login:
// connected, with the marker its first prompt printed already seen. That first
// marker is what proves the hook works; it says nothing about a command.
func armHook(t *testing.T, fleet *fakeFleet, ids ...string) {
	t.Helper()
	for _, id := range ids {
		fleet.connect(t, id)
		fleet.sessions[id].ReportExit(0)
	}
}

// The acceptance criterion: after a command sent from the command line, each
// pane header says how that command ended on that host - and the answer to the
// previous question is cleared the moment a new one goes out.
func TestPaneHeaderShowsTheCommandExitStatus(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01")
	armHook(t, fleet, "web-01")
	a = syncFleet(t, a)

	// The login prompt's own marker is not a command's answer.
	if got := plain(a.paneHeader(0, 40, false)); strings.ContainsAny(got, exitOKMark+exitRunningMark) {
		t.Fatalf("a status was invented before any command was sent: %q", got)
	}

	a = sendVia(t, a, "deploy")
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, exitRunningMark) {
		t.Fatalf("the command is out and the header does not say so: %q", got)
	}

	fleet.sessions["web-01"].ReportExit(0)
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, exitOKMark) {
		t.Fatalf("a success is not shown: %q", got)
	}

	// A second command greys the answer to the first one out again.
	a = sendVia(t, a, "restart")
	if got := plain(a.paneHeader(0, 40, false)); strings.Contains(got, exitOKMark) {
		t.Fatalf("the previous command's answer survived a new send: %q", got)
	}

	fleet.sessions["web-01"].ReportExit(2)
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, "exit 2") {
		t.Fatalf("a failure is not shown: %q", got)
	}
}

// A repeated code still ends the command: the marker sequence moves even when
// the number does not, so a second `false` reads as answered, not as running.
func TestRepeatedExitCodeStillAnswers(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01")
	armHook(t, fleet, "web-01")
	a = syncFleet(t, a)

	for range 2 {
		a = sendVia(t, a, "false")
		fleet.sessions["web-01"].ReportExit(1)
		a = syncFleet(t, a)
		if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, "exit 1") {
			t.Fatalf("the repeat did not answer: %q", got)
		}
	}
}

// The acceptance criterion: a shell that never emits the marker shows no
// indicator at all - not a green tick, and not a dot that never resolves.
func TestNoIndicatorWithoutTheHook(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01")
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)

	a = sendVia(t, a, "deploy")
	got := plain(a.paneHeader(0, 40, false))
	if strings.ContainsAny(got, exitOKMark+exitRunningMark) || strings.Contains(got, "exit") {
		t.Fatalf("a hookless shell produced an indicator: %q", got)
	}

	// A host that only proves its hook later is answered from that marker on:
	// the send's mark was zero, and the first marker past it is the answer.
	fleet.sessions["web-01"].ReportExit(3)
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, "exit 3") {
		t.Fatalf("a late hook was ignored: %q", got)
	}
}

// The acceptance criterion: raw keystrokes are not command-line sends, so they
// claim nothing. lazycssh cannot tell where a command starts in a stream of
// typing, and a marker arriving out of a typed line answers no question it
// asked.
func TestRawKeystrokesProduceNoIndicator(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01")
	armHook(t, fleet, "web-01")
	a = syncFleet(t, a)

	// Type into the focused pane, enter included: the keystroke path, not the
	// command line.
	a = focusGrid(t, a)
	for _, k := range []string{"f", "a", "l", "s", "e", "enter"} {
		a = pressKey(t, a, k)
	}
	if got := fleet.sessions["web-01"].Written(); !strings.Contains(got, "false") {
		t.Fatalf("the keystrokes never reached the host: %q", got)
	}
	fleet.sessions["web-01"].ReportExit(1)
	a = syncFleet(t, a)

	if got := plain(a.paneHeader(0, 40, false)); strings.Contains(got, "exit") {
		t.Fatalf("raw typing produced a command status: %q", got)
	}
	if a.failureSummary() != "" {
		t.Fatalf("raw typing was counted as a failed command: %q", plain(a.failureSummary()))
	}
}

// The mark is taken before the command's bytes leave. A host that answers
// while the send is still in flight would otherwise have its own answer
// counted as the state the send found, and the pane would sit on the dot until
// some later prompt happened to produce another marker.
func TestMarkIsTakenBeforeTheSend(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01")
	armHook(t, fleet, "web-01")
	a = syncFleet(t, a)

	sender, ok := a.cfg.Sender.(*fakeSender)
	if !ok {
		t.Fatalf("the sender is a %T", a.cfg.Sender)
	}
	sender.onSend = func() { fleet.sessions["web-01"].ReportExit(7) }

	a = sendVia(t, a, "deploy")
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, "exit 7") {
		t.Fatalf("an answer that arrived during the send was swallowed: %q", got)
	}
}

// A host outside the send has no answer to that command: it keeps no mark, so
// its pane says nothing rather than showing what an older question produced.
func TestOnlyTheHostsTheCommandReachedAreMarked(t *testing.T) {
	a, fleet, router := exitApp(t, "web-01", "web-02")
	armHook(t, fleet, "web-01", "web-02")
	a = syncFleet(t, a)

	router.Toggle("web-01")
	if err := router.SetMode(broadcast.ModeSelected); err != nil {
		t.Fatalf("SetMode: %v", err)
	}

	a = sendVia(t, a, "deploy")
	fleet.sessions["web-01"].ReportExit(0)
	fleet.sessions["web-02"].ReportExit(1)
	a = syncFleet(t, a)

	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, exitOKMark) {
		t.Fatalf("the target's answer is missing: %q", got)
	}
	if got := plain(a.paneHeader(1, 40, false)); strings.Contains(got, "exit") {
		t.Fatalf("a host the command never reached reported one: %q", got)
	}
}

// A host the send could not reach keeps no mark either: the status bar already
// says how many missed the command, and a stale tick would say they answered.
func TestUndeliveredHostIsNotMarked(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01", "web-02")
	armHook(t, fleet, "web-01", "web-02")
	a = syncFleet(t, a)

	sender, ok := a.cfg.Sender.(*fakeSender)
	if !ok {
		t.Fatalf("the sender is a %T", a.cfg.Sender)
	}
	sender.delivery = broadcast.Delivery{
		Mode: broadcast.ModeAll, Scope: 2, Targets: 2, Delivered: 1, Failed: []string{"web-02"},
	}

	a = sendVia(t, a, "deploy")
	fleet.sessions["web-02"].ReportExit(1)
	a = syncFleet(t, a)

	if got := plain(a.paneHeader(1, 40, false)); strings.Contains(got, "exit") {
		t.Fatalf("a host that never received the command reported one: %q", got)
	}
}

// A reconnect starts a new shell with its own marker counter. The mark belongs
// to the session that died, so the indicator clears instead of comparing
// against a smaller number and reading as "still running" forever.
func TestReconnectClearsTheIndicator(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01")
	armHook(t, fleet, "web-01")
	a = syncFleet(t, a)

	a = sendVia(t, a, "deploy")
	fleet.sessions["web-01"].ReportExit(1)
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); !strings.Contains(got, "exit 1") {
		t.Fatalf("the failure is not shown: %q", got)
	}

	fleet.sessions["web-01"] = ssh.NewFake("web-01", hosts.Host{Alias: "web-01", Addr: "web-01"}, nil)
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)
	if got := plain(a.paneHeader(0, 40, false)); strings.Contains(got, "exit") ||
		strings.Contains(got, exitRunningMark) {
		t.Fatalf("the dead session's status survived the reconnect: %q", got)
	}
}

// When the header is too narrow for everything, the state goes before the exit
// code: a failure must outlive the state label.
func TestPaneHeaderKeepsTheFailureOverTheState(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-production-01")
	armHook(t, fleet, "web-production-01")
	a = syncFleet(t, a)
	a = sendVia(t, a, "deploy")
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
	a, fleet, _ := exitApp(t, "web-01", "web-02")
	armHook(t, fleet, "web-01", "web-02")
	a = syncFleet(t, a)
	a = sendVia(t, a, "deploy")
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

// A command still out is not a failure: the pane keeps its ordinary frame and
// the status bar counts nothing until the shell answers.
func TestRunningCommandIsNotAFailure(t *testing.T) {
	a, fleet, _ := exitApp(t, "web-01")
	armHook(t, fleet, "web-01")
	a = syncFleet(t, a)

	a = sendVia(t, a, "deploy")
	if a.commandFailed("web-01") {
		t.Fatal("a command that has not answered counts as failed")
	}
	if a.failureSummary() != "" {
		t.Fatalf("a running command was counted: %q", plain(a.failureSummary()))
	}
}

// The status bar summary: the acceptance criterion's "3 hosts failed".
func TestStatusBarCountsFailedHosts(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = fmt.Sprintf("web-%02d", i+1)
	}
	a, fleet, _ := exitApp(t, names...)
	// All twenty panes on one page: paging limits the broadcast to what is
	// visible, and this test is about the count, not about the limit.
	a = resize(t, a, 240, 100)
	armHook(t, fleet, names...)
	a = syncFleet(t, a)

	if strings.Contains(plain(a.View().Content), "failed") {
		t.Fatalf("a clean run reports failures:\n%s", plain(a.View().Content))
	}

	a = sendVia(t, a, "deploy")
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
	a, fleet, _ := exitApp(t, names...)
	// One page, so the send reaches every host: the broadcast limit follows
	// the visible set, and the jump is what is under test here.
	a = resize(t, a, 240, 100)
	armHook(t, fleet, names...)
	a = syncFleet(t, a)
	a = sendVia(t, a, "deploy")
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
	if status, _ := a.commandExit("h1"); status != exitUnknown {
		t.Fatal("an exit status was invented without a transport")
	}
	if a.exitLabel("h1") != "" {
		t.Fatal("an exit label was invented without a transport")
	}
	if a.failureSummary() != "" {
		t.Fatal("a failure summary was invented without a transport")
	}
	if a.liveExitSeq("h1") != 0 {
		t.Fatal("a marker sequence was invented without a transport")
	}
}
