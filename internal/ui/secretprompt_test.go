package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// secretApp opens a run with a password prompt in one pane's scrollback, the
// way the program layer writes it, and delivers the question. The pane is
// focused, so typing answers it.
func secretApp(t *testing.T, echo bool) (App, *fakeFleet) {
	t.Helper()
	fleet := newFakeFleet("web-01", "web-02")
	a := resize(t, NewApp(Config{Fleet: fleet, Theme: Options{Dark: true}}), 200, 60)
	fleet.sessions["web-02"].Emit("test@web-02's password: ")
	model, _ := a.Update(SecretQuestionMsg{SessionID: "web-02", Host: "web-02", Prompt: "test@web-02's password: ", Echo: echo})
	a = model.(App)
	a.focus = AreaGrid
	a.paneIndex = 1
	return a, fleet
}

// A masked answer echoes nothing in the pane - all a terminal shows of a
// typed password - and the typed value appears nowhere in the frame.
func TestSecretPromptMasksTheValue(t *testing.T) {
	a, _ := secretApp(t, false)
	for _, k := range []string{"h", "u", "n", "t"} {
		a = pressKey(t, a, k)
	}

	body := plain(a.paneBody("web-02", 80, 10))
	if !strings.Contains(body, "password: ") {
		t.Fatalf("the pane is missing the prompt:\n%s", body)
	}
	if strings.Contains(body, "hunt") {
		t.Fatalf("the typed secret is rendered in the pane:\n%s", body)
	}
	if view := plain(a.View().Content); strings.Contains(view, "hunt") {
		t.Fatalf("the typed secret is rendered somewhere in the frame:\n%s", view)
	}
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH web-02") {
		t.Fatalf("the status bar is missing AUTH web-02:\n%s", bar)
	}
}

// An echoing keyboard-interactive answer shows inline as it is typed.
func TestSecretPromptEchoesInThePane(t *testing.T) {
	a, fleet := secretApp(t, true)
	fleet.sessions["web-02"].Emit("\r\nVerification code: ")
	for _, k := range []string{"o", "t", "p"} {
		a = pressKey(t, a, k)
	}
	if body := plain(a.paneBody("web-02", 80, 10)); !strings.Contains(body, "Verification code: otp") {
		t.Fatalf("the echoing answer does not echo inline:\n%s", body)
	}
}

// enter submits what was typed; the answer names the session and the buffer
// is gone with the question.
func TestSecretPromptSubmits(t *testing.T) {
	a, _ := secretApp(t, false)
	for _, k := range []string{"h", "u", "n", "t"} {
		a = pressKey(t, a, k)
	}

	model, cmd := a.Update(keyMsgFor(t, "enter"))
	a = model.(App)
	if a.AuthPending() != 0 {
		t.Fatal("the prompt is still open after enter")
	}
	if cmd == nil {
		t.Fatal("enter produced no answer")
	}
	answer, ok := cmd().(SecretAnswerMsg)
	if !ok || !answer.Ok || answer.Value != "hunt" || answer.SessionID != "web-02" {
		t.Fatalf("enter produced %#v", cmd())
	}
}

// esc cancels: the attempt fails rather than hanging, and nothing is sent.
func TestSecretPromptCancels(t *testing.T) {
	a, _ := secretApp(t, false)
	a = pressKey(t, a, "x")

	model, cmd := a.Update(keyMsgFor(t, "esc"))
	a = model.(App)
	if a.AuthPending() != 0 {
		t.Fatal("the prompt is still open after esc")
	}
	if cmd == nil {
		t.Fatal("esc produced no answer")
	}
	answer, ok := cmd().(SecretAnswerMsg)
	if !ok || answer.Ok || answer.Value != "" {
		t.Fatalf("esc produced %#v", cmd())
	}
}

// ctrl+q still quits: the prompt must not be able to trap the user.
func TestSecretPromptCannotTrapTheUser(t *testing.T) {
	a, _ := secretApp(t, false)
	_, cmd := a.Update(keyMsgFor(t, "ctrl+q"))
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+q did not quit while the prompt was open")
	}
}
