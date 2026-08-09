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

// Saving reports whether the save-as prompt has the keyboard - including the
// moment a write is in flight, so no other binding can slip in between the
// enter and its result.
func (a App) Saving() bool {
	return a.saveInput.Focused() || a.confirmOverwrite || a.savePending
}

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

// handleSessionEndKey answers the end question: enter or y sends the shutdown
// keystrokes, esc or n withdraws the question, and anything else leaves it
// standing (see [readConfirm]).
func (a App) handleSessionEndKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	answer := readConfirm(msg)
	if answer == answerNone {
		return a, nil
	}

	name := a.endSession
	a.endSession = ""
	if answer == answerNo {
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
	a.savePending = false
	return a
}

// SaveResultMsg reports what the save-as prompt's write did. The write runs in
// a [tea.Cmd] - disk I/O never blocks Update (issue #225) - and the prompt
// stays pending until this message says what happened.
type SaveResultMsg struct {
	// Name is the name the run was saved under.
	Name string
	// Err is why it was not, or nil. [sessions.ErrExists] means the overwrite
	// question must be asked.
	Err error
}

// commitSave writes the run as a group. An existing name is not replaced until
// the user has said so: the first attempt asks, the second overwrites.
func (a App) commitSave(overwrite bool) (App, tea.Cmd) {
	name := strings.TrimSpace(a.saveInput.Value())
	if name == "" || a.cfg.Sessions == nil {
		return a.cancelSave(), nil
	}
	if err := sessions.ValidateName(name); err != nil {
		// A bad name is known without the disk; report it now rather than
		// after the round trip through the Cmd.
		a.saveErr = err
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

	// The prompt keeps the keyboard while the write is in flight - Saving()
	// covers savePending - so a second enter cannot start a second write and
	// the typed name survives a failure.
	a.savePending = true
	a.saveInput.Blur()
	store := a.cfg.Sessions
	return a, func() tea.Msg {
		_, err := store.SaveRun(run, overwrite)
		return SaveResultMsg{Name: name, Err: err}
	}
}

// applySaveResult lands the save-as write's outcome: success closes the prompt
// and re-reads the group directory, a taken name opens the overwrite question,
// and any other failure reports in the panel with the prompt closed.
func (a App) applySaveResult(msg SaveResultMsg) (App, tea.Cmd) {
	a.savePending = false
	if msg.Err == nil {
		a = a.cancelSave()
		a.cfg.SessionName = msg.Name
		return a, func() tea.Msg { return SessionsChangedMsg{} }
	}
	if errors.Is(msg.Err, sessions.ErrExists) {
		a.confirmOverwrite = true
		a.saveInput.Blur()
		return a, nil
	}
	a.saveErr = msg.Err
	return a.cancelSave(), nil
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
// The end question this panel asks floats over the frame rather than taking
// its first line; see modal.go. focused reports whether this panel is the one
// that would actually receive a keystroke right now (issue #222).
func (a App) sessionsPanel(width, height int, focused bool) string {
	var b strings.Builder

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
		b.WriteString(a.openSessionLine(a.open[i], i == cursor, i == a.active, focused))
	}
	if hidden := len(a.open) - last; hidden > 0 {
		b.WriteString("\n")
		b.WriteString(a.theme.Muted.Render(fmt.Sprintf("+%d more", hidden)))
	}

	return a.theme.Base.Width(max(0, width)).Render(b.String())
}

// openSessionLine renders one open session. The foreground one is marked with
// a character as well as a style, so it survives a terminal without colour.
// focused decides whether the cursor row gets the strong highlight or the
// muted one; see [Theme.ListCursor].
func (a App) openSessionLine(s openSession, underCursor, foreground, focused bool) string {
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
		return a.theme.ListCursor(focused).Render(label)
	case foreground:
		return a.theme.Selected.Render(label)
	default:
		return a.theme.Base.Render(label)
	}
}
