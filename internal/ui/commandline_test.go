package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/commandlog"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// fakeSender records what the command line sent and what it reported.
type fakeSender struct {
	sent     []string
	delivery broadcast.Delivery
	err      error
}

func (f *fakeSender) Send(p []byte) (broadcast.Delivery, error) {
	f.sent = append(f.sent, string(p))
	return f.delivery, f.err
}

// cmdApp builds an app with a router, a sender and a command log.
func cmdApp(t *testing.T, names ...string) (App, *fakeSender, *broadcast.Router, *commandlog.Log) {
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
	log := commandlog.New(0)

	a := resize(t, NewApp(Config{
		Fleet:      fleet,
		Targets:    router,
		WorkingSet: ws,
		Sender:     sender,
		Recorder:   log,
		Panes:      fleet,
		CommandLog: log,
		Theme:      Options{Dark: true},
	}), 120, 40)

	return a, sender, router, log
}

// typeCommand opens the command line and types into it.
func typeCommand(t *testing.T, a App, text string) App {
	t.Helper()

	a = pressKey(t, a, ":")
	if !a.CommandLineOpen() {
		t.Fatal(": did not open the command line")
	}
	for _, r := range text {
		a = pressKey(t, a, string(r))
	}
	return a
}

// enter drives an enter key press through the model.
func enter(t *testing.T, a App) (App, tea.Cmd) {
	t.Helper()

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	return next, cmd
}

func TestCommandLineSendsToTheBroadcastSet(t *testing.T) {
	a, sender, _, log := cmdApp(t, "web-01", "web-02", "web-03")

	a = typeCommand(t, a, "uptime")
	if a.CommandLineValue() != "uptime" {
		t.Fatalf("CommandLineValue() = %q", a.CommandLineValue())
	}

	a, cmd := enter(t, a)
	if a.CommandLineOpen() {
		t.Fatal("the command line stayed open after sending")
	}
	if len(sender.sent) != 1 || sender.sent[0] != "uptime\n" {
		t.Fatalf("sent = %q", sender.sent)
	}

	// The audit trail gets one entry, with the count it reached.
	entry, ok := log.Last()
	if !ok || entry.Command != "uptime" || entry.Targets != 3 {
		t.Fatalf("log entry = %+v (%v)", entry, ok)
	}

	if cmd == nil {
		t.Fatal("sending produced no message")
	}
	msg, ok := cmd().(CommandSentMsg)
	if !ok {
		t.Fatalf("sending produced a %T", cmd())
	}
	if msg.Command != "uptime" || msg.Delivery.Delivered != 3 {
		t.Fatalf("CommandSentMsg = %+v", msg)
	}
}

// cmd+arrow edits the line like any macOS input (issue #202): super+left jumps
// to the start, super+right back to the end.
func TestCommandLineNavigatesWithCmdArrows(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01")

	a = typeCommand(t, a, "grep err")
	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModSuper})
	a = typeMore(t, a, "z")
	if a.CommandLineValue() != "zgrep err" {
		t.Fatalf("CommandLineValue() = %q after super+left, want %q", a.CommandLineValue(), "zgrep err")
	}

	a = press(t, a, tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModSuper})
	a = typeMore(t, a, "s")
	if a.CommandLineValue() != "zgrep errs" {
		t.Fatalf("CommandLineValue() = %q after super+right, want %q", a.CommandLineValue(), "zgrep errs")
	}
}

// typeMore types into the already-open command line without reopening it.
func typeMore(t *testing.T, a App, text string) App {
	t.Helper()
	for _, r := range text {
		a = pressKey(t, a, string(r))
	}
	return a
}

// The acceptance criterion: no confirmation dialog, but the target count is
// visible in the prompt while typing.
func TestPromptShowsTheTargetCountWhileTyping(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01", "web-02", "web-03")

	a = typeCommand(t, a, "rm -rf /tmp/cache")
	view := plain(a.View().Content)

	if !strings.Contains(view, ":rm -rf /tmp/cache") {
		t.Fatalf("the prompt does not show what is being typed:\n%s", view)
	}
	if !strings.Contains(view, fmt.Sprintf("BROADCAST all (%d host", len(a.WindowHosts()))) {
		t.Fatalf("the prompt does not show the scope:\n%s", view)
	}
	// No confirmation step: enter sends.
	a, _ = enter(t, a)
	if a.CommandLineOpen() {
		t.Fatal("enter opened a confirmation instead of sending")
	}
}

