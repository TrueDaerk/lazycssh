package ui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// Exporting a pane's scrollback to a file — the postmortem path. alt+y and
// alt+d (copy.go) put text on the clipboard for as long as the next paste;
// this writes the whole retained ring to disk so it survives after the run
// ends, for the "ran it on forty, need the log from the one that broke"
// case (issue #252).
//
// This is deliberately not session logging (internal/sessionlog): that is
// opt-in at the run's start, continuous, and captures every host for the
// run's whole life. This is a one-shot, explicit export of the one pane the
// user is looking at right now — nothing is written until the key is
// pressed, and only that pane's content ever is.

// exportTimeFormat names the export file after the moment it was written,
// filesystem-safe on every platform: no colons.
const exportTimeFormat = "2006-01-02_15-04-05"

// PaneExportedMsg lands a file export's outcome. The write is disk I/O, so it
// runs in a Cmd (issue #225) and its result arrives as a message like every
// other write in this package.
type PaneExportedMsg struct {
	Host  string
	Path  string
	Lines int
	Err   error
}

// report renders the status line for an export outcome, successful or not.
func (m PaneExportedMsg) report() string {
	if m.Err != nil {
		return fmt.Sprintf("%s: export failed: %v", m.Host, m.Err)
	}
	return fmt.Sprintf("wrote %d line%s of %s's scrollback to %s",
		m.Lines, plural(m.Lines), m.Host, m.Path)
}

// exportFileName names an export after the host and the moment it was
// written: lazycssh-<alias>-<timestamp>.log.
func exportFileName(now time.Time, host string) string {
	return fmt.Sprintf("lazycssh-%s-%s.log", sanitizeFileToken(host), now.Format(exportTimeFormat))
}

// sanitizeFileToken turns a host id into a safe file name component, the
// same rule [sessionlog] applies to a log file name: path separators, colons
// (a host id can be user@host:port) and control bytes become "_", and a
// leading dot cannot hide the file.
func sanitizeFileToken(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r == '/' || r == '\\' || r == ':' || r < 0x20 || r == 0x7f:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" || s[0] == '.' {
		s = "_" + s
	}
	return s
}

// exportScrollback writes the focused pane's whole retained scrollback - the
// history that scrolled off screen plus the visible screen - to a file in the
// working directory, ANSI escapes stripped. Unlike the clipboard copies in
// copy.go this never blocks Update: the write happens inside the returned
// Cmd and its outcome comes back as a PaneExportedMsg (issue #225).
func (a App) exportScrollback() (App, tea.Cmd) {
	id := a.FocusedHost()
	if id == "" {
		return a, nil
	}
	t := a.paneTerminal(id)
	if t == nil {
		return a, nil
	}

	text := strings.TrimRight(ansi.Strip(t.Text()), "\n")
	if text == "" {
		a.lastDelivery = id + ": nothing to export"
		return a, nil
	}
	lines := strings.Count(text, "\n") + 1
	path := exportFileName(a.now(), id)

	return a, func() tea.Msg {
		err := os.WriteFile(path, []byte(text+"\n"), 0o600)
		return PaneExportedMsg{Host: id, Path: path, Lines: lines, Err: err}
	}
}
