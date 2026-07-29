package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func promptApp(t *testing.T, aliases []string, hosts ...string) App {
	t.Helper()

	fleet := newFakeFleet(hosts...)
	return resize(t, NewApp(Config{
		Fleet:         fleet,
		Panes:         fleet,
		ConfigAliases: aliases,
		Theme:         Options{Dark: true},
	}), 120, 40)
}

// n works from the app level: it selects the Status panel and opens the
// prompt there.
func TestNewHostKeyOpensThePromptInTheStatusPanel(t *testing.T) {
	a := promptApp(t, nil, "web-01")
	a = pressKey(t, a, "2") // somewhere else first

	a = pressKey(t, a, "n")
	if a.Panel() != PanelStatus || !a.ConnectPromptOpen() {
		t.Fatalf("Panel() = %v, ConnectPromptOpen() = %v after n", a.Panel(), a.ConnectPromptOpen())
	}

	view := plain(a.View().Content)
	if !strings.Contains(view, newHostPrompt) {
		t.Fatalf("the open prompt is not rendered:\n%s", view)
	}
}

func TestHostPromptConnectsTheTypedPattern(t *testing.T) {
	a := promptApp(t, nil, "web-01")
	a = pressKey(t, a, "n")
	for _, r := range "db-{01..02}" {
		a = pressKey(t, a, string(r))
	}

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a = model.(App)
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(HostConnectMsg)
	if !ok || len(msg.Patterns) != 1 || msg.Patterns[0] != "db-{01..02}" {
		t.Fatalf("enter produced %#v, want the typed pattern", msg)
	}
	if a.ConnectPromptOpen() {
		t.Fatal("the prompt did not close on enter")
	}
}

// The ssh-config offer survives as completion: matching aliases are listed
// under the open prompt and tab completes the first one. Hosts already in the
// run are not offered again.
func TestHostPromptOffersAndCompletesAliases(t *testing.T) {
	a := promptApp(t, []string{"web-01", "web-02", "db-01"}, "web-01")
	a = pressKey(t, a, "n")

	view := plain(a.View().Content)
	for _, want := range []string{"web-02", "db-01"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the empty prompt does not offer %q:\n%s", want, view)
		}
	}

	for _, r := range "db" {
		a = pressKey(t, a, string(r))
	}
	if hints := a.aliasHints(); len(hints) != 1 || hints[0] != "db-01" {
		t.Fatalf("aliasHints() = %v, want the one matching alias", hints)
	}

	a = pressKey(t, a, "tab")
	if got := a.ConnectPromptValue(); got != "db-01" {
		t.Fatalf("ConnectPromptValue() = %q after tab", got)
	}
}

func TestHostPromptEscCloses(t *testing.T) {
	a := promptApp(t, nil, "web-01")
	a = pressKey(t, a, "n")
	a = pressKey(t, a, "x")
	a = pressKey(t, a, "esc")
	if a.ConnectPromptOpen() || a.ConnectPromptValue() != "" {
		t.Fatal("esc did not abandon the prompt")
	}
}

// ctrl+q must not be trappable by the prompt an empty start opens into.
func TestCtrlQQuitsFromTheHostPrompt(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	if !a.ConnectPromptOpen() {
		t.Fatal("setup: the prompt is not open on an empty start")
	}
	_, cmd := a.Update(tea.KeyPressMsg{Code: 'q', Mod: tea.ModCtrl})
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+q inside the host prompt did not quit")
	}
}
