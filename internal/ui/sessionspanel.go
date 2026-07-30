package ui

import (
	"errors"
	"fmt"
	"strings"

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

// SessionCursor is the position of the cursor in the Sessions panel.
func (a App) SessionCursor() int { return a.sessionCursor }

// SelectedOpenSession is the open session under the cursor, or the empty
// string when nothing is open.
func (a App) SelectedOpenSession() string {
	if len(a.open) == 0 {
		return ""
	}
	return a.open[clamp(a.sessionCursor, 0, len(a.open)-1)].Name
}

// Saving reports whether the save-as prompt has the keyboard.
func (a App) Saving() bool { return a.saveInput.Focused() || a.confirmOverwrite }

// SaveError is the last save failure, or nil.
func (a App) SaveError() error { return a.saveErr }

// moveSessionCursor moves the cursor, stopping at the ends.
func (a App) moveSessionCursor(delta int) App {
	a.sessionCursor = clamp(a.sessionCursor+delta, 0, max(0, len(a.open)-1))
	return a
}

// EndSessionPending is the session the end question is about, empty when
// none is being asked.
func (a App) EndSessionPending() string { return a.endSession }

// beginEndSession opens the end question for the session under the cursor.
// ctrl+c on N machines is not sent without the user answering for it.
func (a App) beginEndSession() App {
	a.endSession = a.SelectedOpenSession()
	return a
}

// handleSessionEndKey answers the end question: y sends the shutdown
// keystrokes, anything else withdraws the question.
func (a App) handleSessionEndKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	name := a.endSession
	a.endSession = ""
	switch msg.String() {
	case "y", "Y":
		return a.endSessionNow(name)
	default:
		return a, nil
	}
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
			// ctrl+c, and the ctrl+d reaches the prompt that follows.
			_, _ = w.Write([]byte{0x03})
			_, _ = w.Write([]byte{0x04})
		}
		break
	}
	// Hosts that are already done end the session right here rather than
	// waiting for a fleet event that will never come.
	return a.reapSessions()
}

// foregroundSelectedSession brings the open session under the cursor to the
// foreground. Nothing is dialled: the panes and the broadcast scope change,
// the connections do not.
func (a App) foregroundSelectedSession() (App, tea.Cmd) {
	if len(a.open) == 0 {
		return a, nil
	}
	index := clamp(a.sessionCursor, 0, len(a.open)-1)
	if index == a.active {
		return a, nil
	}
	return a.foregroundSession(index), gridChanged()
}

// beginSave opens the save-as prompt, pre-filled with the foreground session's
// name, because "save it again under the same name" is the common case.
func (a App) beginSave() App {
	a.saveErr = nil
	name := a.ActiveSession()
	if name == adHocSessionName {
		name = ""
	}
	a.saveInput.SetValue(name)
	a.saveInput.CursorEnd()
	a.saveInput.Focus()
	return a
}

// cancelSave closes the prompt without writing anything.
func (a App) cancelSave() App {
	a.saveInput.Blur()
	a.saveInput.SetValue("")
	a.confirmOverwrite = false
	return a
}

// commitSave writes the run as a group. An existing name is not replaced until
// the user has said so: the first attempt asks, the second overwrites.
func (a App) commitSave(overwrite bool) (App, tea.Cmd) {
	name := strings.TrimSpace(a.saveInput.Value())
	if name == "" || a.cfg.Sessions == nil {
		return a.cancelSave(), nil
	}

	run := sessions.Run{
		Name:      name,
		Patterns:  a.cfg.RunPatterns,
		Defaults:  a.cfg.RunDefaults,
		Broadcast: a.broadcastMode(),
	}
	if a.cfg.WorkingSet != nil {
		run.WorkingSet = a.cfg.WorkingSet.Active()
	}
	if len(run.Patterns) == 0 {
		// Nothing was recorded about how the run was started, so the hosts as
		// they are now is the honest thing to write.
		run.Patterns = a.fleetIDs()
	}
	if len(run.Patterns) == 0 {
		// An empty run cannot be saved, but the typed name must survive the
		// telling: the user may connect a host and press enter again.
		a.saveErr = errors.New("nothing to save: no hosts in the run")
		return a, nil
	}

	if _, err := a.cfg.Sessions.SaveRun(run, overwrite); err != nil {
		if errors.Is(err, sessions.ErrExists) {
			a.confirmOverwrite = true
			a.saveInput.Blur()
			return a, nil
		}
		a.saveErr = err
		return a.cancelSave(), nil
	}

	a = a.cancelSave()
	a.cfg.SessionName = name
	return a, func() tea.Msg { return SessionsChangedMsg{} }
}

// broadcastMode is the mode the run is in, defaulting to all when there is no
// router yet.
func (a App) broadcastMode() broadcast.Mode {
	if a.cfg.Targets == nil {
		return broadcast.ModeAll
	}
	return a.cfg.Targets.Mode()
}

// sessionsPanel renders the open sessions: which exist, which is in the
// foreground, and how many of each one's hosts are up.
func (a App) sessionsPanel(width, height int) string {
	var b strings.Builder

	if a.endSession != "" {
		b.WriteString(a.theme.StatusWarning.Render(
			fmt.Sprintf("end %q? y/n — ctrl+c and ctrl+d go to its hosts", a.endSession)))
		b.WriteString("\n")
	}

	if len(a.open) == 0 {
		b.WriteString(a.theme.Muted.Render("no open sessions — open a group in [2]"))
		return a.theme.Base.Width(max(0, width)).Render(b.String())
	}

	cursor := clamp(a.sessionCursor, 0, len(a.open)-1)
	first, last := visibleRange(cursor, len(a.open), max(1, height))
	for i := first; i < last; i++ {
		if i > first {
			b.WriteString("\n")
		}
		b.WriteString(a.openSessionLine(a.open[i], i == cursor, i == a.active))
	}
	if hidden := len(a.open) - last; hidden > 0 {
		b.WriteString("\n")
		b.WriteString(a.theme.Muted.Render(fmt.Sprintf("+%d more", hidden)))
	}

	return a.theme.Base.Width(max(0, width)).Render(b.String())
}

// openSessionLine renders one open session. The foreground one is marked with
// a character as well as a style, so it survives a terminal without colour.
func (a App) openSessionLine(s openSession, underCursor, foreground bool) string {
	up := 0
	for _, id := range s.Hosts {
		if a.state(id) == ssh.StateConnected {
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
		return a.theme.Cursor.Render(label)
	case foreground:
		return a.theme.Selected.Render(label)
	default:
		return a.theme.Base.Render(label)
	}
}
