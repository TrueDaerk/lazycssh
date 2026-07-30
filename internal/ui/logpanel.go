package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/commandlog"
)

// CommandResendMsg asks the program to send a command from the log again.
//
// It carries the command, not the hosts it originally went to: resending is
// "run this again on what I am working on now", and the target set is whatever
// the broadcast scope currently says. Replaying the old target list would send a
// command to machines the user has since paged away from.
type CommandResendMsg struct {
	// Command is the command to send again.
	Command string
}

// CommandLog is what the panel needs from the run's command history.
type CommandLog interface {
	// Entries returns the commands sent this run, oldest first.
	Entries() []commandlog.Entry
	// Dropped is how many entries fell off the start of a full log.
	Dropped() int
}

// LogCursor is the position of the cursor in the Command log panel, counted
// from the newest entry.
func (a App) LogCursor() int { return a.logCursor }

// SelectedCommand is the command under the Command log cursor.
func (a App) SelectedCommand() string {
	entries := a.logEntries()
	if len(entries) == 0 {
		return ""
	}
	return entries[clamp(a.logCursor, 0, len(entries)-1)].Command
}

// logEntries returns the log, oldest first, or nothing when the run has no log.
func (a App) logEntries() []commandlog.Entry {
	if a.cfg.CommandLog == nil {
		return nil
	}
	return a.cfg.CommandLog.Entries()
}

// moveLogCursor moves the cursor, stopping at the ends.
func (a App) moveLogCursor(delta int) App {
	a.logCursor = clamp(a.logCursor+delta, 0, max(0, len(a.logEntries())-1))
	return a
}

// resendSelectedCommand asks the program to send the command under the cursor
// again, to the current target set.
func (a App) resendSelectedCommand() (App, tea.Cmd) {
	command := a.SelectedCommand()
	if command == "" {
		return a, nil
	}
	return a, func() tea.Msg { return CommandResendMsg{Command: command} }
}

// logPanel renders the command history, newest last, each with the number of
// hosts it went to and the mode it went out in.
func (a App) logPanel(width, height int) string {
	if a.cfg.CommandLog == nil {
		return a.theme.Muted.Render("no command log")
	}

	entries := a.logEntries()
	if len(entries) == 0 {
		return a.theme.Muted.Render("nothing sent yet")
	}

	cursor := clamp(a.logCursor, 0, len(entries)-1)

	// The window is budgeted in visual lines, not entries: a command longer
	// than the panel wraps over several lines, and counting it as one row
	// would push the rows below it - the cursor among them - past the box's
	// clip (issue #132). Each entry is wrapped on its own so its height is
	// known before it is admitted.
	rendered := make(map[int]string, height)
	line := func(i int) string {
		if s, ok := rendered[i]; ok {
			return s
		}
		s := a.theme.Base.Width(max(0, width)).Render(a.logLine(entries[i], i == cursor))
		rendered[i] = s
		return s
	}

	avail := max(1, height)
	first, last := cursor, cursor+1
	lines := lipgloss.Height(line(cursor))
	for {
		grew := false
		if last < len(entries) && lines+lipgloss.Height(line(last)) <= avail {
			lines += lipgloss.Height(line(last))
			last++
			grew = true
		}
		if first > 0 && lines+lipgloss.Height(line(first-1)) <= avail {
			lines += lipgloss.Height(line(first - 1))
			first--
			grew = true
		}
		if !grew {
			break
		}
	}

	notice := ""
	if dropped := a.cfg.CommandLog.Dropped(); dropped > 0 && first == 0 {
		// A log that quietly forgets is worse than one that says it forgot.
		notice = a.theme.Muted.Render(fmt.Sprintf("(%d older entries dropped)", dropped))
		// The notice takes its line back from the bottom of the window; the
		// cursor entry is never given up - past that, the notice is.
		for lines+1 > avail && last-1 > cursor {
			last--
			lines -= lipgloss.Height(line(last))
		}
		if lines+1 > avail {
			notice = ""
		}
	}

	var b strings.Builder
	if notice != "" {
		b.WriteString(notice)
		b.WriteString("\n")
	}
	for i := first; i < last; i++ {
		if i > first {
			b.WriteString("\n")
		}
		b.WriteString(line(i))
	}
	return b.String()
}

// logLine renders one entry. A command that went to every host is drawn in the
// warning style: the audit trail should read the way the decision felt.
func (a App) logLine(entry commandlog.Entry, underCursor bool) string {
	label := entry.String()
	switch {
	case underCursor:
		return a.theme.Cursor.Render(label)
	case entry.Mode == broadcast.ModeFleet:
		return a.theme.StatusWarning.Render(label)
	default:
		return a.theme.Base.Render(label)
	}
}