// The acceptance criterion: a command sent while some hosts are down reports
// how many actually received it.
func TestDeliveryReportNamesWhatWasMissed(t *testing.T) {
	a, sender, _, _ := cmdApp(t, "web-01", "web-02", "web-03")
	sender.delivery = broadcast.Delivery{
		Mode: broadcast.ModeAll, Scope: 3, Targets: 2, Delivered: 2,
	}

	a = typeCommand(t, a, "uptime")
	a, _ = enter(t, a)

	if got := a.LastDelivery(); got != "sent to 2/3 hosts (1 did not receive it)" {
		t.Fatalf("LastDelivery() = %q", got)
	}
	if !strings.Contains(plain(a.View().Content), "1 did not receive it") {
		t.Fatalf("the status bar does not report the miss:\n%s", plain(a.View().Content))
	}
}

func TestSendErrorIsReported(t *testing.T) {
	a, sender, _, _ := cmdApp(t, "web-01")
	sender.err = errors.New("broken pipe")

	a = typeCommand(t, a, "uptime")
	a, _ = enter(t, a)

	if !strings.Contains(a.LastDelivery(), "broken pipe") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
}

// The acceptance criterion: editing keys work in the input line without leaking
// to the remote hosts.
func TestEditingKeysDoNotLeak(t *testing.T) {
	a, sender, router, _ := cmdApp(t, "web-01", "web-02")

	// "b" would switch the broadcast mode, ":" would open a second prompt, "s"
	// would switch to single. Inside the command line they are just letters.
	a = typeCommand(t, a, "bs: echo hi")
	if router.Mode() != broadcast.ModeAll {
		t.Fatalf("a typed letter switched the broadcast mode to %v", router.Mode())
	}
	if a.CommandLineValue() != "bs: echo hi" {
		t.Fatalf("CommandLineValue() = %q", a.CommandLineValue())
	}
	if len(sender.sent) != 0 {
		t.Fatalf("typing reached the hosts: %q", sender.sent)
	}

	// Backspace edits the line rather than reaching a shell.
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.CommandLineValue() != "bs: echo h" {
		t.Fatalf("backspace left %q", a.CommandLineValue())
	}
	if len(sender.sent) != 0 {
		t.Fatalf("an editing key reached the hosts: %q", sender.sent)
	}
}

func TestEscapeAbandonsTheCommand(t *testing.T) {
	a, sender, _, _ := cmdApp(t, "web-01")

	a = typeCommand(t, a, "reboot")
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.CommandLineOpen() {
		t.Fatal("escape left the command line open")
	}
	if len(sender.sent) != 0 {
		t.Fatalf("escape sent %q", sender.sent)
	}
	if len(a.History()) != 0 {
		t.Fatalf("escape recorded %v in the history", a.History())
	}
}

func TestEmptyCommandSendsNothing(t *testing.T) {
	a, sender, _, log := cmdApp(t, "web-01")

	a = typeCommand(t, a, "   ")
	a, cmd := enter(t, a)

	if len(sender.sent) != 0 || cmd != nil {
		t.Fatalf("an empty command was sent: %q", sender.sent)
	}
	if log.Len() != 0 {
		t.Fatalf("an empty command was logged")
	}
	if a.CommandLineOpen() {
		t.Fatal("the prompt stayed open")
	}
}

// History: up and down walk what was typed this run.
func TestHistoryRecallsCommands(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01")

	for _, command := range []string{"uptime", "df -h"} {
		a = typeCommand(t, a, command)
		a, _ = enter(t, a)
	}
	if got := strings.Join(a.History(), ","); got != "uptime,df -h" {
		t.Fatalf("History() = %q", got)
	}

	a = pressKey(t, a, ":")
	a = pressArrow(t, a, tea.KeyUp)
	if a.CommandLineValue() != "df -h" {
		t.Fatalf("first recall = %q", a.CommandLineValue())
	}
	a = pressArrow(t, a, tea.KeyUp)
	if a.CommandLineValue() != "uptime" {
		t.Fatalf("second recall = %q", a.CommandLineValue())
	}
	a = pressArrow(t, a, tea.KeyUp)
	if a.CommandLineValue() != "uptime" {
		t.Fatalf("recall past the oldest entry = %q", a.CommandLineValue())
	}

	a = pressArrow(t, a, tea.KeyDown)
	if a.CommandLineValue() != "df -h" {
		t.Fatalf("recall forward = %q", a.CommandLineValue())
	}
	a = pressArrow(t, a, tea.KeyDown)
	if a.CommandLineValue() != "" {
		t.Fatalf("past the newest entry = %q, want an empty line", a.CommandLineValue())
	}
}

