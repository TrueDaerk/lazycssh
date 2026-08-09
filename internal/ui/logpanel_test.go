package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/commandlog"
)

// logApp builds an app on the Command log panel over a log with a fixed clock.
func logApp(t *testing.T, capacity int) (App, *commandlog.Log) {
	t.Helper()

	log := commandlog.New(capacity)
	at := time.Date(2026, 7, 28, 14, 5, 9, 0, time.UTC)
	log.SetClock(func() time.Time { return at })

	a := resize(t, NewApp(Config{
		Hosts:      []string{"web-01", "web-02"},
		CommandLog: log,
		Theme:      Options{Dark: true},
	}), 120, 40)

	return pressKey(t, a, "4"), log
}

// The acceptance criterion: a command sent to 40 hosts appears once with its
// target count, not 40 times.
func TestLogPanelShowsOneLinePerCommand(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record("systemctl restart nginx", broadcast.ModeAll, 40)

	view := plain(a.logPanel(70, 20))
	if got := strings.Count(view, "systemctl restart nginx"); got != 1 {
		t.Fatalf("the command appears %d times:\n%s", got, view)
	}
	if !strings.Contains(view, "40 hosts") || !strings.Contains(view, "[all]") {
		t.Fatalf("the entry does not carry its count and mode:\n%s", view)
	}
}

// The other acceptance criterion: passwords typed in single mode never appear.
func TestLogPanelNeverShowsSingleModeInput(t *testing.T) {
	const password = "hunter2-typed-at-a-sudo-prompt"

	a, log := logApp(t, 0)
	log.Record(password, broadcast.ModeSingle, 1)
	log.Record("uptime", broadcast.ModeAll, 2)

	view := plain(a.logPanel(70, 20))
	if strings.Contains(view, password) {
		t.Fatalf("the panel rendered single mode input:\n%s", view)
	}
	if !strings.Contains(view, "uptime") {
		t.Fatalf("the panel dropped a command it should show:\n%s", view)
	}
}

func TestLogPanelIsOldestFirst(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record("first", broadcast.ModeAll, 1)
	log.Record("second", broadcast.ModeAll, 2)

	view := plain(a.logPanel(70, 20))
	if strings.Index(view, "first") > strings.Index(view, "second") {
		t.Fatalf("the newest entry is not last:\n%s", view)
	}
}

// Resending sends to the current target set, not to the hosts the command
// originally reached.
func TestEnterResendsTheSelectedCommand(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record("uptime", broadcast.ModeAll, 2)
	log.Record("df -h", broadcast.ModeSelected, 1)

	a = pressKey(t, a, "j")
	if a.SelectedCommand() != "df -h" {
		t.Fatalf("SelectedCommand() = %q", a.SelectedCommand())
	}

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(CommandResendMsg)
	if !ok {
		t.Fatalf("the command produced a %T", cmd())
	}
	if msg.Command != "df -h" {
		t.Fatalf("CommandResendMsg = %+v", msg)
	}
}

// At the top of the list, up is a no-op: it never switches away from the
// Command log panel (issue #212).
func TestLogCursorMovesAndStaysInThePanelAtTheTop(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record("one", broadcast.ModeAll, 1)
	log.Record("two", broadcast.ModeAll, 1)

	a = pressKey(t, a, "j")
	if a.LogCursor() != 1 {
		t.Fatalf("LogCursor() = %d", a.LogCursor())
	}
	a = pressKey(t, a, "k")
	a = pressKey(t, a, "k")
	if a.LogCursor() != 0 {
		t.Fatalf("LogCursor() = %d after running off the top", a.LogCursor())
	}
	if a.Panel() != PanelCommandLog {
		t.Fatalf("Panel() = %v after moving off the top", a.Panel())
	}
}

// A log that quietly forgets is worse than one that says it forgot.
func TestDroppedEntriesAreReported(t *testing.T) {
	a, log := logApp(t, 2)
	for _, command := range []string{"one", "two", "three"} {
		log.Record(command, broadcast.ModeAll, 1)
	}

	view := plain(a.logPanel(70, 20))
	if !strings.Contains(view, "1 older entries dropped") {
		t.Fatalf("the panel does not report dropped entries:\n%s", view)
	}
	if strings.Contains(view, "one") && !strings.Contains(view, "older entries") {
		t.Fatalf("a dropped entry is still shown:\n%s", view)
	}
}

