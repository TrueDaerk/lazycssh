package program

import (
	"context"
	"errors"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// The secret prompt's transport (issue #175), the same channel bridge the host
// key question uses: a dialling session that needs a password, a passphrase or
// a keyboard-interactive answer blocks in [secretPrompter], the question
// crosses into the event loop, the UI's masked input answers it. One question
// at a time; the transport's own caches (per user, per key file) keep the
// question count down across a fleet.

// errPromptCancelled is what an esc in the prompt turns into: the
// authentication attempt fails with a readable reason instead of hanging.
var errPromptCancelled = errors.New("cancelled at the prompt")

// secretQuestion is one blocked session's request for a secret.
type secretQuestion struct {
	// host is the alias of the host whose dial is blocked on the answer; the
	// UI uses it to render the prompt in that host's pane.
	host   string
	prompt string
	echo   bool
	// answer is buffered so the answering side never blocks, even if the
	// session gave up (context cancelled) meanwhile.
	answer chan secretAnswer
}

// secretAnswer is the user's response: a value, or a cancel.
type secretAnswer struct {
	value string
	ok    bool
}

// secretPrompter implements [ssh.Prompter] over a channel into the event loop.
type secretPrompter struct {
	questions chan *secretQuestion
}

// ask ships one question and waits for its answer under the session's context.
func (p *secretPrompter) ask(ctx context.Context, host, prompt string, echo bool) (string, error) {
	q := &secretQuestion{host: host, prompt: prompt, echo: echo, answer: make(chan secretAnswer, 1)}
	select {
	case p.questions <- q:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	select {
	case a := <-q.answer:
		if !a.ok {
			return "", errPromptCancelled
		}
		return a.value, nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (p *secretPrompter) Password(ctx context.Context, host hosts.Host) (string, error) {
	return p.ask(ctx, host.Alias, fmt.Sprintf("password for %s@%s", host.User, host.Alias), false)
}

func (p *secretPrompter) Passphrase(ctx context.Context, host hosts.Host, keyPath string) (string, error) {
	return p.ask(ctx, host.Alias, "passphrase for "+keyPath, false)
}

func (p *secretPrompter) Question(ctx context.Context, host hosts.Host, question string, echo bool) (string, error) {
	return p.ask(ctx, host.Alias, host.Alias+": "+question, echo)
}

// secretQuestionMsg delivers one question into the event loop.
type secretQuestionMsg struct {
	q *secretQuestion
}

// secretPump waits for the next secret question; re-armed only by the answer,
// which is what serializes the prompts.
func (m *Model) secretPump() tea.Cmd {
	if m.secrets == nil {
		return nil
	}
	questions := m.secrets.questions
	return func() tea.Msg {
		q, ok := <-questions
		if !ok {
			return nil
		}
		return secretQuestionMsg{q: q}
	}
}

// askSecret shows the prompt in the UI and remembers whose it is.
func (m *Model) askSecret(msg secretQuestionMsg) (tea.Model, tea.Cmd) {
	m.pendingSecret = msg.q
	return m, m.forward(ui.SecretQuestionMsg{Host: msg.q.host, Prompt: msg.q.prompt, Echo: msg.q.echo})
}

// answerSecret releases the blocked session and re-arms the pump.
func (m *Model) answerSecret(msg ui.SecretAnswerMsg) (tea.Model, tea.Cmd) {
	if m.pendingSecret == nil {
		return m, nil
	}
	m.pendingSecret.answer <- secretAnswer{value: msg.Value, ok: msg.Ok}
	m.pendingSecret = nil
	return m, m.secretPump()
}
