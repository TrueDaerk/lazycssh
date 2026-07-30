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

// The question renders where the user is looking: the Status panel is
// selected and carries the host, the key type and the full fingerprint.
func TestHostKeyQuestionRenders(t *testing.T) {
	a := questionApp(t)

	if a.Panel() != PanelStatus {
		t.Fatalf("Panel() = %v, want the Status panel", a.Panel())
	}
	if got := a.HostKeyQuestionPending(); got != "web-01" {
		t.Fatalf("HostKeyQuestionPending() = %q", got)
	}
	// The panel wraps at its width; compare with the whitespace squeezed out
	// so the assertion does not depend on where the breaks fall.
	squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(28)))
	for _, want := range []string{"unknownecdsa-sha2-nistp256keyforweb-01",
		"SHA256:H1rWNMxFHHGXdxzBXKZIRZnMSoJ4ZyVy8N187uFr1yg"} {
		if !strings.Contains(squeezed, want) {
			t.Fatalf("the Status panel is missing %q:\n%s", want, plain(a.statusPanel(28)))
		}
	}
}

// y accepts: the answer goes back to the program and the question closes.
func TestHostKeyQuestionAccept(t *testing.T) {
	a := questionApp(t)

	model, cmd := a.Update(keyMsgFor(t, "y"))
	a = model.(App)
	if a.HostKeyQuestionPending() != "" {
		t.Fatal("the question is still open after y")
	}
	if cmd == nil {
		t.Fatal("y produced no answer")
	}
	answer, ok := cmd().(HostKeyAnswerMsg)
	if !ok || !answer.Accept || answer.Host != "web-01" {
		t.Fatalf("y produced %#v", cmd())
	}
}

// n and esc reject.
func TestHostKeyQuestionReject(t *testing.T) {
	for _, key := range []string{"n", "esc"} {
		a := questionApp(t)
		model, cmd := a.Update(keyMsgFor(t, key))
		a = model.(App)
		if a.HostKeyQuestionPending() != "" {
			t.Fatalf("the question is still open after %s", key)
		}
		if cmd == nil {
			t.Fatalf("%s produced no answer", key)
		}
		answer, ok := cmd().(HostKeyAnswerMsg)
		if !ok || answer.Accept {
			t.Fatalf("%s produced %#v", key, cmd())
		}
	}
}

// Every other key is swallowed: a keystroke meant for a host must not answer
// a security question, and the question must survive it.
func TestHostKeyQuestionSwallowsOtherKeys(t *testing.T) {
	a := questionApp(t)

	for _, key := range []string{"x", "enter", "2", "q"} {
		model, cmd := a.Update(keyMsgFor(t, key))
		a = model.(App)
		if a.HostKeyQuestionPending() != "web-01" {
			t.Fatalf("%q closed the question", key)
		}
		if cmd != nil {
			t.Fatalf("%q produced a command: %#v", key, cmd())
		}
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
