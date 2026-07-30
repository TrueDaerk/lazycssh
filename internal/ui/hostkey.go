package ui

// The host key question (issue #173): a host offering a key that is not in
// known_hosts blocks its own dial while the user is asked. The UI cannot
// verify or write anything; it renders the question the program relays -
// ssh's own wording, in the session's scrollback - and answers with a
// message. A *changed* key is never a question - see ssh.HostKeyChangedError -
// only a first meeting is. The typed-answer plumbing is shared with the
// secret prompt; see internal/ui/authpane.go.

// HostKeyQuestionMsg asks the user about an unknown host key. The program
// emits it when a dialling session meets a key that known_hosts does not
// have; the session stays blocked until [HostKeyAnswerMsg] comes back.
type HostKeyQuestionMsg struct {
	// SessionID names the pane whose dial is blocked - exact, because ten
	// dials of the same alias are ten panes (issue #182).
	SessionID string
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
	// SessionID names the question being answered.
	SessionID string
	// Accept is the user's decision.
	Accept bool
}

// showHostKeyQuestion opens the question in its pane. The prompt text is in
// the session's scrollback already; the pane takes typed input for it from
// here on.
func (a App) showHostKeyQuestion(msg HostKeyQuestionMsg) App {
	q := msg
	return a.addAuth(msg.SessionID, paneAuth{hostKey: &q})
}
