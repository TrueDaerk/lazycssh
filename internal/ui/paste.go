package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Bracketed paste. A paste is input, so it goes where typing goes: the
// focused pane's host while a pane has the keyboard, the broadcast targets
// while the bar has it (issue #307). Nothing else takes a paste — the
// sidebar, the overlays and every text prompt ignore it, which is what they
// did before there was a pane path at all.

// pendingPaste is a multiline paste large enough to reach more than one host,
// held until the user reviews it: a pasted N-line script fanned out to M
// hosts unreviewed is the sharpest edge broadcasting has (issue #248). A
// paste that is one line, or whose targets are one host, carries no more risk
// than the keystrokes it replaces and is never held — which is why the pane
// path below never holds one: a focused pane is exactly one host.
type pendingPaste struct {
	// content is the paste, verbatim, to send on release.
	content string
	// lines is how many lines it contains, for the notice.
	lines int
	// hosts is the resolved target count at the moment of the paste.
	hosts int
}

// handlePaste is where a bracketed paste lands and is routed by focus.
//
// Only the broadcast bar thinks about holding it: a paste anywhere else
// reaches at most one host, which is no different from typing it. A prompt or
// an overlay owning the keyboard swallows the paste rather than letting it
// slip through to a host the user is not looking at.
func (a App) handlePaste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if a.promptOwnsKeyboard() || a.showHelp || a.showPreview {
		return a, nil
	}
	switch {
	case a.focus == AreaGrid:
		return a.pasteToPane(msg.Content)
	case a.focus == AreaBroadcast && !a.broadcastView:
		return a.pasteToBroadcast(msg.Content)
	}
	return a, nil
}

// pasteToPane sends a paste to the focused host, the way typing into that
// pane does: through the host's own emulator, so the text is wrapped in
// bracketed-paste markers whenever the remote app asked for them and a
// multi-line paste lands in the shell's line editor instead of executing line
// by line. One pane is one host, so nothing is held for review; the status
// line names the host that got it, which is the target count a single pane
// can have.
func (a App) pasteToPane(content string) (tea.Model, tea.Cmd) {
	// A paste mid ctrl+a-prefix has nothing to do with the prefix, exactly as
	// in the bar: it starts a fresh decision rather than being read as the
	// prefix's second key.
	a.prefixArmed = false
	if content == "" {
		return a, nil
	}
	id := a.FocusedHost()
	if id == "" {
		return a, nil
	}
	if q := a.authFor(id); q != nil {
		// The pane's open auth question takes the paste, the way it takes the
		// typing (issue #182): a passphrase is pasted far more often than it
		// is typed. Only the first line — the rest of a paste would be an
		// answer to a question that has not been asked yet.
		line, _, _ := strings.Cut(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
		q.answer = append(q.answer, []rune(strings.TrimSuffix(line, "\r"))...)
		return a, nil
	}
	if a.cfg.Panes == nil {
		a.lastDelivery = "no transport: nothing was sent"
		return a, nil
	}
	if !a.cfg.Panes.Paste(id, content) {
		// Same refusal typing reports: a dead pane swallowing a paste
		// silently would read as a hung host.
		a.lastDelivery = id + " is not connected — " + a.reconnectKey() + " reconnects, " + a.escapeKey() + " leaves"
		return a, nil
	}
	lines := pasteLineCount(content)
	a.lastDelivery = fmt.Sprintf("pasted %d line%s → %s", lines, plural(lines), id)
	return a, nil
}

// pasteToBroadcast is the bar's paste: multiline text reaching more than one
// host is held for review, everything else goes out at once.
func (a App) pasteToBroadcast(content string) (tea.Model, tea.Cmd) {
	// A paste mid ctrl+a-prefix has nothing to do with the prefix: it starts
	// a fresh decision rather than being read as the prefix's second key.
	a.prefixArmed = false

	lines := pasteLineCount(content)
	hosts := 0
	if a.cfg.Targets != nil {
		hosts = a.cfg.Targets.Count()
	}
	if lines > 1 && hosts > 1 {
		a.pendingPaste = &pendingPaste{content: content, lines: lines, hosts: hosts}
		return a, nil
	}
	return a.sendBroadcastRaw([]byte(content))
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
