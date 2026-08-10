package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// SessionStore is what the interface needs from the on-disk group store. It is
// the subset of [sessions.Store] the interface uses, so the panels can be
// tested against a fake and cannot reach past it.
type SessionStore interface {
	// List returns the saved group names in lexical order.
	List() ([]string, error)
	// Load reads one group.
	Load(name string) (*sessions.Session, error)
	// Exists reports whether a name is taken.
	Exists(name string) bool
	// SaveRun writes the live run as a named group. overwrite false returns
	// [sessions.ErrExists] rather than replacing.
	SaveRun(run sessions.Run, overwrite bool) (*sessions.Session, error)
	// Remove deletes a group file.
	Remove(name string) error
}

// SessionsChangedMsg asks the Groups panel to re-read the group directory. It
// is sent after a save or a delete and can be sent by anything that changes
// the directory.
type SessionsChangedMsg struct{}

// sessionsPanel is the Sessions child model: the cursor over the open
// sessions, and the end-session question. The open sessions themselves are
// domain state - the grid and the broadcast scope hang off them - so they stay
// on the root and arrive here as the [panelContext] snapshot; bringing one to
// the foreground is asked for through the chosen mailbox the root drains right
// after routing the key.
type sessionsPanel struct {
	ctx  panelContext
	keys *KeyMap

	// cursor is the selected row.
	cursor int

	// endPending is the open session the x key asked about; the question
	// floats until y answers it or anything else withdraws it.
	endPending string

	// chosen is the row enter or space committed, -1 while none is. The
	// foreground switch is the root's move; this is how the panel asks for
	// it without reaching past its interface.
	chosen int
}

// Title is the heading shown in the sidebar.
func (p *sessionsPanel) Title() string { return PanelSessions.Title() }

// Number is the key that jumps to this panel.
func (p *sessionsPanel) Number() int { return PanelSessions.Number() }

// Cursor is the position of the cursor.
func (p *sessionsPanel) Cursor() int { return p.cursor }

// Selected is the open session under the cursor, or the empty string when
// nothing is open.
func (p *sessionsPanel) Selected() string {
	if len(p.ctx.open) == 0 {
		return ""
	}
	return p.ctx.open[clamp(p.cursor, 0, len(p.ctx.open)-1)].Name
}

// EndPending is the session the end question is about, empty when none is
// being asked.
func (p *sessionsPanel) EndPending() string { return p.endPending }

// clampCursor keeps the cursor inside the list after sessions left it. The
// root calls it whenever it pushes a fresh snapshot.
func (p *sessionsPanel) clampCursor() {
	p.cursor = clamp(p.cursor, 0, max(0, len(p.ctx.open)-1))
}

// MoveCursor nudges the cursor without committing - the wheel browses.
func (p *sessionsPanel) MoveCursor(delta int) {
	p.cursor = clamp(p.cursor+delta, 0, max(0, len(p.ctx.open)-1))
}

// SetCursorRow puts the cursor on a clicked visible body row.
func (p *sessionsPanel) SetCursorRow(row int) {
	p.cursor = clamp(row, 0, max(0, len(p.ctx.open)-1))
}

// Update drives the panel: the arrows move through the open sessions, enter
// or space asks for one in the foreground, and x opens the end question.
func (p *sessionsPanel) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, p.keys.Up):
		p.MoveCursor(-1)
	case key.Matches(msg, p.keys.Down):
		p.MoveCursor(+1)
	case key.Matches(msg, p.keys.Choose), key.Matches(msg, p.keys.Toggle):
		// Nothing is dialled: the panes and the broadcast scope change, the
		// connections do not. Choosing the foreground again is not a change.
		if len(p.ctx.open) == 0 {
			return nil
		}
		if index := clamp(p.cursor, 0, len(p.ctx.open)-1); index != p.ctx.active {
			p.chosen = index
		}
	case key.Matches(msg, p.keys.SessionEnd):
		// ctrl+c on N machines is not sent without the user answering for it.
		p.endPending = p.Selected()
	}
	return nil
}

// takeChosen drains the foreground request: the committed row, and whether
// one was committed since the last drain.
func (p *sessionsPanel) takeChosen() (int, bool) {
	index := p.chosen
	p.chosen = -1
	return index, index >= 0
}

// answerEnd reads a key as the end question's answer and reports the session
// it was about. asked stays false until a yes or no lands - anything else
// leaves the question standing (see [readConfirm]).
func (p *sessionsPanel) answerEnd(msg tea.KeyPressMsg) (name string, confirmed, asked bool) {
	answer := readConfirm(p.keys, msg)
	if answer == answerNone {
		return "", false, false
	}
	name = p.endPending
	p.endPending = ""
	return name, answer == answerYes, true
}

// modal is the end-session question while it is open.
func (p *sessionsPanel) modal() (modal, bool) {
	if p.endPending == "" {
		return modal{}, false
	}
	return confirmModal(p.ctx.theme, p.keys, "End session",
		fmt.Sprintf("end %q?", p.endPending),
		"ctrl+c and ctrl+d go to its hosts"), true
}

// View renders the open sessions: which exist, which is in the foreground,
// and how many of each one's hosts are up. The end question this panel asks
// floats over the frame rather than taking its first line; see modal.go.
// focused reports whether this panel is the one that would actually receive a
// keystroke right now (issue #222).
func (p *sessionsPanel) View(focused bool, width, height int) string {
	theme := p.ctx.theme
	var b strings.Builder

	if len(p.ctx.open) == 0 {
		b.WriteString(theme.Muted.Render("no open sessions — open a group in [2]"))
		return theme.Base.Width(max(0, width)).Render(b.String())
	}

	cursor := clamp(p.cursor, 0, len(p.ctx.open)-1)
	first, last := visibleRange(cursor, len(p.ctx.open), max(1, height))
	for i := first; i < last; i++ {
		if i > first {
			b.WriteString("\n")
		}
		b.WriteString(p.line(p.ctx.open[i], i == cursor, i == p.ctx.active, focused))
	}
	if hidden := len(p.ctx.open) - last; hidden > 0 {
		b.WriteString("\n")
		b.WriteString(theme.Muted.Render(fmt.Sprintf("+%d more", hidden)))
	}

	return theme.Base.Width(max(0, width)).Render(b.String())
}

