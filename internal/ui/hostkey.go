package ui

import (
	"strings"

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

// showHostKeyQuestion opens the question in the host's own pane, which is
// focused for the user - a security question must not be answerable without
// being seeable, and the pane is where the host it concerns is. A host without
// a visible pane is asked about in the Status panel instead.
func (a App) showHostKeyQuestion(msg HostKeyQuestionMsg) App {
	q := msg
	a.keyQuestion = &q
	a.keyAnswer = nil
	return a.focusQuestionPane(msg.Host)
}

// handleHostKeyQuestionKey answers the question the way ssh does (issue #180):
// the answer is typed - yes/y accepts and remembers the key, no/n rejects and
// fails the pane - and enter sends it. Anything else typed and entered clears
// and asks again, so a keystroke meant for a host cannot accidentally answer.
// esc rejects, the way interrupting ssh at the question does.
func (a App) handleHostKeyQuestionKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	q := *a.keyQuestion
	switch msg.String() {
	case "enter":
		answer := strings.ToLower(strings.TrimSpace(string(a.keyAnswer)))
		a.keyAnswer = nil
		switch answer {
		case "yes", "y":
			a.keyQuestion = nil
			a.questionPaneID = ""
			return a, func() tea.Msg { return HostKeyAnswerMsg{Host: q.Host, Accept: true} }
		case "no", "n":
			a.keyQuestion = nil
			a.questionPaneID = ""
			return a, func() tea.Msg { return HostKeyAnswerMsg{Host: q.Host, Accept: false} }
		}
		return a, nil
	case "esc":
		a.keyQuestion = nil
		a.keyAnswer = nil
		a.questionPaneID = ""
		return a, func() tea.Msg { return HostKeyAnswerMsg{Host: q.Host, Accept: false} }
	case "backspace":
		if n := len(a.keyAnswer); n > 0 {
			a.keyAnswer = a.keyAnswer[:n-1]
		}
		return a, nil
	}
	if msg.Text != "" {
		a.keyAnswer = append(a.keyAnswer, []rune(msg.Text)...)
	}
	return a, nil
}

// hostKeyQuestionLines renders the open question for the Status panel - the
// fallback for a host without a visible pane; a visible pane shows ssh's own
// wording in its scrollback instead. The fingerprint is the substance of the
// decision, so it gets its own line and is never truncated into prettiness.
func (a App) hostKeyQuestionLines() []string {
	if a.keyQuestion == nil {
		return nil
	}
	q := a.keyQuestion
	return []string{
		a.theme.StatusWarning.Render("unknown " + q.KeyType + " key for " + q.Host),
		a.theme.Base.Render(q.Fingerprint),
		a.theme.Base.Render("continue connecting (yes/no)? " + string(a.keyAnswer)),
		a.theme.Muted.Render("yes accepts and remembers it · no rejects and fails the pane"),
	}
}
