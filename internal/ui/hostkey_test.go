package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// keyQuestionApp opens a run with the host key prompt in a pane's scrollback,
// the way the program layer writes it, and delivers the question.
func keyQuestionApp(t *testing.T) (App, *fakeFleet) {
	t.Helper()
	fleet := newFakeFleet("web-01", "web-02")
	a := resize(t, NewApp(Config{Fleet: fleet, Theme: Options{Dark: true}}), 200, 60)
	fleet.sessions["web-01"].Emit("Are you sure you want to continue connecting (yes/no)? ")
	model, _ := a.Update(HostKeyQuestionMsg{
		SessionID:   "web-01",
		Host:        "web-01",
		KeyType:     "ecdsa-sha2-nistp256",
		Fingerprint: "SHA256:H1rWNMxFHHGXdxzBXKZIRZnMSoJ4ZyVy8N187uFr1yg",
	})
	a = model.(App)
	a.focus = AreaGrid
	a.paneIndex = 0
	return a, fleet
}

// typeAnswer feeds one character at a time through the key handler.
func typeAnswer(t *testing.T, a App, text string) App {
	t.Helper()
	for _, r := range text {
		a = pressKey(t, a, string(r))
	}
	return a
}

// The typed answer echoes inline at the end of the pane's scrollback, right
// after the prompt - the way a terminal takes a yes/no (issue #180).
func TestHostKeyAnswerEchoesInline(t *testing.T) {
	a, _ := keyQuestionApp(t)
	a = typeAnswer(t, a, "yes")

	body := plain(a.paneBody("web-01", 80, 10))
	if !strings.Contains(body, "(yes/no)? yes") {
		t.Fatalf("the typed answer does not echo after the prompt:\n%s", body)
	}
	if other := plain(a.paneBody("web-02", 80, 10)); strings.Contains(other, "yes") {
		t.Fatalf("another pane echoes the answer:\n%s", other)
	}

	// backspace edits the answer like a terminal does.
	a = pressKey(t, a, "backspace")
	if body := plain(a.paneBody("web-01", 80, 10)); !strings.Contains(body, "(yes/no)? ye") || strings.Contains(body, "? yes") {
		t.Fatalf("backspace did not edit the echoed answer:\n%s", body)
	}
	// The status bar names the one prompting host.
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH web-01") {
		t.Fatalf("the status bar is missing AUTH web-01:\n%s", bar)
	}
}

// Typed yes (or y) plus enter accepts; the answer names the session.
func TestHostKeyQuestionAccept(t *testing.T) {
	for _, answer := range []string{"yes", "y", "YES"} {
		a, _ := keyQuestionApp(t)
		a = typeAnswer(t, a, answer)
		model, cmd := a.Update(keyMsgFor(t, "enter"))
		a = model.(App)
		if a.AuthPending() != 0 {
			t.Fatalf("the question is still open after %q + enter", answer)
		}
		if cmd == nil {
			t.Fatalf("%q + enter produced no answer", answer)
		}
		got, ok := cmd().(HostKeyAnswerMsg)
		if !ok || !got.Accept || got.SessionID != "web-01" {
			t.Fatalf("%q + enter produced %#v", answer, cmd())
		}
	}
}

// Typed no (or n) plus enter rejects, and so does esc.
func TestHostKeyQuestionReject(t *testing.T) {
	for _, answer := range []string{"no", "n"} {
		a, _ := keyQuestionApp(t)
		a = typeAnswer(t, a, answer)
		model, cmd := a.Update(keyMsgFor(t, "enter"))
		a = model.(App)
		if a.AuthPending() != 0 {
			t.Fatalf("the question is still open after %q + enter", answer)
		}
		got, ok := cmd().(HostKeyAnswerMsg)
		if !ok || got.Accept {
			t.Fatalf("%q + enter produced %#v", answer, cmd())
		}
	}

	a, _ := keyQuestionApp(t)
	model, cmd := a.Update(keyMsgFor(t, "esc"))
	a = model.(App)
	if a.AuthPending() != 0 {
		t.Fatal("the question is still open after esc")
	}
	if answer, ok := cmd().(HostKeyAnswerMsg); !ok || answer.Accept {
		t.Fatalf("esc produced %#v", cmd())
	}
}

// Anything else typed and entered clears and asks again: a keystroke meant
// for a shell must not answer a security question.
func TestHostKeyQuestionSurvivesOtherAnswers(t *testing.T) {
	a, _ := keyQuestionApp(t)

	for _, answer := range []string{"x", "quit", ""} {
		a = typeAnswer(t, a, answer)
		model, cmd := a.Update(keyMsgFor(t, "enter"))
		a = model.(App)
		if a.AuthPending() != 1 {
			t.Fatalf("%q + enter closed the question", answer)
		}
		if cmd != nil {
			t.Fatalf("%q + enter produced a command: %#v", answer, cmd())
		}
		if len(a.auth["web-01"].answer) != 0 {
			t.Fatalf("the rejected answer %q was not cleared", answer)
		}
	}
}

// ctrl+q still quits: the question must not be able to trap the user.
func TestHostKeyQuestionCannotTrapTheUser(t *testing.T) {
	a, _ := keyQuestionApp(t)
	_, cmd := a.Update(keyMsgFor(t, "ctrl+q"))
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+q did not quit while the question was open")
	}
}