// line renders one open session. The foreground one is marked with a
// character as well as a style, so it survives a terminal without colour.
// focused decides whether the cursor row gets the strong highlight or the
// muted one; see [Theme.ListCursor].
func (p *sessionsPanel) line(s openSession, underCursor, foreground, focused bool) string {
	theme := p.ctx.theme
	up := 0
	for _, id := range s.Hosts {
		if stateOf(p.ctx.hostStates, id) == ssh.StateConnected {
			up++
		}
	}

	marker := "  "
	if foreground {
		marker = "▸ "
	}
	label := fmt.Sprintf("%s%s (%d/%d up)", marker, s.Name, up, len(s.Hosts))
	if s.Ending {
		label += " (ending)"
	}
	switch {
	case underCursor:
		return theme.ListCursor(focused).Render(label)
	case foreground:
		return theme.Selected.Render(label)
	default:
		return theme.Base.Render(label)
	}
}

// Preview describes the open session under the cursor: whether it is the one
// on screen, and how each of its hosts is doing (issue #218).
func (p *sessionsPanel) Preview(width, height int) (string, string, bool) {
	theme := p.ctx.theme
	if len(p.ctx.open) == 0 {
		return "Session", fitLines(theme, width, height,
			[]string{theme.Muted.Render("no open sessions")}), true
	}
	index := clamp(p.cursor, 0, len(p.ctx.open)-1)
	s := p.ctx.open[index]

	where := "background"
	if index == p.ctx.active {
		where = "foreground"
	}
	if s.Ending {
		where += ", ending"
	}

	up := 0
	hosts := make([]string, 0, len(s.Hosts))
	for _, id := range s.Hosts {
		if id == "" {
			// A hole is a grid position a closed host left behind, not a host.
			continue
		}
		state := stateOf(p.ctx.hostStates, id)
		if state == ssh.StateConnected {
			up++
		}
		line := theme.Base.Render("  "+id+"  ") + theme.State(state).Render(state.String())
		if err := stateErrOf(p.ctx.hostStates, id); err != "" {
			line += theme.Failure.Render("  " + err)
		}
		hosts = append(hosts, line)
	}

	lines := []string{
		field(theme, "state", where),
		field(theme, "hosts", fmt.Sprintf("%d/%d up", up, len(hosts))),
		"",
	}
	if len(hosts) == 0 {
		lines = append(lines, theme.Muted.Render("no hosts"))
	}
	lines = append(lines, hosts...)
	return "Session — " + s.Name, fitLines(theme, width, height, lines), true
}

// The root's accessors and the domain moves only the root can make.

// SessionCursor is the position of the cursor in the Sessions panel.
func (a App) SessionCursor() int { return a.panels.sessions.Cursor() }

// SelectedOpenSession is the open session under the cursor, or the empty
// string when nothing is open.
func (a App) SelectedOpenSession() string { return a.panels.sessions.Selected() }

// EndSessionPending is the session the end question is about, empty when
// none is being asked.
func (a App) EndSessionPending() string { return a.panels.sessions.EndPending() }

// handleSessionEndKey answers the end question: enter or y sends the shutdown
// keystrokes, esc or n withdraws the question, and anything else leaves it
// standing. The question lives in the panel; ending a session touches the
// open list and the transport, which is the root's move.
func (a App) handleSessionEndKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name, confirmed, asked := a.panels.sessions.answerEnd(msg)
	if !asked {
		return a, nil
	}
	if !confirmed {
		return a, nil
	}
	return a.endSessionNow(name)
}

// endSessionNow marks a session as ending and sends every connected terminal
// ctrl+c then ctrl+d: interrupt the foreground process, then log the shell
// out. The session leaves the list once its hosts are done - the remote
// shells exit on their own time, and the fleet events drive the reaping. A
// shell that swallows the keystrokes keeps its session listed as ending;
// x asks again and resends.
//
// The bytes travel the pane path, not the broadcast router: this targets the
// session's hosts exactly, whatever the broadcast mode is, and keystrokes are
// never recorded.
func (a App) endSessionNow(name string) (App, tea.Cmd) {
	for i := range a.open {
		if a.open[i].Name != name {
			continue
		}
		a.open[i].Ending = true
		if a.cfg.Panes == nil {
			break
		}
		for _, id := range a.open[i].Hosts {
			w, ok := a.cfg.Panes.Writer(id)
			if !ok {
				continue
			}
			// Two writes, interrupt first: a foreground process dies on the
			// ctrl+c, and the ctrl+d reaches the prompt that follows. Writing
			// inline is safe: a session's Write enqueues on its stdin queue
			// and never blocks on the network (issue #225).
			_, _ = w.Write([]byte{0x03})
			_, _ = w.Write([]byte{0x04})
		}
		break
	}
	// Hosts that are already done end the session right here rather than
	// waiting for a fleet event that will never come.
	return a.reapSessions()
}

// broadcastMode is the mode the run is in, defaulting to all when there is no
// router yet.
func (a App) broadcastMode() broadcast.Mode {
	if a.cfg.Targets == nil {
		return broadcast.ModeAll
	}
	return a.cfg.Targets.Mode()
}
