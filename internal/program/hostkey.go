package program

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// The host key question's transport (issue #173). A dialling session that
// meets an unknown key blocks inside [keyPrompter.ConfirmHostKey]; the
// question travels over a channel into the event loop, the UI renders it and
// answers with [ui.HostKeyAnswerMsg], and the answer releases the session's
// goroutine. One question is in flight at a time - the channel is unbuffered
// and the pump is re-armed only after the answer - so twenty new hosts ask
// twenty times, in order, never as a pile of dialogs.

// keyQuestion is one blocked session's question.
type keyQuestion struct {
	host        hosts.Host
	keyType     string
	fingerprint string
	// answer releases the session goroutine. Buffered so the answering side
	// never blocks, even if the session gave up (context cancelled) meanwhile.
	answer chan bool
}

// keyPrompter implements [ssh.HostKeyPrompter] over a channel into the event
// loop.
type keyPrompter struct {
	questions chan *keyQuestion
}

// ConfirmHostKey ships the question to the UI and waits for the answer. The
// context is the session's own: a run shutting down, or that session being
// closed, withdraws the question instead of leaking a goroutine.
func (p *keyPrompter) ConfirmHostKey(ctx context.Context, host hosts.Host, keyType, fingerprint string) (bool, error) {
	q := &keyQuestion{host: host, keyType: keyType, fingerprint: fingerprint, answer: make(chan bool, 1)}
	select {
	case p.questions <- q:
	case <-ctx.Done():
		return false, ctx.Err()
	}
	select {
	case accept := <-q.answer:
		return accept, nil
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// keyQuestionMsg delivers one question into the event loop.
type keyQuestionMsg struct {
	q *keyQuestion
}

// promptPump waits for the next host key question, like pump does for fleet
// events. It is armed at Init and re-armed by the answer, never earlier - the
// serialization lives here.
func (m *Model) promptPump() tea.Cmd {
	if m.prompter == nil {
		return nil
	}
	questions := m.prompter.questions
	return func() tea.Msg {
		q, ok := <-questions
		if !ok {
			return nil
		}
		return keyQuestionMsg{q: q}
	}
}

// askHostKey shows the question in the UI and remembers whose it is. The
// question is also written into the host's scrollback in ssh's own wording, so
// the pane reads like a plain terminal (issue #180).
func (m *Model) askHostKey(msg keyQuestionMsg) (tea.Model, tea.Cmd) {
	m.pendingKey = msg.q
	echoPrompt(m.promptScrollback(msg.q.host.Alias), hostKeyPromptText(msg.q))
	return m, m.forward(ui.HostKeyQuestionMsg{
		Host:        msg.q.host.Alias,
		KeyType:     msg.q.keyType,
		Fingerprint: msg.q.fingerprint,
	})
}

// answerHostKey releases the blocked session and re-arms the pump for the
// next question.
func (m *Model) answerHostKey(msg ui.HostKeyAnswerMsg) (tea.Model, tea.Cmd) {
	if m.pendingKey == nil {
		return m, nil
	}
	shown := "no"
	if msg.Accept {
		shown = "yes"
	}
	echoAnswer(m.promptScrollback(m.pendingKey.host.Alias), shown)
	m.pendingKey.answer <- msg.Accept
	m.pendingKey = nil
	return m, m.promptPump()
}
