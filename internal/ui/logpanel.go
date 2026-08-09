package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
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

// logPanel is the Command log child model: the cursor over the audit trail.
// The log itself is the run's, owned outside the UI; the panel reads it the
// way the legacy view did - at render time, through an interface that cannot
// write.
type logPanel struct {
	ctx  panelContext
	keys KeyMap

	// log is the run's command history, nil when the run has none.
	log CommandLog

	// cursor is the selected entry, counted from the oldest.
	cursor int
}

// Title is the heading shown in the sidebar.
func (p *logPanel) Title() string { return PanelCommandLog.Title() }

// Number is the key that jumps to this panel.
func (p *logPanel) Number() int { return PanelCommandLog.Number() }

// Cursor is the position of the cursor.
func (p *logPanel) Cursor() int { return p.cursor }

// entries returns the log, oldest first, or nothing when the run has no log.
func (p *logPanel) entries() []commandlog.Entry {
	if p.log == nil {
		return nil
	}
	return p.log.Entries()
}

// Selected is the command under the cursor.
func (p *logPanel) Selected() string {
	entries := p.entries()
	if len(entries) == 0 {
		return ""
	}
	return entries[clamp(p.cursor, 0, len(entries)-1)].Command
}

// MoveCursor nudges the cursor without committing - the wheel browses.
func (p *logPanel) MoveCursor(delta int) {
	p.cursor = clamp(p.cursor+delta, 0, max(0, len(p.entries())-1))
}

// SetCursorRow puts the cursor on a clicked visible body row.
func (p *logPanel) SetCursorRow(row int) {
	p.cursor = clamp(row, 0, max(0, len(p.entries())-1))
}

// Update drives the panel: the arrows move through the history and enter
// sends an entry again.
func (p *logPanel) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, p.keys.Up):
		p.MoveCursor(-1)
	case key.Matches(msg, p.keys.Down):
		p.MoveCursor(+1)
	case key.Matches(msg, p.keys.Choose):
		// Resent to the current target set, not the original one; see
		// [CommandResendMsg].
		command := p.Selected()
		if command == "" {
			return nil
		}
		return func() tea.Msg { return CommandResendMsg{Command: command} }
	}
	return nil
}

// View renders the command history, newest last, each with the number of
// hosts it went to and the mode it went out in. focused reports whether this
// panel is the one that would actually receive a keystroke right now (issue
// #222).
func (p *logPanel) View(focused bool, width, height int) string {
	theme := p.ctx.theme
	if p.log == nil {
		return theme.Muted.Render("no command log")
	}

	entries := p.entries()
	if len(entries) == 0 {
		return theme.Muted.Render("nothing sent yet")
	}

	cursor := clamp(p.cursor, 0, len(entries)-1)

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
		s := theme.Base.Width(max(0, width)).Render(p.line(entries[i], i == cursor, focused))
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
	if dropped := p.log.Dropped(); dropped > 0 && first == 0 {
		// A log that quietly forgets is worse than one that says it forgot.
		notice = theme.Muted.Render(fmt.Sprintf("(%d older entries dropped)", dropped))
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

// line renders one entry. A command that went to every host is drawn in the
// warning style: the audit trail should read the way the decision felt.
// focused decides whether the cursor row gets the strong highlight or the
// muted one; see [Theme.ListCursor].
func (p *logPanel) line(entry commandlog.Entry, underCursor, focused bool) string {
	theme := p.ctx.theme
	label := entry.String()
	switch {
	case underCursor:
		return theme.ListCursor(focused).Render(label)
	case entry.Mode == broadcast.ModeFleet:
		return theme.StatusWarning.Render(label)
	default:
		return theme.Base.Render(label)
	}
}

// Preview shows the whole of the command under the cursor: the list truncates
// a long command to its row, and "what exactly did I send" is the question
// the log exists to answer (issue #218).
func (p *logPanel) Preview(width, height int) (string, string, bool) {
	theme := p.ctx.theme
	entries := p.entries()
	if len(entries) == 0 {
		if p.log == nil {
			return "Command", fitLines(theme, width, height,
				[]string{theme.Muted.Render("no command log")}), true
		}
		return "Command", fitLines(theme, width, height,
			[]string{theme.Muted.Render("nothing sent yet")}), true
	}
	entry := entries[clamp(p.cursor, 0, len(entries)-1)]

	scope := fmt.Sprintf("%s → %d host%s", entry.Mode, entry.Targets, plural(entry.Targets))
	scopeLine := field(theme, "scope", scope)
	if entry.Mode == broadcast.ModeFleet {
		// A command that went to every host reads here the way it read in the
		// list: the audit trail should feel like the decision did.
		scopeLine = theme.StatusWarning.Render(scope)
	}

	lines := []string{
		field(theme, "sent", entry.At.Format("2006-01-02 15:04:05")),
		scopeLine,
		"",
		theme.Base.Render(entry.Command),
	}
	return "Command", fitLines(theme, width, height, lines), true
}

// The root's accessors: what the rest of the model and the tests ask the
// Command log panel about.

// LogCursor is the position of the cursor in the Command log panel, counted
// from the newest entry.
func (a App) LogCursor() int { return a.panels.log.Cursor() }

// SelectedCommand is the command under the Command log cursor.
func (a App) SelectedCommand() string { return a.panels.log.Selected() }
