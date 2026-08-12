package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// handleBroadcastKey is the broadcast bar behaving like a terminal for the
// whole target set: every keystroke is encoded and fanned out live to the
// current broadcast targets — ctrl+c interrupts all of them, tab completes on
// all of them, and each pane shows its own host's echo.
//
// The bar keeps the reserved escape and the pane-management chords for itself,
// exactly like a focused pane, plus the csshx-style ctrl+a prefix: ctrl+a esc
// switches the bar to view mode, where keys are commands rather than
// keystrokes, ctrl+a ctrl+a and ctrl+a a send the literal ctrl+a a remote
// screen or tmux needs, ctrl+a and a command key runs that command (issue
// #289), and anything else after the prefix is forwarded to the targets
// unchanged.
func (a App) handleBroadcastKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	// A held paste owns the keyboard until it is answered: enter and esc are
	// the only keys that mean anything, so a stray keystroke aimed at the
	// hosts cannot silently release or drop a fleet-wide paste (issue #248).
	if a.pendingPaste != nil {
		return a.resolvePendingPaste(msg)
	}
	if key.Matches(msg, a.keys.LeaveTyping) {
		a.focus = AreaSidebar
		a.broadcastView, a.prefixArmed = false, false
		return a, nil
	}
	if a.broadcastView {
		return a.handleBroadcastViewKey(msg)
	}
	if a.prefixArmed {
		return a.resolveBroadcastEscape(msg)
	}
	if key.Matches(msg, a.keys.Prefix) {
		// The prefix shadows the ctrl+a the bar used to forward; the literal
		// is still reachable as ctrl+a a.
		a.prefixArmed = true
		return a, nil
	}
	if next, cmd, handled := a.handlePaneKey(msg); handled {
		return next, cmd
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
	// whole command is what the audit trail is for. Each key goes out as an
	// event and every host's emulator encodes it for that host (issue #206).
	delivery, err := a.sendBroadcastKey(msg)
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
			a.cfg.Recorder.Record(line, delivery.Mode, delivery.To)
		}
		a.broadcastLine = nil
	}
	return a, tea.Batch(authCmds...)
}

// pendingPaste is a multiline paste large enough to reach more than one host,
// held until the user reviews it: a pasted N-line script fanned out to M
// hosts unreviewed is the sharpest edge broadcasting has (issue #248). A
// paste that is one line, or whose targets are one host, carries no more risk
// than the keystrokes it replaces and is never held.
type pendingPaste struct {
	// content is the paste, verbatim, to send on release.
	content string
	// lines is how many lines it contains, for the notice.
	lines int
	// hosts is the resolved target count at the moment of the paste.
	hosts int
}

// handlePaste is where a bracketed paste lands. Only the broadcast bar
// thinks about holding it: a paste anywhere else in the app reaches at most
// one host, which is no different from typing it.
func (a App) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if a.focus != AreaBroadcast || a.broadcastView {
		return a, nil
	}
	// A paste mid ctrl+a-prefix has nothing to do with the prefix: it starts
	// a fresh decision rather than being read as the prefix's second key.
	a.prefixArmed = false

	lines := pasteLineCount(msg.Content)
	hosts := 0
	if a.cfg.Targets != nil {
		hosts = a.cfg.Targets.Count()
	}
	if lines > 1 && hosts > 1 {
		a.pendingPaste = &pendingPaste{content: msg.Content, lines: lines, hosts: hosts}
		return a, nil
	}
	return a.sendBroadcastRaw([]byte(msg.Content))
}

// resolvePendingPaste answers a held paste: enter sends it verbatim, esc
// drops it, everything else is ignored.
func (a App) resolvePendingPaste(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	p := a.pendingPaste
	switch {
	case key.Matches(msg, a.keys.PromptSubmit):
		a.pendingPaste = nil
		return a.sendBroadcastRaw([]byte(p.content))
	case key.Matches(msg, a.keys.PromptCancel):
		a.pendingPaste = nil
		a.lastDelivery = fmt.Sprintf("paste discarded: %d line%s to %d host%s",
			p.lines, plural(p.lines), p.hosts, plural(p.hosts))
		return a, nil
	}
	return a, nil
}

// pasteLineCount counts a paste's lines, ignoring one trailing newline: a
// two-line snippet copied with a trailing newline is still two lines to a
// user deciding whether to release it.
func pasteLineCount(content string) int {
	trimmed := strings.TrimSuffix(content, "\n")
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, "\n") + 1
}

