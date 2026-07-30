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

// The question renders where the user is looking: the host's pane is focused
// and carries the host, the key type and the full fingerprint (issue #177).
func TestHostKeyQuestionRendersInThePane(t *testing.T) {
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
	// The pane wraps at its width; compare with the whitespace squeezed out
	// so the assertion does not depend on where the breaks fall.
	lines := a.paneQuestionLines("web-01", 60)
	squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(strings.Join(lines, "\n")))
	for _, want := range []string{"unknownecdsa-sha2-nistp256keyforweb-01",
		"SHA256:H1rWNMxFHHGXdxzBXKZIRZnMSoJ4ZyVy8N187uFr1yg"} {
		if !strings.Contains(squeezed, want) {
			t.Fatalf("the pane is missing %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
	// Every other pane stays clean, and the Status panel does not repeat a
	// question the pane already shows.
	if got := a.paneQuestionLines("web-02", 60); got != nil {
		t.Fatalf("web-02 renders someone else's question:\n%s", strings.Join(got, "\n"))
	}
	if squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(28))); strings.Contains(squeezed, "unknown") {
		t.Fatalf("the Status panel repeats the in-pane question:\n%s", plain(a.statusPanel(28)))
	}
	// The status bar says a question owns the keyboard.
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH web-01") {
		t.Fatalf("the status bar is missing AUTH web-01:\n%s", bar)
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
	squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(28)))
	if !strings.Contains(squeezed, "unknownecdsa-sha2-nistp256keyfornot-in-the-run") {
		t.Fatalf("the Status panel is missing the question:\n%s", plain(a.statusPanel(28)))
	}
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH not-in-the-run — see [1] Status") {
		t.Fatalf("the status bar does not point at the Status panel:\n%s", bar)
	}
}

// Answering clears the in-pane rendering with the question.
func TestHostKeyQuestionLeavesThePaneOnAnswer(t *testing.T) {
	a := questionApp(t)
	model, _ := a.Update(keyMsgFor(t, "y"))
	a = model.(App)
	if got := a.paneQuestionLines("web-01", 60); got != nil {
		t.Fatalf("the pane still renders the answered question:\n%s", strings.Join(got, "\n"))
	}
	if bar := plain(a.renderStatusBar()); strings.Contains(bar, "AUTH") {
		t.Fatalf("the status bar still says AUTH after the answer:\n%s", bar)
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
