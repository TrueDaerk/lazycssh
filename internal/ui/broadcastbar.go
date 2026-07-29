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
// The bar keeps only the reserved escape and the pane-management chords for
// itself, exactly like a focused pane.
func (a App) handleBroadcastKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.LeaveTyping) {
		a.focus = AreaSidebar
		return a, nil
	}
	if next, cmd, handled := a.handlePaneKey(msg); handled {
		return next, cmd
	}

	raw := keystrokeBytes(msg)
	if len(raw) == 0 {
		return a, nil
	}

	a = a.echoBroadcastKey(msg)

	if a.cfg.Sender == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}
	// Individual keystrokes are never recorded — this is where a password may
	// be typed. The assembled line is recorded on enter instead, because a
	// whole command is what the audit trail is for.
	delivery, err := a.cfg.Sender.Send(raw)
	if err != nil {
		a.lastDelivery = delivery.String() + ": " + err.Error()
	}

	if msg.Code == tea.KeyEnter && msg.Mod == 0 {
		line := strings.TrimSpace(string(a.broadcastLine))
		if line != "" && a.cfg.Recorder != nil {
			a.cfg.Recorder.Record(line, delivery.Mode, delivery.Delivered)
		}
		a.broadcastLine = nil
	}
	return a, nil
}

// echoBroadcastKey keeps the bar's local echo line in step with what was
// typed: printable text appends, backspace trims, enter clears (after the
// caller records the line). Control keys change nothing here — their effect is
// on the hosts, and the panes show it.
func (a App) echoBroadcastKey(msg tea.KeyPressMsg) App {
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

// BroadcastLine is the bar's local echo of what was typed since the last
// enter.
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
	title := fmt.Sprintf("Broadcast [6] → %d host%s", count, plural(count))
	if warning {
		title += " ⚠"
	}

	line := string(a.broadcastLine)
	switch {
	case focused:
		line += "▏"
	case line == "":
		line = a.theme.Muted.Render("press 6 and type — every key goes to the targets live")
	}

	if r.Height == 1 {
		// Too tight for a box: a bare line that still carries the count.
		label := a.theme.PanelTitle(focused).Render(title) + " " + line
		return a.theme.Base.MaxWidth(r.Width).MaxHeight(1).Render(label)
	}
	return titledBox(a.theme, focused, r.Width, r.Height, title, line)
}
