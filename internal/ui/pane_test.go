package ui

import (
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/scrollback"
)

// The pane shows the newest output: watching the tail is what the grid is for.
func TestPaneBodyFollowsTheTail(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 30; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}

	body := plain(a.paneBody("web-01", 40, 5))
	if !strings.Contains(body, "line-30") {
		t.Fatalf("the newest line is missing:\n%s", body)
	}
	if strings.Contains(body, "line-01") {
		t.Fatalf("the oldest line pushed the newest out:\n%s", body)
	}
	if got := len(strings.Split(body, "\n")); got > 5 {
		t.Fatalf("body is %d lines, want at most 5:\n%s", got, body)
	}
}

// The acceptance criterion: output from `ls --color` looks right - the colours
// are still there after sanitizing and wrapping.
func TestPaneBodyPreservesColors(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("\x1b[01;34mbin\x1b[0m  \x1b[01;36mlink\x1b[0m  file.txt\n")

	body := a.paneBody("web-01", 40, 5)
	if !strings.Contains(body, "\x1b[01;34m") {
		t.Fatalf("the directory colour is gone:\n%q", body)
	}
	if !strings.Contains(plain(body), "bin") || !strings.Contains(plain(body), "file.txt") {
		t.Fatalf("the text is gone:\n%q", body)
	}
}

// The acceptance criterion: a remote program emitting cursor escapes cannot
// corrupt the surrounding layout. Every rendered line of the whole frame stays
// exactly as wide as the terminal.
func TestCursorEscapesCannotCorruptTheLayout(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02")
	fleet.sessions["web-01"].Emit("\x1b[2J\x1b[H\x1b[10;20Hboom\x1b[5A\x1b]0;title\a\n")
	fleet.sessions["web-02"].Emit("calm\n")

	view := plain(a.View().Content)
	for i, line := range strings.Split(view, "\n") {
		if got := len([]rune(line)); got != 120 {
			t.Fatalf("line %d is %d columns wide, want 120:\n%s", i, got, view)
		}
	}
	if !strings.Contains(view, "boom") || !strings.Contains(view, "calm") {
		t.Fatalf("the output text is missing:\n%s", view)
	}
}

// A line longer than the pane wraps rather than widening the pane.
func TestPaneBodyWrapsLongLines(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit(strings.Repeat("abcdefghij", 5) + "\n")

	body := plain(a.paneBody("web-01", 20, 10))
	lines := strings.Split(body, "\n")
	if len(lines) < 3 {
		t.Fatalf("a 50-column line did not wrap at 20:\n%s", body)
	}
	for i, line := range lines {
		if len([]rune(line)) > 20 {
			t.Fatalf("wrapped line %d is %d columns wide: %q", i, len([]rune(line)), line)
		}
	}
	if got := strings.Join(lines, ""); got != strings.Repeat("abcdefghij", 5) {
		t.Fatalf("wrapping lost content: %q", got)
	}
}

// The acceptance criterion for the epic: truncated scrollback is visible, not
// silent. The marker sits where the missing output was.
func TestPaneBodyMarksDroppedOutput(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].UseScrollback(scrollback.New(5))
	for i := 1; i <= 12; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}

	body := plain(a.paneBody("web-01", 40, 10))
	if !strings.Contains(body, "7 lines dropped") {
		t.Fatalf("no dropped marker:\n%s", body)
	}
	if !strings.HasPrefix(body, "~ 7 lines dropped ~") {
		t.Fatalf("the marker is not where the missing output was:\n%s", body)
	}
}

// The pane renders through View as well: the grid shows the output, not just
// the host name.
func TestPaneOutputReachesTheFrame(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("$ uptime\n 17:02:11 up 42 days\n")

	view := plain(a.View().Content)
	if !strings.Contains(view, "up 42 days") {
		t.Fatalf("the pane does not show the output:\n%s", view)
	}
}

// Output only asks for a redraw; there is nothing to store and no command to
// run.
func TestSessionOutputMsgRedraws(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	before := a.View().Content

	fleet.sessions["web-01"].Emit("fresh output\n")
	model, cmd := a.Update(SessionOutputMsg{ID: "web-01"})
	if cmd != nil {
		t.Fatal("SessionOutputMsg produced a command")
	}
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if next.View().Content == before {
		t.Fatal("new output changed nothing on screen")
	}
}

// Degenerate sizes and missing sessions render as nothing rather than
// panicking: the pane must never take the program down.
func TestPaneBodyDegenerateCases(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01")
	fleet.sessions["web-01"].Emit("text\n")

	if got := a.paneBody("web-01", 0, 5); got != "" {
		t.Fatalf("zero width rendered %q", got)
	}
	if got := a.paneBody("web-01", 20, 0); got != "" {
		t.Fatalf("zero height rendered %q", got)
	}
	if got := a.paneBody("web-99", 20, 5); got != "" {
		t.Fatalf("an unknown host rendered %q", got)
	}

	quiet := a.paneBody("web-01", 20, 5)
	if plain(quiet) != "text" {
		t.Fatalf("a quiet session renders %q", quiet)
	}

	noFleet := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	if got := noFleet.paneBody("h1", 20, 5); got != "" {
		t.Fatalf("a run without a fleet rendered %q", got)
	}
}