// A command that went to every host is drawn as a warning, so the audit trail
// reads the way the decision felt.
func TestFleetCommandsAreMarked(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record("uptime", broadcast.ModeAll, 2) // the cursor sits here
	log.Record("reboot", broadcast.ModeFleet, 40)

	entry, _ := log.Last()
	styled := a.logPanel(70, 20)
	if !strings.Contains(styled, a.Theme().StatusWarning.Render(entry.String())) {
		t.Fatalf("a fleet-wide command is not marked:\n%s", styled)
	}
}

func TestLogPanelWithoutALog(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "4")

	if got := plain(a.logPanel(70, 20)); !strings.Contains(got, "no command log") {
		t.Fatalf("logPanel() = %q", got)
	}
	if a.SelectedCommand() != "" {
		t.Fatalf("SelectedCommand() = %q", a.SelectedCommand())
	}
	// Enter must not panic without a log behind it.
	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd != nil {
		t.Fatal("enter produced a command with nothing to resend")
	}
}

func TestLogPanelWithNothingSent(t *testing.T) {
	a, _ := logApp(t, 0)
	if got := plain(a.logPanel(70, 20)); !strings.Contains(got, "nothing sent yet") {
		t.Fatalf("logPanel() = %q", got)
	}
}

// The log is in memory only: nothing here writes to disk. The Config carries no
// path and the panel offers no way to name one.
func TestLogPanelHasNoPathToDisk(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record("uptime", broadcast.ModeAll, 2)

	view := plain(a.logPanel(70, 20))
	for _, unwanted := range []string{"/", ".log", "written"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("the panel mentions a file (%q):\n%s", unwanted, view)
		}
	}
	_ = a
}

// The regression for issue 132: an entry wrapping over several visual lines
// must not push the window past the panel's height - the box clips the
// bottom, and the cursor entry was the first thing to vanish. The window is
// budgeted in visual lines, and up/down still moves exactly one entry.
func TestLogPanelBudgetsWrappedEntriesByVisualLines(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record(strings.Repeat("very-long-command ", 6), broadcast.ModeAll, 2) // wraps
	for i := 0; i < 5; i++ {
		log.Record(fmt.Sprintf("short-%d", i), broadcast.ModeAll, 2)
	}

	const width, height = 40, 5
	for cursor := 0; cursor < 6; cursor++ {
		a.logCursor = cursor
		panel := a.logPanel(width, height)
		if got := lipgloss.Height(panel); got > height {
			t.Fatalf("cursor %d: panel is %d lines high, want at most %d:\n%s",
				cursor, got, height, plain(panel))
		}
		want := a.logEntries()[cursor].Command
		want = strings.TrimSpace(want[:min(20, len(want))]) // the wrapped head identifies it
		if !strings.Contains(plain(panel), want) {
			t.Fatalf("cursor %d: the entry under the cursor is not visible:\n%s",
				cursor, plain(panel))
		}
	}
}

// Up/down moves one entry per keypress regardless of wrapping: the cursor is
// an entry index, never a visual line.
func TestLogCursorStepsOneEntryAcrossWrappedNeighbours(t *testing.T) {
	a, log := logApp(t, 0)
	log.Record("first", broadcast.ModeAll, 2)
	log.Record(strings.Repeat("wrapped ", 10), broadcast.ModeAll, 2)
	log.Record("last", broadcast.ModeAll, 2)

	a.logCursor = 0
	a = pressKey(t, a, "down")
	if got := a.SelectedCommand(); !strings.HasPrefix(got, "wrapped") {
		t.Fatalf("one down from the top selects %q", got)
	}
	a = pressKey(t, a, "down")
	if got := a.SelectedCommand(); got != "last" {
		t.Fatalf("two down from the top selects %q", got)
	}
	a = pressKey(t, a, "up")
	if got := a.SelectedCommand(); !strings.HasPrefix(got, "wrapped") {
		t.Fatalf("one up from the bottom selects %q", got)
	}
}
