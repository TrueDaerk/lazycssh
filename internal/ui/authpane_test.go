package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// authTestApp opens a run over a fake fleet with prompts already written into
// two panes' scrollbacks, the way the program layer writes them, and delivers
// both questions - concurrently open, one per pane (issue #182).
func authTestApp(t *testing.T) (App, *fakeFleet) {
	t.Helper()
	fleet := newFakeFleet("localhost", "localhost#2", "localhost#3")
	ws := workingset.New(fleet.IDs())
	router, err := broadcast.NewRouter(ws)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	sender := &fakeSender{delivery: broadcast.Delivery{Mode: broadcast.ModeAll}}
	a := resize(t, NewApp(Config{
		Fleet: fleet, Targets: router, WorkingSet: ws, Sender: sender,
		Panes: fleet, Theme: Options{Dark: true},
	}), 200, 60)
	for _, id := range []string{"localhost", "localhost#2"} {
		fleet.sessions[id].Emit("test@localhost's password: ")
		model, _ := a.Update(SecretQuestionMsg{SessionID: id, Host: "localhost", Prompt: "test@localhost's password: "})
		a = model.(App)
	}
	return a, fleet
}

// Every prompting pane holds its own question at once; the status bar counts
// them and the Status panel names them.
func TestAuthQuestionsAreConcurrentAndPerPane(t *testing.T) {
	a, _ := authTestApp(t)

	if got := a.AuthPending(); got != 2 {
		t.Fatalf("AuthPending() = %d, want 2", got)
	}
	for _, id := range []string{"localhost", "localhost#2"} {
		if _, ok := a.inlineAnswerEcho(id); !ok {
			t.Fatalf("%s does not await input", id)
		}
	}
	if _, ok := a.inlineAnswerEcho("localhost#3"); ok {
		t.Fatal("a pane without a question awaits input")
	}
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH 2 hosts") {
		t.Fatalf("the status bar does not count the prompts:\n%s", bar)
	}
	if panel := plain(a.statusPanel(80)); !strings.Contains(panel, "auth: 2 prompts open — localhost localhost#2") {
		t.Fatalf("the Status panel does not name the prompts:\n%s", panel)
	}
}

// Typing into the focused pane answers only that pane's prompt: per-host
// passwords stay possible in a fleet that prompts everywhere.
func TestFocusedPaneAnswersItsOwnPrompt(t *testing.T) {
	a, _ := authTestApp(t)
	a.focus = AreaGrid
	a.paneIndex = 1 // localhost#2

	for _, k := range []string{"h", "u", "n", "t"} {
		a = pressKey(t, a, k)
	}
	model, cmd := a.Update(keyMsgFor(t, "enter"))
	a = model.(App)

	if cmd == nil {
		t.Fatal("enter produced no answer")
	}
	answer, ok := cmd().(SecretAnswerMsg)
	if !ok || answer.SessionID != "localhost#2" || answer.Value != "hunt" || !answer.Ok {
		t.Fatalf("enter produced %#v", cmd())
	}
	if got := a.AuthPending(); got != 1 {
		t.Fatalf("AuthPending() = %d, the other pane's prompt must survive", got)
	}
	if _, ok := a.inlineAnswerEcho("localhost"); !ok {
		t.Fatal("the unanswered pane stopped awaiting input")
	}
}

// The broadcast line answers every prompting target at once: one typing
// action for a uniform cluster.
func TestBroadcastAnswersEveryPrompt(t *testing.T) {
	a, _ := authTestApp(t)
	a.focus = AreaBroadcast

	for _, k := range []string{"h", "u", "n", "t"} {
		a = pressKey(t, a, k)
	}
	model, cmd := a.Update(keyMsgFor(t, "enter"))
	a = model.(App)

	if got := a.AuthPending(); got != 0 {
		t.Fatalf("AuthPending() = %d, want every prompt answered", got)
	}
	if cmd == nil {
		t.Fatal("enter produced no answers")
	}
	got := map[string]string{}
	collectAnswers(t, cmd(), got)
	if len(got) != 2 || got["localhost"] != "hunt" || got["localhost#2"] != "hunt" {
		t.Fatalf("answers = %v", got)
	}
}

// collectAnswers walks a possibly-batched command result for SecretAnswerMsg.
func collectAnswers(t *testing.T, msg tea.Msg, into map[string]string) {
	t.Helper()
	switch v := msg.(type) {
	case SecretAnswerMsg:
		if !v.Ok {
			t.Fatalf("answer for %s arrived cancelled", v.SessionID)
		}
		into[v.SessionID] = v.Value
	case tea.BatchMsg:
		for _, cmd := range v {
			collectAnswers(t, cmd(), into)
		}
	default:
		t.Fatalf("unexpected message %#v", msg)
	}
}

// Backspace edits and esc cancels only the focused pane's answer.
func TestAuthEditingAndCancel(t *testing.T) {
	a, _ := authTestApp(t)
	a.focus = AreaGrid
	a.paneIndex = 0

	a = pressKey(t, a, "a")
	a = pressKey(t, a, "b")
	a = pressKey(t, a, "backspace")
	if got := string(a.auth["localhost"].answer); got != "a" {
		t.Fatalf("answer = %q after backspace", got)
	}

	model, cmd := a.Update(keyMsgFor(t, "esc"))
	a = model.(App)
	if cmd == nil {
		t.Fatal("esc produced no answer")
	}
	if answer, ok := cmd().(SecretAnswerMsg); !ok || answer.Ok || answer.SessionID != "localhost" {
		t.Fatalf("esc produced %#v", cmd())
	}
	if got := a.AuthPending(); got != 1 {
		t.Fatalf("AuthPending() = %d, the other prompt must survive the cancel", got)
	}
}

// A question whose session failed is pruned with the fleet snapshot: its
// buffer must not keep swallowing keystrokes.
func TestAuthQuestionIsPrunedWithItsSession(t *testing.T) {
	a, fleet := authTestApp(t)
	fleet.fail(t, "localhost")
	a = syncFleet(t, a)

	if _, ok := a.inlineAnswerEcho("localhost"); ok {
		t.Fatal("a failed session still awaits input")
	}
	if got := a.AuthPending(); got != 1 {
		t.Fatalf("AuthPending() = %d, want only the live prompt", got)
	}
}
