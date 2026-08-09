package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/commandlog"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
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

// CommandResendMissingMsg asks the program to send a logged command to the
// hosts that did *not* receive it the first time.
//
// It is the deliberate opposite of [CommandResendMsg]: a host that reconnects
// after the fleet already ran a command missed it, and re-sending to the whole
// scope would double-execute on the machines that did not miss it (issue
// #256). It carries the entry's original target set rather than the resolved
// difference, so the hosts that are connected *now* are read where the message
// lands rather than a frame earlier.
type CommandResendMissingMsg struct {
	// Command is the command to send again.
	Command string
	// Mode is the broadcast mode the original send went out in, which is what
	// the new audit entry records: the resend repeats that decision, it does
	// not make a new one.
	Mode broadcast.Mode
	// Received are the hosts the command reached the first time.
	Received []string
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
	entry, ok := p.selectedEntry()
	if !ok {
		return ""
	}
	return entry.Command
}

// selectedEntry is the whole entry under the cursor, ok false on an empty log.
func (p *logPanel) selectedEntry() (commandlog.Entry, bool) {
	entries := p.entries()
	if len(entries) == 0 {
		return commandlog.Entry{}, false
	}
	return entries[clamp(p.cursor, 0, len(entries)-1)], true
}

// connectedHosts are the run's hosts that can take input right now, in host
// order, read from the fleet snapshot the panel was drawn with.
func (p *logPanel) connectedHosts() []string {
	var out []string
	for _, id := range p.ctx.fleetIDs {
		if stateOf(p.ctx.hostStates, id) == ssh.StateConnected {
			out = append(out, id)
		}
	}
	return out
}

// missing are the hosts that are up now and were not targets of an entry - the
// resolved target list of "send to missing", shown before the send so the
// count is never a surprise.
func (p *logPanel) missing(entry commandlog.Entry) []string {
	return entry.Missing(p.connectedHosts())
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
	case key.Matches(msg, p.keys.SendMissing):
		// The complement of Choose: only the hosts that never got this one.
		entry, ok := p.selectedEntry()
		if !ok {
			return nil
		}
		msg := CommandResendMissingMsg{
			Command:  entry.Command,
			Mode:     entry.Mode,
			Received: entry.Hosts,
		}
		return func() tea.Msg { return msg }
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

	targets := entry.Targets()
	scope := fmt.Sprintf("%s → %d host%s", entry.Mode, targets, plural(targets))
	scopeLine := field(theme, "scope", scope)
	if entry.Mode == broadcast.ModeFleet {
		// A command that went to every host reads here the way it read in the
		// list: the audit trail should feel like the decision did.
		scopeLine = theme.StatusWarning.Render(scope)
	}

	lines := []string{
		field(theme, "sent", entry.At.Format("2006-01-02 15:04:05")),
		scopeLine,
	}
	lines = append(lines, p.missingLines(entry)...)
	lines = append(lines, "", theme.Base.Render(entry.Command))
	return "Command", fitLines(theme, width, height, lines), true
}

// missingLines are the preview's answer to "who would `m` send this to". The
// resolved list is on screen *before* the key is pressed, which is the same
// rule the broadcast label follows: the number of machines about to receive a
// command is never a surprise.
func (p *logPanel) missingLines(entry commandlog.Entry) []string {
	theme := p.ctx.theme
	missing := p.missing(entry)
	if len(missing) == 0 {
		return []string{theme.Muted.Render("all hosts already received this")}
	}
	return []string{
		theme.StatusWarning.Render(fmt.Sprintf("missing → %d host%s", len(missing), plural(len(missing)))),
		theme.Base.Render(strings.Join(missing, " ")),
	}
}

// The root's accessors: what the rest of the model and the tests ask the
// Command log panel about.

// connectedHosts are the hosts that can take input right now, in host order,
// read from the fleet snapshot Update took.
func (a App) connectedHosts() []string {
	var out []string
	for _, id := range a.fleetIDs() {
		if a.state(id) == ssh.StateConnected {
			out = append(out, id)
		}
	}
	return out
}

// resendMissing sends a logged command to the hosts that are up now and were
// not among its original targets (issue #256) - the host that reconnected into
// a fleet that has already run three commands, without running them a fourth
// time everywhere else.
//
// The difference is resolved here, not in the panel: the panel's preview shows
// it so the count is on screen before the key is pressed, but what is actually
// sent is computed against the snapshot this message lands on. With nothing
// missing the action is a true no-op that says so.
func (a App) resendMissing(msg CommandResendMissingMsg) (tea.Model, tea.Cmd) {
	command := strings.TrimSpace(msg.Command)
	if command == "" {
		return a, nil
	}

	missing := commandlog.Missing(msg.Received, a.connectedHosts())
	if len(missing) == 0 {
		a.lastDelivery = "all hosts already received this"
		return a, nil
	}

	// The count is in the report before the delivery's own, so the status bar
	// says how many machines this was aimed at even when every write fails.
	a.lastDelivery = fmt.Sprintf("sending to %d missing host%s", len(missing), plural(len(missing)))
	next, cmd := a.sendCommandTo(command, msg.Mode, missing)
	return next, cmd
}

// LogCursor is the position of the cursor in the Command log panel, counted
// from the newest entry.
func (a App) LogCursor() int { return a.panels.log.Cursor() }

// SelectedCommand is the command under the Command log cursor.
func (a App) SelectedCommand() string { return a.panels.log.Selected() }