// pressArrow drives an arrow key through the model.
func pressArrow(t *testing.T, a App, code rune) App {
	t.Helper()

	model, _ := a.Update(tea.KeyPressMsg{Code: code})
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	return next
}

func TestHistoryDoesNotRepeatTheSameCommand(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01")

	for range 3 {
		a = typeCommand(t, a, "uptime")
		a, _ = enter(t, a)
	}
	if got := a.History(); len(got) != 1 {
		t.Fatalf("History() = %v", got)
	}
}

// Passwords typed in single mode never reach the log; the command line hands
// the mode to the recorder and the log enforces it.
func TestSingleModeCommandIsNotLogged(t *testing.T) {
	a, sender, router, log := cmdApp(t, "web-01", "web-02")
	if err := router.SetMode(broadcast.ModeSingle); err != nil {
		t.Fatalf("SetMode: %v", err)
	}
	sender.delivery = broadcast.Delivery{Mode: broadcast.ModeSingle, Scope: 1, Targets: 1, Delivered: 1}

	a = typeCommand(t, a, "hunter2")
	a, _ = enter(t, a)

	if log.Len() != 0 {
		entry, _ := log.Last()
		t.Fatalf("single mode input was logged: %q", entry.Command)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("the command was not sent: %q", sender.sent)
	}
}

func TestCommandLineWithoutATransport(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)

	a = typeCommand(t, a, "uptime")
	a, cmd := enter(t, a)

	if cmd != nil {
		t.Fatal("a run with no transport reported a send")
	}
	if !strings.Contains(a.LastDelivery(), "nothing was sent") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
	if !strings.Contains(plain(a.View().Content), "nothing was sent") {
		t.Fatalf("the status bar does not say so:\n%s", plain(a.View().Content))
	}
}

func TestCommandLineReplacesTheStatusBarWhileOpen(t *testing.T) {
	a, _, _, _ := cmdApp(t, "web-01")

	closed := plain(a.View().Content)
	a = typeCommand(t, a, "uptime")
	open := plain(a.View().Content)

	if closed == open {
		t.Fatal("opening the command line changed nothing on screen")
	}
	if !strings.Contains(open, ":uptime") {
		t.Fatalf("the prompt is not drawn:\n%s", open)
	}
}

// Resending from the Command log goes through the same path as typing: the
// broadcast set that is active now, the same report, the same audit entry.
func TestResendFromTheCommandLog(t *testing.T) {
	a, sender, _, log := cmdApp(t, "web-01", "web-02")

	a = typeCommand(t, a, "uptime")
	a, _ = enter(t, a)
	if len(sender.sent) != 1 {
		t.Fatalf("setup: sent = %q", sender.sent)
	}

	model, cmd := a.Update(CommandResendMsg{Command: "uptime"})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if len(sender.sent) != 2 || sender.sent[1] != "uptime\n" {
		t.Fatalf("resend sent %q", sender.sent)
	}
	if log.Len() != 2 {
		t.Fatalf("the resend left %d log entries, want 2", log.Len())
	}
	if cmd == nil {
		t.Fatal("the resend produced no report")
	}
	if msg, ok := cmd().(CommandSentMsg); !ok || msg.Command != "uptime" {
		t.Fatalf("the resend produced %#v", cmd())
	}
}

func TestResendOfAnEmptyCommandDoesNothing(t *testing.T) {
	a, sender, _, _ := cmdApp(t, "web-01")

	model, cmd := a.Update(CommandResendMsg{Command: "  "})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if len(sender.sent) != 0 || cmd != nil {
		t.Fatalf("an empty resend sent %q", sender.sent)
	}
}
