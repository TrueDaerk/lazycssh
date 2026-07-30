package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func questionApp(t *testing.T) App {
	t.Helper()
	a := resize(t, testApp(), 120, 40)
	model, _ := a.Update(HostKeyQuestionMsg{
		Host:        "web-01",
		KeyType:     "ecdsa-sha2-nistp256",
		Fingerprint: "SHA256:H1rWNMxFHHGXdxzBXKZIRZnMSoJ4ZyVy8N187uFr1yg",
	})
	return model.(App)
}

// typeAnswer feeds one character at a time through the modal key handler.
func typeAnswer(t *testing.T, a App, text string) App {
	t.Helper()
	for _, r := range text {
		a = pressKey(t, a, string(r))
	}
	return a
}

// The question focuses the host's pane: the prompt itself is in the pane's
// scrollback (written by the program layer, issue #180), so the UI's job is to
// put the user in front of it and to say so in the status bar.
func TestHostKeyQuestionFocusesThePane(t *testing.T) {
	a := questionApp(t)

	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v, want the grid", a.Focus())
	}
	if got := a.FocusedHost(); got != "web-01" {
		t.Fatalf("FocusedHost() = %q, want web-01", got)
	}
	if got := a.HostKeyQuestionPending(); got != "web-01" {
		t.Fatalf("HostKeyQuestionPending() = %q", got)
	}
	// The Status panel does not repeat a question the pane already shows.
	if squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(28))); strings.Contains(squeezed, "unknown") {
		t.Fatalf("the Status panel repeats the in-pane question:\n%s", plain(a.statusPanel(28)))
	}
	// The status bar says a question owns the keyboard.
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH web-01") {
		t.Fatalf("the status bar is missing AUTH web-01:\n%s", bar)
	}
}

// The typed answer echoes inline at the end of the pane's scrollback, right
// after the prompt the program wrote there - the way a terminal takes a
// yes/no (issue #180).
func TestHostKeyAnswerEchoesInline(t *testing.T) {
	fleet := newFakeFleet("web-01", "web-02")
	a := resize(t, NewApp(Config{Hosts: fleet.IDs(), Fleet: fleet, Theme: Options{Dark: true}}), 200, 60)
	fleet.sessions["web-01"].Emit("Are you sure you want to continue connecting (yes/no)? ")

	model, _ := a.Update(HostKeyQuestionMsg{Host: "web-01", KeyType: "ssh-ed25519", Fingerprint: "SHA256:x"})
	a = model.(App)
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
}

// Typed yes (or y) plus enter accepts; the answer goes back to the program and
// the question closes.
func TestHostKeyQuestionAccept(t *testing.T) {
	for _, answer := range []string{"yes", "y", "YES"} {
		a := typeAnswer(t, questionApp(t), answer)
		model, cmd := a.Update(keyMsgFor(t, "enter"))
		a = model.(App)
		if a.HostKeyQuestionPending() != "" {
			t.Fatalf("the question is still open after %q + enter", answer)
		}
		if cmd == nil {
			t.Fatalf("%q + enter produced no answer", answer)
		}
		got, ok := cmd().(HostKeyAnswerMsg)
		if !ok || !got.Accept || got.Host != "web-01" {
			t.Fatalf("%q + enter produced %#v", answer, cmd())
		}
	}
}

// Typed no (or n) plus enter rejects, and so does esc.
func TestHostKeyQuestionReject(t *testing.T) {
	for _, answer := range []string{"no", "n"} {
		a := typeAnswer(t, questionApp(t), answer)
		model, cmd := a.Update(keyMsgFor(t, "enter"))
		a = model.(App)
		if a.HostKeyQuestionPending() != "" {
			t.Fatalf("the question is still open after %q + enter", answer)
		}
		if cmd == nil {
			t.Fatalf("%q + enter produced no answer", answer)
		}
		got, ok := cmd().(HostKeyAnswerMsg)
		if !ok || got.Accept {
			t.Fatalf("%q + enter produced %#v", answer, cmd())
		}
	}

	a := questionApp(t)
	model, cmd := a.Update(keyMsgFor(t, "esc"))
	a = model.(App)
	if a.HostKeyQuestionPending() != "" {
		t.Fatal("the question is still open after esc")
	}
	if answer, ok := cmd().(HostKeyAnswerMsg); !ok || answer.Accept {
		t.Fatalf("esc produced %#v", cmd())
	}
}

// Anything else typed and entered clears and asks again: a keystroke meant
// for a host must not answer a security question, and the question must
// survive it.
func TestHostKeyQuestionSurvivesOtherAnswers(t *testing.T) {
	a := questionApp(t)

	for _, answer := range []string{"x", "quit", ""} {
		a = typeAnswer(t, a, answer)
		model, cmd := a.Update(keyMsgFor(t, "enter"))
		a = model.(App)
		if a.HostKeyQuestionPending() != "web-01" {
			t.Fatalf("%q + enter closed the question", answer)
		}
		if cmd != nil {
			t.Fatalf("%q + enter produced a command: %#v", answer, cmd())
		}
		if len(a.keyAnswer) != 0 {
			t.Fatalf("the rejected answer %q was not cleared", answer)
		}
	}
}

// A question about a host without a pane - hidden by a filter, or a passphrase
// question without a host - falls back to the Status panel, as before.
func TestHostKeyQuestionFallsBackToTheStatusPanel(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	model, _ := a.Update(HostKeyQuestionMsg{
		Host:        "not-in-the-run",
		KeyType:     "ecdsa-sha2-nistp256",
		Fingerprint: "SHA256:H1rWNMxFHHGXdxzBXKZIRZnMSoJ4ZyVy8N187uFr1yg",
	})
	a = model.(App)

	if a.Panel() != PanelStatus {
		t.Fatalf("Panel() = %v, want the Status panel", a.Panel())
	}
	a = typeAnswer(t, a, "ye")
	squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(28)))
	for _, want := range []string{"unknownecdsa-sha2-nistp256keyfornot-in-the-run",
		"SHA256:H1rWNMxFHHGXdxzBXKZIRZnMSoJ4ZyVy8N187uFr1yg", "(yes/no)?ye"} {
		if !strings.Contains(squeezed, want) {
			t.Fatalf("the Status panel is missing %q:\n%s", want, plain(a.statusPanel(28)))
		}
	}
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH not-in-the-run — see [1] Status") {
		t.Fatalf("the status bar does not point at the Status panel:\n%s", bar)
	}
}

// Answering clears the inline echo with the question.
func TestHostKeyQuestionLeavesThePaneOnAnswer(t *testing.T) {
	a := typeAnswer(t, questionApp(t), "yes")
	model, _ := a.Update(keyMsgFor(t, "enter"))
	a = model.(App)
	if _, ok := a.inlineAnswerEcho("web-01"); ok {
		t.Fatal("the pane still echoes the answered question")
	}
	if bar := plain(a.renderStatusBar()); strings.Contains(bar, "AUTH") {
		t.Fatalf("the status bar still says AUTH after the answer:\n%s", bar)
	}
}

// ctrl+q still quits: the question must not be able to trap the user.
func TestHostKeyQuestionCannotTrapTheUser(t *testing.T) {
	a := questionApp(t)
	_, cmd := a.Update(keyMsgFor(t, "ctrl+q"))
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+q did not quit while the question was open")
	}
}
