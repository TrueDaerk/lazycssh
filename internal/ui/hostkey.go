package ui

import (
	tea "charm.land/bubbletea/v2"
)

// The host key question (issue #173): a host offering a key that is not in
// known_hosts blocks its own dial while the user is asked - per host, one
// question at a time. The UI cannot verify or write anything; it renders the
// question the program relays and answers with a message. A *changed* key is
// never a question - see ssh.HostKeyChangedError - only a first meeting is.

// HostKeyQuestionMsg asks the user about an unknown host key. The program
// emits it when a dialling session meets a key that known_hosts does not
// have; the session stays blocked until [HostKeyAnswerMsg] comes back.
type HostKeyQuestionMsg struct {
	// Host is the alias the user dialled, the name they know the machine by.
	Host string
	// KeyType is the offered key's algorithm, e.g. ecdsa-sha2-nistp256.
	KeyType string
	// Fingerprint is the SHA256 fingerprint ssh itself prints.
	Fingerprint string
}

// HostKeyAnswerMsg carries the user's answer back to the program: accept
// remembers the key in known_hosts and continues the handshake, reject fails
// that pane.
type HostKeyAnswerMsg struct {
	// Host names the question being answered.
	Host string
	// Accept is the user's decision.
	Accept bool
}

// HostKeyQuestionPending is the host whose key question is open, empty when
// none is.
func (a App) HostKeyQuestionPending() string {
	if a.keyQuestion == nil {
		return ""
	}
	return a.keyQuestion.Host
}

// showHostKeyQuestion opens the question. The Status panel is where it
// renders, so the panel is selected for the user - a security question must
// not be answerable without being seeable.
func (a App) showHostKeyQuestion(msg HostKeyQuestionMsg) App {
	q := msg
	a.keyQuestion = &q
	a.panel = PanelStatus
	return a
}

// handleHostKeyQuestionKey answers the question: y accepts and remembers the
// key, n or esc rejects it and fails the pane. Everything else is swallowed -
// a keystroke meant for a host must not accidentally answer for one.
func (a App) handleHostKeyQuestionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	q := *a.keyQuestion
	switch msg.String() {
	case "y", "Y":
		a.keyQuestion = nil
		return a, func() tea.Msg { return HostKeyAnswerMsg{Host: q.Host, Accept: true} }
	case "n", "N", "esc":
		a.keyQuestion = nil
		return a, func() tea.Msg { return HostKeyAnswerMsg{Host: q.Host, Accept: false} }
	}
	return a, nil
}

// hostKeyQuestionLines renders the open question for the Status panel. The
// fingerprint is the substance of the decision, so it gets its own line and is
// never truncated into prettiness.
func (a App) hostKeyQuestionLines() []string {
	if a.keyQuestion == nil {
		return nil
	}
	q := a.keyQuestion
	return []string{
		a.theme.StatusWarning.Render("unknown " + q.KeyType + " key for " + q.Host),
		a.theme.Base.Render(q.Fingerprint),
		a.theme.Muted.Render("y accepts and remembers it · n rejects and fails the pane"),
	}
}
