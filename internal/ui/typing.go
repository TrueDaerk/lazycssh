package ui

import (
	"io"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// PaneWriter is the one way the interface writes to a single host: the typing
// path of a focused pane. It is the narrowest possible slice of [ssh.Manager],
// declared here so typing can be tested against a fake and so this package
// still cannot dial.
type PaneWriter interface {
	// Writer returns where the host's keystrokes go, or false when the
	// session cannot take input.
	Writer(id string) (io.Writer, bool)
}

// handleTypingKey is the focused pane behaving like a terminal: every key
// press is encoded and written to that one host, immediately, enter not
// required — ctrl+c, tab and esc included, because a shell that cannot see
// them is not a shell.
//
// lazycssh keeps only two kinds of keys for itself while typing: the one
// reserved escape, ctrl+], and the alt/shift pane-management chords —
// combinations [keystrokeBytes] never encoded, so intercepting them forwards
// nothing a user could previously send.
func (a App) handleTypingKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.LeaveTyping) {
		return a.leaveTyping(), nil
	}
	if next, cmd, handled := a.handlePaneKey(msg); handled {
		return next, cmd
	}

	raw := keystrokeBytes(msg)
	if len(raw) == 0 {
		return a, nil
	}

	id := a.FocusedHost()
	if id == "" {
		return a, nil
	}
	if a.cfg.Panes == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}
	w, ok := a.cfg.Panes.Writer(id)
	if !ok {
		// A dead pane swallowing keystrokes silently would read as a hung
		// host; saying so is the difference.
		a.lastDelivery = id + " is not connected — alt+r reconnects, " + escapeKeystroke + " leaves"
		return a, nil
	}

	// Typed keys are never recorded: this is where passwords are typed, and
	// the audit trail is for commands — see wiki/core/command-log.md.
	if _, err := w.Write(raw); err != nil {
		a.lastDelivery = id + ": " + err.Error()
	}
	return a, nil
}

// leaveTyping hands the keyboard back to the app: focus returns to the Hosts
// panel with the cursor on the host that was just typed to, which is the host
// any follow-up command is about.
func (a App) leaveTyping() App {
	focused := a.FocusedHost()
	a.focus = AreaSidebar
	a.panel = PanelHosts
	for i, row := range a.hostRows() {
		if !row.Candidate && row.ID == focused {
			a.hostCursor = i
			break
		}
	}
	return a
}

// handlePaneKey is pane management: switching, paging, full screen, scrolling,
// search, close and reconnect. The chords work the same while typing and from
// the app level, so managing panes never requires leaving either.
func (a App) handlePaneKey(msg tea.KeyPressMsg) (App, tea.Cmd, bool) {
	switch {
	case key.Matches(msg, a.keys.PaneLeft):
		return a.movePane(-1).followFocus(), nil, true
	case key.Matches(msg, a.keys.PaneRight):
		return a.movePane(+1).followFocus(), nil, true
	case key.Matches(msg, a.keys.PaneUp):
		return a.movePane(-a.grid().Columns).followFocus(), nil, true
	case key.Matches(msg, a.keys.PaneDown):
		return a.movePane(+a.grid().Columns).followFocus(), nil, true
	case key.Matches(msg, a.keys.NextPage):
		return a.pageBy(+1), nil, true
	case key.Matches(msg, a.keys.PrevPage):
		return a.pageBy(-1), nil, true
	case key.Matches(msg, a.keys.FullScreen):
		a.fullScreen = !a.fullScreen
		return a, nil, true
	case key.Matches(msg, a.keys.Reconnect):
		if id := a.FocusedHost(); id != "" {
			return a, func() tea.Msg { return ReconnectHostMsg{ID: id} }, true
		}
		return a, nil, true
	case key.Matches(msg, a.keys.ClosePane):
		if id := a.FocusedHost(); id != "" {
			return a, a.closeOrRemove(id), true
		}
		return a, nil, true

	case key.Matches(msg, a.keys.ScrollUp):
		return a.scrollBy(+a.scrollPage()), nil, true
	case key.Matches(msg, a.keys.ScrollDown):
		return a.scrollBy(-a.scrollPage()), nil, true
	case key.Matches(msg, a.keys.ScrollTop):
		return a.scrollToTop(), nil, true
	case key.Matches(msg, a.keys.ScrollBottom):
		return a.scrollToBottom(), nil, true
	case key.Matches(msg, a.keys.SearchPane):
		return a.openSearch(), nil, true
	case key.Matches(msg, a.keys.NextMatch):
		return a.stepMatch(-1), nil, true
	case key.Matches(msg, a.keys.PrevMatch):
		return a.stepMatch(+1), nil, true
	case key.Matches(msg, a.keys.ClearSearch):
		return a.clearSearch(), nil, true
	}
	return a, nil, false
}