// handleBroadcastViewKey is the bar in view mode: every key is an app-level
// command and none reaches a host. enter is the one key the mode keeps for
// itself — it returns to edit mode. Every other key is dispatched to the app
// keymap, so the global commands stay reachable from inside the bar.
func (a App) handleBroadcastViewKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, a.keys.BroadcastEdit) {
		a.broadcastView = false
		return a, nil
	}
	return a.handleAppKey(msg)
}

// resolveBroadcastEscape is the key after ctrl+a in the broadcast bar. The
// prefix is the lazycssh command prefix here (issue #289): while the bar has
// the keyboard every plain key belongs to the hosts, and the chord is the one
// way to run a command without leaving the line.
//
//   - ctrl+a — one literal ctrl+a, the GNU-screen double press; ctrl+a ctrl+a c
//     opens a screen window on every host.
//   - a — the same literal, matching screen's own ctrl+a a. Neither this nor
//     the double press can be rebound: a remote multiplexer, and readline's
//     beginning-of-line, must stay reachable whatever the keymap says.
//   - →/← — page to the next/previous screenful, the portable alternative to
//     ctrl+shift+arrows that macOS Terminal.app never sends (issue #273).
//   - esc — switch the bar to view mode, where every key is a command until
//     enter returns to typing.
//   - any app-level command — ctrl+a r re-tiles, ctrl+a b switches the
//     broadcast scope, and so on for the whole global set; a plain letter
//     stands for its ctrl chord, so the commands a host would otherwise eat
//     are reachable too. See [App.chordCommandKey].
//   - everything else still reaches the targets as the keystroke it is, which
//     is what issue #214 asked of the prefix and what keeps a stray chord from
//     being swallowed.
//
// The prefix is cleared before the second key is handled, so it cannot chain.
// Forwarded keys take the same send path as plain typing but stay out of the
// assembled line: a prefixed key is a control sequence, not command text.
func (a App) resolveBroadcastEscape(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	a.prefixArmed = false
	if next, cmd, handled := a.resolvePrefixPaging(msg); handled {
		return next, cmd
	}
	switch {
	case key.Matches(msg, a.keys.PrefixCancel):
		a.broadcastView = true
		return a, nil
	case key.Matches(msg, a.keys.PrefixLiteral):
		return a.sendBroadcastRaw([]byte{literalPrefixByte})
	}
	if command := a.chordCommandKey(msg); a.keys.isCommand(command) {
		return a.handleAppKey(command)
	}
	return a.forwardBroadcastKey(msg)
}

// forwardBroadcastKey sends one keystroke to the targets without touching the
// assembled line — the prefix passthrough. Plain typing goes through
// [App.handleBroadcastKey] instead, which also tracks the line and feeds
// pending auth prompts.
func (a App) forwardBroadcastKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if a.cfg.Sender == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}
	delivery, err := a.sendBroadcastKey(msg)
	switch {
	case err != nil:
		a.lastDelivery = delivery.String() + ": " + err.Error()
	case delivery.Delivered == 0:
		a.lastDelivery = delivery.String() + " — no host can take input right now"
	}
	return a, nil
}

// sendBroadcastKey fans one keystroke out as events, so every host's emulator
// encodes it for that host (issue #206). The last delivery wins: a key that
// expands to several events reaches the same target set for all of them.
func (a App) sendBroadcastKey(msg tea.KeyPressMsg) (broadcast.Delivery, error) {
	var delivery broadcast.Delivery
	var err error
	for _, ev := range paneKeyEvents(msg) {
		delivery, err = a.cfg.Sender.SendKey(ev)
		if err != nil {
			break
		}
	}
	return delivery, err
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
	case focused && a.pendingPaste != nil:
		p := a.pendingPaste
		line = a.theme.StatusWarning.Render(fmt.Sprintf(
			"paste: %d line%s → %d host%s  [enter send / esc cancel]",
			p.lines, plural(p.lines), p.hosts, plural(p.hosts)))
	case focused && a.broadcastView:
		// No cursor: nothing typed here is going anywhere.
		line = a.theme.Muted.Render("view mode — keys are commands · enter returns to typing")
	case focused && a.prefixArmed:
		line = "▏" + a.theme.Muted.Render(" "+a.prefixKey()+"…")
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
