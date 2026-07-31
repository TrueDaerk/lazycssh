package ui

import (
	"io"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/term"
)

// PaneWriter is the one way the interface writes to a single host: the typing
// path of a focused pane. It is the narrowest possible slice of [ssh.Manager],
// declared here so typing can be tested against a fake and so this package
// still cannot dial.
type PaneWriter interface {
	// Writer returns where the host's raw bytes go, or false when the
	// session cannot take input.
	Writer(id string) (io.Writer, bool)
	// SendKey delivers one key press through the host's terminal emulator,
	// which encodes it the way that host's current modes demand (issue #206).
	// It reports false when the session cannot take input.
	SendKey(id string, k term.KeyEvent) bool
}

// handleTypingKey is the focused pane behaving like a terminal: every key
// press is encoded and written to that one host, immediately, enter not
// required — ctrl+c, tab and esc included, because a shell that cannot see
// them is not a shell.
//
// lazycssh keeps only two kinds of keys for itself while typing: the one
// reserved escape, ctrl+], and the pane-management chords. Plain alt+arrows
// are not among them (issue #202): on macOS they are the shell's word
// navigation, so pane movement takes shift as well and the bare chords are
// forwarded by [keystrokeBytes].
func (a App) handleTypingKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.LeaveTyping) {
		return a.leaveTyping(), nil
	}
	if next, cmd, handled := a.handlePaneKey(msg); handled {
		return next, cmd
	}

	id := a.FocusedHost()
	if q := a.authFor(id); q != nil {
		// The pane's open auth question takes the typing (issue #182): the
		// keystrokes edit its answer, not a shell that is not there yet.
		return a.handleAuthKey(id, msg)
	}

	if id == "" {
		return a, nil
	}
	if a.cfg.Panes == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}

	// Typed keys are never recorded: this is where passwords are typed, and
	// the audit trail is for commands — see wiki/core/command-log.md. The
	// events go through the host's own emulator, so what reaches the shell is
	// what that host's terminal modes ask for (issue #206).
	for _, ev := range paneKeyEvents(msg) {
		if !a.cfg.Panes.SendKey(id, ev) {
			// A dead pane swallowing keystrokes silently would read as a hung
			// host; saying so is the difference.
			a.lastDelivery = id + " is not connected — alt+r reconnects, " + escapeKeystroke + " leaves"
			return a, nil
		}
	}
	return a, nil
}

// leaveTyping hands the keyboard back to the app: focus returns to the Status
// panel, which answers the question a user leaving a terminal has - where do
// my keys go now.
func (a App) leaveTyping() App {
	a.focus = AreaSidebar
	a.panel = PanelStatus
	return a
}

// handlePaneKey is pane management: switching, paging, full screen, scrolling,
// search, close and reconnect. The chords work the same while typing and from
// the app level, so managing panes never requires leaving either.
func (a App) handlePaneKey(msg tea.KeyPressMsg) (App, tea.Cmd, bool) {
	switch {
	// Moving the pane focus is entering it: an alt+arrow from anywhere lands
	// in that pane's terminal, which is the fastest route into typing.
	case key.Matches(msg, a.keys.PaneLeft):
		return a.enterPane().movePane(-1).followFocus(), nil, true
	case key.Matches(msg, a.keys.PaneRight):
		return a.enterPane().movePane(+1).followFocus(), nil, true
	case key.Matches(msg, a.keys.PaneUp):
		return a.enterPane().movePane(-a.grid().Columns).followFocus(), nil, true
	case key.Matches(msg, a.keys.PaneDown):
		return a.enterPane().movePane(+a.grid().Columns).followFocus(), nil, true
	case key.Matches(msg, a.keys.ToggleSelect):
		// The selection lives in the router, keyed by host identifier, so it
		// survives a reconnect and a page turn.
		if id := a.FocusedHost(); id != "" && a.cfg.Targets != nil {
			a.cfg.Targets.Toggle(id)
		}
		return a, nil, true
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

	case key.Matches(msg, a.keys.CopyPane):
		next, cmd := a.copyVisible()
		return next, cmd, true
	case key.Matches(msg, a.keys.CopyBuffer):
		next, cmd := a.copyScrollback()
		return next, cmd, true

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

// enterPane puts the keyboard into the focused pane's terminal.
func (a App) enterPane() App {
	if a.FocusedHost() != "" {
		a.focus = AreaGrid
	}
	return a
}
