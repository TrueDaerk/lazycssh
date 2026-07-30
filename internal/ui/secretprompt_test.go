package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func secretApp(t *testing.T, echo bool) App {
	t.Helper()
	a := resize(t, testApp(), 120, 40)
	model, _ := a.Update(SecretQuestionMsg{Prompt: "password for test@db1", Echo: echo})
	return model.(App)
}

// The prompt renders in the Status panel and the typed value never does.
func TestSecretPromptMasksTheValue(t *testing.T) {
	a := secretApp(t, false)

	if a.Panel() != PanelStatus {
		t.Fatalf("Panel() = %v, want the Status panel", a.Panel())
	}
	a = pressKey(t, a, "h")
	a = pressKey(t, a, "u")
	a = pressKey(t, a, "n")
	a = pressKey(t, a, "t")

	squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(40)))
	if !strings.Contains(squeezed, "passwordfortest@db1") {
		t.Fatalf("the prompt label is missing:\n%s", plain(a.statusPanel(40)))
	}
	if strings.Contains(squeezed, "hunt") {
		t.Fatalf("the typed secret is rendered:\n%s", plain(a.statusPanel(40)))
	}
}

// enter submits what was typed; the input is wiped.
func TestSecretPromptSubmits(t *testing.T) {
	a := secretApp(t, false)
	for _, k := range []string{"h", "u", "n", "t"} {
		a = pressKey(t, a, k)
	}

	model, cmd := a.Update(keyMsgFor(t, "enter"))
	a = model.(App)
	if a.SecretPromptOpen() {
		t.Fatal("the prompt is still open after enter")
	}
	if cmd == nil {
		t.Fatal("enter produced no answer")
	}
	answer, ok := cmd().(SecretAnswerMsg)
	if !ok || !answer.Ok || answer.Value != "hunt" {
		t.Fatalf("enter produced %#v", cmd())
	}
	if a.secretInput.Value() != "" {
		t.Fatal("the typed secret survived in the input buffer")
	}
}

// esc cancels: the attempt fails rather than hanging, and nothing is sent.
func TestSecretPromptCancels(t *testing.T) {
	a := secretApp(t, false)
	a = pressKey(t, a, "x")

	model, cmd := a.Update(keyMsgFor(t, "esc"))
	a = model.(App)
	if a.SecretPromptOpen() {
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

// A keyboard-interactive question with echo shows what is typed.
func TestSecretPromptHonoursEcho(t *testing.T) {
	a := secretApp(t, true)
	for _, k := range []string{"o", "t", "p"} {
		a = pressKey(t, a, k)
	}
	if !strings.Contains(plain(a.statusPanel(60)), "otp") {
		t.Fatalf("an echoing question hides its answer:\n%s", plain(a.statusPanel(60)))
	}
}

// A prompt naming a host focuses that host's pane, where the prompt sits in
// the scrollback (written by the program layer, issue #180): a masked answer
// echoes nothing there - all a terminal shows of a typed password - and the
// Status panel does not repeat the question.
func TestSecretPromptMasksInThePane(t *testing.T) {
	fleet := newFakeFleet("web-01", "web-02")
	a := resize(t, NewApp(Config{Hosts: fleet.IDs(), Fleet: fleet, Theme: Options{Dark: true}}), 200, 60)
	fleet.sessions["web-02"].Emit("test@web-02's password: ")

	model, _ := a.Update(SecretQuestionMsg{Host: "web-02", Prompt: "test@web-02's password: ", Echo: false})
	a = model.(App)

	if a.Focus() != AreaGrid {
		t.Fatalf("Focus() = %v, want the grid", a.Focus())
	}
	if got := a.FocusedHost(); got != "web-02" {
		t.Fatalf("FocusedHost() = %q, want web-02", got)
	}
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
	squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(40)))
	if strings.Contains(squeezed, "password") {
		t.Fatalf("the Status panel repeats the in-pane prompt:\n%s", plain(a.statusPanel(40)))
	}
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH web-02") {
		t.Fatalf("the status bar is missing AUTH web-02:\n%s", bar)
	}
}

// An echoing keyboard-interactive answer shows inline in the pane as it is
// typed, the way a terminal echoes it.
func TestSecretPromptEchoesInThePane(t *testing.T) {
	fleet := newFakeFleet("web-01")
	a := resize(t, NewApp(Config{Hosts: fleet.IDs(), Fleet: fleet, Theme: Options{Dark: true}}), 200, 60)
	fleet.sessions["web-01"].Emit("Verification code: ")

	model, _ := a.Update(SecretQuestionMsg{Host: "web-01", Prompt: "Verification code: ", Echo: true})
	a = model.(App)
	for _, k := range []string{"o", "t", "p"} {
		a = pressKey(t, a, k)
	}
	if body := plain(a.paneBody("web-01", 80, 10)); !strings.Contains(body, "Verification code: otp") {
		t.Fatalf("the echoing answer does not echo inline:\n%s", body)
	}
}

// ctrl+q still quits: the prompt must not be able to trap the user.
func TestSecretPromptCannotTrapTheUser(t *testing.T) {
	a := secretApp(t, false)
	_, cmd := a.Update(keyMsgFor(t, "ctrl+q"))
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+q did not quit while the prompt was open")
	}
}
