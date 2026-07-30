package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// handleBroadcastKey is the broadcast bar behaving like a terminal for the
// whole target set: every keystroke is encoded and fanned out live to the
// current broadcast targets — ctrl+c interrupts all of them, tab completes on
// all of them, and each pane shows its own host's echo.
//
// The bar keeps the reserved escape and the pane-management chords for itself,
// exactly like a focused pane, plus the csshx-style ctrl+a prefix: ctrl+a esc
// switches the bar to view mode, where keys are commands rather than
// keystrokes, and ctrl+a a sends the literal ctrl+a a remote screen or tmux
// needs.
func (a App) handleBroadcastKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.LeaveTyping) {
		a.focus = AreaSidebar
		a.broadcastView, a.broadcastPending = false, false
		return a, nil
	}
	if a.broadcastView {
		return a.handleBroadcastViewKey(msg)
	}
	if a.broadcastPending {
		return a.resolveBroadcastEscape(msg)
	}
	if key.Matches(msg, a.keys.BroadcastEscape) {
		// The prefix shadows the ctrl+a the bar used to forward; the literal
		// is still reachable as ctrl+a a, and the global connected-only toggle
		// as ctrl+a esc ctrl+a.
		a.broadcastPending = true
		return a, nil
	}
	if next, cmd, handled := a.handlePaneKey(msg); handled {
		return next, cmd
	}

	raw := keystrokeBytes(msg)
	if len(raw) == 0 {
		return a, nil
	}

	a = a.trackBroadcastLine(msg)

	// Prompting targets take the keystroke as their answer (issue #182): the
	// broadcast line is how one typing action fills every pane's password
	// prompt at once.
	a, authCmds, fed := a.feedAuthBroadcast(msg)

	if a.cfg.Sender == nil {
		if fed == 0 {
			a.lastDelivery = "no transport: nothing was sent"
		}
		return a, tea.Batch(authCmds...)
	}
	// Individual keystrokes are never recorded — this is where a password may
	// be typed. The assembled line is recorded on enter instead, because a
	// whole command is what the audit trail is for.
	delivery, err := a.cfg.Sender.Send(raw)
	switch {
	case err != nil:
		a.lastDelivery = delivery.String() + ": " + err.Error()
	case delivery.Delivered == 0 && fed == 0:
		// The keystroke reached nobody and nothing errored: the scope is
		// empty or every host in it is down. Typing into the void must not
		// look like typing (issue #133).
		a.lastDelivery = delivery.String() + " — no host can take input right now"
	}

	if msg.Code == tea.KeyEnter && msg.Mod == 0 {
		line := strings.TrimSpace(string(a.broadcastLine))
		// A line no live host received was an answer to prompts, not a
		// command: it may be a password, and the audit trail must not hold it.
		if line != "" && a.cfg.Recorder != nil && delivery.Delivered > 0 {
			a.cfg.Recorder.Record(line, delivery.Mode, delivery.Delivered)
		}
		a.broadcastLine = nil
	}
	return a, tea.Batch(authCmds...)
}

// handleBroadcastViewKey is the bar in view mode: every key is an app-level
// command and none reaches a host. enter is the one key the mode keeps for
// itself — it returns to edit mode. ctrl+a is a command here too, which is how
// the global connected-only toggle stays reachable from inside the bar.
func (a App) handleBroadcastViewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.BroadcastEdit) {
		a.broadcastView = false
		return a, nil
	}
	return a.handleAppKey(msg)
}

// resolveBroadcastEscape is the key after ctrl+a: esc switches to view mode, a
// sends a literal ctrl+a to the targets, and **any other key is a one-shot
// lazycssh command** (issue #148) — dispatched to the app keymap exactly as if
// the bar did not have the keyboard, so ctrl+a ctrl+a toggles connected-only
// and ctrl+a ? opens the help without leaving the bar. The prefix is cleared
// before the second key is handled, so it cannot chain. A key with no app
// binding is a no-op that says so, because a silently swallowed keystroke
// reads as a hung bar; nothing after the prefix ever reaches the hosts except
// the literal `a`.
func (a App) resolveBroadcastEscape(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a.broadcastPending = false
	switch msg.String() {
	case "esc":
		a.broadcastView = true
		return a, nil
	case "a":
		return a.sendBroadcastRaw([]byte{0x01})
	}
	if !a.keys.matchesAppBinding(msg) {
		a.lastDelivery = "ctrl+a " + msg.String() + " has no lazycssh binding — nothing was sent"
		return a, nil
	}
	return a.handleAppKey(msg)
}

// sendBroadcastRaw fans raw bytes out to the targets — the same path a typed
// keystroke takes, without touching the assembled line.
func (a App) sendBroadcastRaw(raw []byte) (tea.Model, tea.Cmd) {
	if a.cfg.Sender == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}
	delivery, err := a.cfg.Sender.Send(raw)
	switch {
	case err != nil:
		a.lastDelivery = delivery.String() + ": " + err.Error()
	case delivery.Delivered == 0:
		a.lastDelivery = delivery.String() + " — no host can take input right now"
	}
	return a, nil
}

// trackBroadcastLine assembles the typed line for the command log: printable
// text appends, backspace trims, enter clears (after the caller records the
// line). The line is never displayed — the panes show each host's own echo —
// it only exists so the audit trail can record whole commands.
func (a App) trackBroadcastLine(msg tea.KeyPressMsg) App {
	switch {
	case msg.Code == tea.KeyBackspace && msg.Mod == 0:
		if len(a.broadcastLine) > 0 {
			a.broadcastLine = a.broadcastLine[:len(a.broadcastLine)-1]
		}
	case msg.Mod&^tea.ModShift == 0 && msg.Text != "":
		a.broadcastLine = append(a.broadcastLine, []rune(msg.Text)...)
	}
	return a
}

// BroadcastLine is the line assembled from what was typed since the last
// enter. It is recorded on enter, not rendered.
func (a App) BroadcastLine() string { return string(a.broadcastLine) }

// renderBroadcastBar draws the always-visible broadcast input. The target
// count lives in the title because it is the one number that must be read
// before pressing a key here.
func (a App) renderBroadcastBar() string {
	r := a.layout.Broadcast
	focused := a.focus == AreaBroadcast

	count := 0
	warning := false
	if a.cfg.Targets != nil {
		count = a.cfg.Targets.Count()
		warning = a.cfg.Targets.Warning()
	}
	title := fmt.Sprintf("Broadcast [5] → %d host%s", count, plural(count))
	if warning {
		title += " ⚠"
	}

	// Typed text is never mirrored here — the panes show each host's own
	// echo. The bar only says what state it is in.
	var line string
	switch {
	case focused && a.broadcastView:
		// No cursor: nothing typed here is going anywhere.
		line = a.theme.Muted.Render("view mode — keys are commands · enter returns to typing")
	case focused && a.broadcastPending:
		line = "▏" + a.theme.Muted.Render(" ctrl+a…")
	case focused:
		line = "▏" + a.theme.Muted.Render(" keys go to the targets live — the panes echo")
	default:
		line = a.theme.Muted.Render("press 5 and type — every key goes to the targets live")
	}

	if r.Height == 1 {
		// Too tight for a box: a bare line that still carries the count.
		label := a.theme.PanelTitle(focused).Render(title) + " " + line
		return a.theme.Base.MaxWidth(r.Width).MaxHeight(1).Render(label)
	}
	return titledBox(a.theme, focused, r.Width, r.Height, title, line)
}
