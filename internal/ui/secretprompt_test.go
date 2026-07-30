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

// A prompt naming a host renders in that host's pane, which gets the focus;
// the Status panel does not repeat it (issue #177).
func TestSecretPromptRendersInThePane(t *testing.T) {
	a := resize(t, testApp(), 120, 40)
	model, _ := a.Update(SecretQuestionMsg{Host: "web-02", Prompt: "password for test@web-02", Echo: false})
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
	lines := strings.Join(a.paneQuestionLines("web-02", 60), "\n")
	if !strings.Contains(plain(lines), "password for test@web-02") {
		t.Fatalf("the pane is missing the prompt label:\n%s", lines)
	}
	if strings.Contains(plain(lines), "hunt") {
		t.Fatalf("the typed secret is rendered in the pane:\n%s", lines)
	}
	squeezed := strings.NewReplacer(" ", "", "\n", "").Replace(plain(a.statusPanel(40)))
	if strings.Contains(squeezed, "passwordfortest@web-02") {
		t.Fatalf("the Status panel repeats the in-pane prompt:\n%s", plain(a.statusPanel(40)))
	}
	if bar := plain(a.renderStatusBar()); !strings.Contains(bar, "AUTH web-02") {
		t.Fatalf("the status bar is missing AUTH web-02:\n%s", bar)
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
