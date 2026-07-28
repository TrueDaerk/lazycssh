package ui

import (
	"errors"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
)

// SessionStore is what the Sessions panel needs from the on-disk session store.
// It is the subset of [sessions.Store] the interface uses, so the panel can be
// tested against a fake and cannot reach past it.
type SessionStore interface {
	// List returns the saved session names in lexical order.
	List() ([]string, error)
	// Load reads one session.
	Load(name string) (*sessions.Session, error)
	// Exists reports whether a name is taken.
	Exists(name string) bool
	// SaveRun writes the live run as a named session. overwrite false returns
	// [sessions.ErrExists] rather than replacing.
	SaveRun(run sessions.Run, overwrite bool) (*sessions.Session, error)
}

// SessionsChangedMsg asks the panel to re-read the session directory. It is sent
// after a save and can be sent by anything that changes the directory.
type SessionsChangedMsg struct{}

// SessionLaunchMsg asks the program to open a saved session. The UI does not
// dial: it says what the user chose, and the layer that owns the transport acts
// on it.
type SessionLaunchMsg struct {
	// Name is the session to open.
	Name string
	// Merge asks for the session's hosts to be added to the current run rather
	// than replacing it.
	Merge bool
}

// sessionRow is one line of the Sessions panel.
type sessionRow struct {
	// Name is the session name.
	Name string
	// Hosts is how many hosts it opens, or -1 when the file could not be read.
	Hosts int
	// Description is the session's free-text note.
	Description string
	// Err is why the row could not be read, if it could not.
	Err error
}

// Label renders the row.
func (r sessionRow) Label() string {
	if r.Err != nil {
		return fmt.Sprintf("%s (unreadable)", r.Name)
	}
	label := fmt.Sprintf("%s (%d host%s)", r.Name, r.Hosts, plural(r.Hosts))
	if r.Description != "" {
		label += " — " + r.Description
	}
	return label
}

// loadSessions re-reads the session directory. One unreadable file becomes one
// unreadable row rather than an empty panel: the other sessions are still
// usable, and hiding them would make a typo look like data loss.
func (a App) loadSessions() App {
	a.sessionRows = nil
	a.sessionsErr = nil

	if a.cfg.Sessions == nil {
		return a
	}

	names, err := a.cfg.Sessions.List()
	if err != nil {
		a.sessionsErr = err
		return a
	}

	for _, name := range names {
		sess, err := a.cfg.Sessions.Load(name)
		if err != nil {
			a.sessionRows = append(a.sessionRows, sessionRow{Name: name, Hosts: -1, Err: err})
			continue
		}
		count, err := sess.HostCount()
		if err != nil {
			a.sessionRows = append(a.sessionRows, sessionRow{Name: name, Hosts: -1, Err: err})
			continue
		}
		a.sessionRows = append(a.sessionRows, sessionRow{
			Name: name, Hosts: count, Description: sess.Description,
		})
	}
	a.sessionCursor = clamp(a.sessionCursor, 0, max(0, len(a.sessionRows)-1))
	return a
}

// SessionCursor is the position of the cursor in the Sessions panel.
func (a App) SessionCursor() int { return a.sessionCursor }

// SelectedSession is the session under the cursor, or the empty string when none
// is saved.
func (a App) SelectedSession() string {
	if len(a.sessionRows) == 0 {
		return ""
	}
	return a.sessionRows[clamp(a.sessionCursor, 0, len(a.sessionRows)-1)].Name
}

// Saving reports whether the save-as prompt has the keyboard.
func (a App) Saving() bool { return a.saveInput.Focused() || a.confirmOverwrite }

// SaveError is the last save failure, or nil.
func (a App) SaveError() error { return a.saveErr }

// moveSessionCursor moves the cursor, stopping at the ends.
func (a App) moveSessionCursor(delta int) App {
	a.sessionCursor = clamp(a.sessionCursor+delta, 0, max(0, len(a.sessionRows)-1))
	return a
}

// launchSelectedSession asks the program to open the session under the cursor.
func (a App) launchSelectedSession(merge bool) (App, tea.Cmd) {
	name := a.SelectedSession()
	if name == "" {
		return a, nil
	}
	return a, func() tea.Msg { return SessionLaunchMsg{Name: name, Merge: merge} }
}

// beginSave opens the save-as prompt, pre-filled with the session the run was
// started from, because "save it again under the same name" is the common case.
func (a App) beginSave() App {
	a.saveErr = nil
	a.saveInput.SetValue(a.cfg.SessionName)
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

// commitSave writes the run. An existing name is not replaced until the user has
// said so: the first attempt asks, the second overwrites.
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
		run.Patterns = a.hostIDs()
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

// sessionsPanel renders the saved sessions, the save prompt and the overwrite
// question.
func (a App) sessionsPanel(width, height int) string {
	var b strings.Builder

	switch {
	case a.confirmOverwrite:
		b.WriteString(a.theme.StatusWarning.Render(
			fmt.Sprintf("overwrite %q? y/n", strings.TrimSpace(a.saveInput.Value()))))
		b.WriteString("\n")
	case a.saveInput.Focused():
		b.WriteString(a.theme.Base.Render("save as: " + a.saveInput.Value()))
		b.WriteString("\n")
	case a.saveErr != nil:
		b.WriteString(a.theme.Failure.Render(a.saveErr.Error()))
		b.WriteString("\n")
	}

	switch {
	case a.cfg.Sessions == nil:
		b.WriteString(a.theme.Muted.Render("no session directory"))
	case a.sessionsErr != nil:
		b.WriteString(a.theme.Failure.Render(a.sessionsErr.Error()))
	case len(a.sessionRows) == 0:
		b.WriteString(a.theme.Muted.Render("no saved sessions"))
	default:
		cursor := clamp(a.sessionCursor, 0, len(a.sessionRows)-1)
		first, last := visibleRange(cursor, len(a.sessionRows), max(1, height-1))
		for i := first; i < last; i++ {
			if i > first {
				b.WriteString("\n")
			}
			b.WriteString(a.sessionLine(a.sessionRows[i], i == cursor))
		}
		if hidden := len(a.sessionRows) - last; hidden > 0 {
			b.WriteString("\n")
			b.WriteString(a.theme.Muted.Render(fmt.Sprintf("+%d more", hidden)))
		}
	}

	return a.theme.Base.Width(max(0, width)).Render(b.String())
}

// sessionLine renders one saved session.
func (a App) sessionLine(row sessionRow, underCursor bool) string {
	switch {
	case underCursor:
		return a.theme.Cursor.Render(row.Label())
	case row.Err != nil:
		return a.theme.Failure.Render(row.Label())
	default:
		return a.theme.Base.Render(row.Label())
	}
}
