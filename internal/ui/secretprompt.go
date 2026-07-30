package ui

// The secret prompt (issue #175): a dialling session that needs a password, a
// key passphrase or a keyboard-interactive answer blocks while the user types
// it into its own pane - the prompt text is in that session's scrollback, the
// answer is typed inline like a terminal takes it, and a masked value is
// never rendered, logged or written to disk. Every session may prompt at
// once (issue #182); the broadcast line answers all of them together. The
// typed-answer plumbing is shared with the host key question; see
// internal/ui/authpane.go.

// SecretQuestionMsg asks the user for one secret. The program emits it when a
// dialling session needs a credential; the session stays blocked until
// [SecretAnswerMsg] comes back.
type SecretQuestionMsg struct {
	// SessionID names the pane whose dial is blocked - exact, because ten
	// dials of the same alias are ten panes (issue #182).
	SessionID string
	// Host is the alias, for display.
	Host string
	// Prompt says what is being asked for, e.g. "test@db1's password: ".
	Prompt string
	// Echo reports whether the answer may be shown while it is typed. It is
	// false for passwords and passphrases, and comes from the server for
	// keyboard-interactive questions.
	Echo bool
}

// SecretAnswerMsg carries the user's answer back to the program. A cancelled
// prompt (Ok false) fails that authentication attempt rather than hanging it.
type SecretAnswerMsg struct {
	// SessionID names the question being answered.
	SessionID string
	// Value is the typed secret, empty when cancelled.
	Value string
	// Ok distinguishes an entered (possibly empty) secret from a cancel.
	Ok bool
}

// showSecretQuestion opens the prompt in its pane. The prompt text is in the
// session's scrollback already; the pane takes typed input for it from here
// on.
func (a App) showSecretQuestion(msg SecretQuestionMsg) App {
	q := msg
	return a.addAuth(msg.SessionID, paneAuth{secret: &q})
}
