package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The acceptance criterion of issue #307: with a host focused, cmd+v puts the
// pasted text into that host's input. Before the fix handlePaste dropped every
// paste that did not arrive in the broadcast bar, so this reached nobody.
func TestPasteReachesTheFocusedHostOnly(t *testing.T) {
	a, fleet := typingApp(t, "web-01", "web-02")

	a = paste(t, a, "echo hello")

	if got := fleet.sessions["web-01"].Written(); got != "echo hello" {
		t.Fatalf("web-01 received %q, want the pasted text", got)
	}
	if got := fleet.sessions["web-02"].Written(); got != "" {
		t.Fatalf("web-02 received %q, want nothing", got)
	}
}

// A multiline paste into a pane is not held: one pane is one host, which is
// the case the broadcast bar's hold explicitly exempts. The status line still
// names what went where, so the target of a paste is never a guess.
func TestMultilinePasteToAPaneIsSentWholeAndReported(t *testing.T) {
	a, fleet := typingApp(t, "web-01", "web-02")

	a = paste(t, a, "echo one\necho two\n")

	if got := fleet.sessions["web-01"].Written(); !strings.Contains(got, "echo one") ||
		!strings.Contains(got, "echo two") {
		t.Fatalf("web-01 received %q, want both pasted lines", got)
	}
	if a.pendingPaste != nil {
		t.Fatal("a paste to a single pane was held for review")
	}
	if got := a.LastDelivery(); !strings.Contains(got, "2 lines") || !strings.Contains(got, "web-01") {
		t.Fatalf("last delivery = %q, want the line count and the target host", got)
	}
}

// The paste goes through the host's emulator, not straight at its stdin, so
// a remote app that turned bracketed paste on gets its markers - the
// difference between a pasted script landing in the line editor and its lines
// executing as they arrive.
func TestPasteToAPaneIsBracketedWhenTheHostAskedForIt(t *testing.T) {
	a, fleet := typingApp(t, "web-01")

	// The remote app enables bracketed paste mode.
	fleet.sessions["web-01"].Emit("\x1b[?2004h")

	paste(t, a, "rm -rf /tmp/x")

	got := fleet.sessions["web-01"].Written()
	if !strings.Contains(got, "\x1b[200~") || !strings.Contains(got, "\x1b[201~") {
		t.Fatalf("web-01 received %q, want it wrapped in bracketed-paste markers", got)
	}
}

// A pane whose session cannot take input says so, the way typing into a dead
// pane does: a swallowed paste would read as a hung host.
func TestPasteToADisconnectedPaneReportsIt(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	if err := fleet.sessions["web-01"].Close(); err != nil {
		t.Fatalf("setup: close: %v", err)
	}

	a = paste(t, a, "echo hello")

	if got := a.LastDelivery(); !strings.Contains(got, "not connected") {
		t.Fatalf("last delivery = %q, want the not-connected notice", got)
	}
}

// A pane blocked on a password question takes the paste as the answer, the
// way it takes typing (issue #182): a passphrase is pasted more often than it
// is typed, and it must not reach a shell that is not there yet.
func TestPasteIntoAPaneWithAnOpenAuthQuestionAnswersIt(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	a = a.showSecretQuestion(SecretQuestionMsg{SessionID: "web-01", Host: "web-01", Prompt: "password: "})

	a = paste(t, a, "hunter2\nrm -rf /\n")

	q := a.authFor("web-01")
	if q == nil {
		t.Fatal("the question was closed by the paste")
	}
	if string(q.answer) != "hunter2" {
		t.Fatalf("answer = %q, want the paste's first line only", string(q.answer))
	}
	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("web-01 received %q, want nothing while the question is open", got)
	}
}

// A prompt owning the keyboard swallows the paste: the scrollback search is
// opened from a focused pane and leaves the focus on the grid, so without the
// guard a paste meant for the search box would go straight to the host.
func TestPasteIsSwallowedWhileAPromptOwnsTheKeyboard(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	a = press(t, a, tea.KeyPressMsg{Code: '/', Mod: tea.ModAlt})
	if !a.searchInput.Focused() {
		t.Fatal("setup: the search prompt does not have the keyboard")
	}

	paste(t, a, "echo hello")

	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("web-01 received %q, want nothing while a prompt has the keyboard", got)
	}
}

// The sidebar is not an input path: a paste there reaches no host, which is
// what it did before panes could take one.
func TestPasteWithSidebarFocusReachesNobody(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	a = press(t, a, tea.KeyPressMsg{Code: ']', Mod: tea.ModCtrl})
	if a.Focus() != AreaSidebar {
		t.Fatal("setup: the sidebar does not have focus")
	}

	paste(t, a, "echo hello")

	if got := fleet.sessions["web-01"].Written(); got != "" {
		t.Fatalf("web-01 received %q, want nothing", got)
	}
}
