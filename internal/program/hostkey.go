package program

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// The host key question's transport (issue #173). A dialling session that
// meets an unknown key blocks inside [keyPrompter.ConfirmHostKey]; the
// question travels over a channel into the event loop, the UI renders it in
// that session's pane and answers with [ui.HostKeyAnswerMsg], and the answer
// releases the session's goroutine. Every dialling session may have its own
// question open at once (issue #182): the pump re-arms on receipt, and the
// open questions are held by session id.

// keyQuestion is one blocked session's question.
type keyQuestion struct {
	sessionID   string
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
func (p *keyPrompter) ConfirmHostKey(ctx context.Context, sessionID string, host hosts.Host, keyType, fingerprint string) (bool, error) {
	q := &keyQuestion{sessionID: sessionID, host: host, keyType: keyType, fingerprint: fingerprint, answer: make(chan bool, 1)}
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
// events. It is armed at Init and re-armed the moment a question arrives, so
// every dialling session's question reaches the UI without waiting for the
// previous answer.
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

// askHostKey shows the question in the UI, remembers whose it is, and re-arms
// the pump so the next session's question is not held back. The question is
// also written into the session's scrollback in ssh's own wording, so the pane
// reads like a plain terminal (issue #180).
func (m *Model) askHostKey(msg keyQuestionMsg) (tea.Model, tea.Cmd) {
	if m.pendingKeys == nil {
		m.pendingKeys = make(map[string]*keyQuestion)
	}
	m.pendingKeys[msg.q.sessionID] = msg.q
	echoPrompt(m.promptTerminal(msg.q.sessionID), hostKeyPromptText(msg.q))
	return m, tea.Batch(m.forward(ui.HostKeyQuestionMsg{
		SessionID:   msg.q.sessionID,
		Host:        msg.q.host.Alias,
		KeyType:     msg.q.keyType,
		Fingerprint: msg.q.fingerprint,
	}), m.promptPump())
}

// answerHostKey releases the blocked session the answer names.
func (m *Model) answerHostKey(msg ui.HostKeyAnswerMsg) (tea.Model, tea.Cmd) {
	q, ok := m.pendingKeys[msg.SessionID]
	if !ok {
		return m, nil
	}
	shown := "no"
	if msg.Accept {
		shown = "yes"
	}
	echoAnswer(m.promptTerminal(q.sessionID), shown)
	q.answer <- msg.Accept
	delete(m.pendingKeys, msg.SessionID)
	return m, nil
}
