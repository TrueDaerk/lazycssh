package ui

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

// The secret prompt (issue #175): a dialling session that needs a password, a
// key passphrase or a keyboard-interactive answer blocks while the user types
// it into a masked input in the Status panel - one question at a time, the
// same channel bridge the host key question uses. The value goes back to the
// program and from there into the transport's in-memory cache; it is never
// rendered, logged or written to disk.

// SecretQuestionMsg asks the user for one secret. The program emits it when a
// dialling session needs a credential; the session stays blocked until
// [SecretAnswerMsg] comes back.
type SecretQuestionMsg struct {
	// Prompt says what is being asked for, e.g. "password for test@db1".
	Prompt string
	// Echo reports whether the answer may be shown while it is typed. It is
	// false for passwords and passphrases, and comes from the server for
	// keyboard-interactive questions.
	Echo bool
}

// SecretAnswerMsg carries the user's answer back to the program. A cancelled
// prompt (Ok false) fails that authentication attempt rather than hanging it.
type SecretAnswerMsg struct {
	// Value is the typed secret, empty when cancelled.
	Value string
	// Ok distinguishes an entered (possibly empty) secret from a cancel.
	Ok bool
}

// SecretPromptOpen reports whether the secret prompt has the keyboard.
func (a App) SecretPromptOpen() bool { return a.secretQuestion != nil }

// showSecretQuestion opens the prompt. The Status panel is where it renders,
// so the panel is selected for the user.
func (a App) showSecretQuestion(msg SecretQuestionMsg) App {
	q := msg
	a.secretQuestion = &q
	a.panel = PanelStatus
	a.secretInput.SetValue("")
	if msg.Echo {
		a.secretInput.EchoMode = textinput.EchoNormal
	} else {
		a.secretInput.EchoMode = textinput.EchoPassword
	}
	a.secretInput.Focus()
	return a
}

// closeSecretQuestion closes the prompt and wipes the typed value from the
// input's buffer.
func (a App) closeSecretQuestion() App {
	a.secretQuestion = nil
	a.secretInput.SetValue("")
	a.secretInput.Blur()
	return a
}

// handleSecretPromptKey drives the prompt: enter submits, esc cancels and
// fails that authentication attempt. Everything else edits the masked input.
func (a App) handleSecretPromptKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		value := a.secretInput.Value()
		a = a.closeSecretQuestion()
		return a, func() tea.Msg { return SecretAnswerMsg{Value: value, Ok: true} }
	case "esc":
		a = a.closeSecretQuestion()
		return a, func() tea.Msg { return SecretAnswerMsg{} }
	}

	var cmd tea.Cmd
	a.secretInput, cmd = a.secretInput.Update(msg)
	return a, cmd
}

// secretPromptLines renders the open prompt for the Status panel.
func (a App) secretPromptLines() []string {
	if a.secretQuestion == nil {
		return nil
	}
	return []string{
		a.theme.StatusWarning.Render(a.secretQuestion.Prompt),
		a.theme.Base.Render(a.secretInput.View()),
		a.theme.Muted.Render("enter sends it · esc cancels and fails the attempt"),
	}
}
