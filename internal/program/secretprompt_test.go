package program

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// The prompter round trip: the question comes out labelled, the answer
// releases the blocked caller with the typed value.
func TestSecretPrompterRoundTrip(t *testing.T) {
	p := &secretPrompter{questions: make(chan *secretQuestion)}

	type result struct {
		value string
		err   error
	}
	got := make(chan result, 1)
	go func() {
		v, err := p.Password(context.Background(), "db1", hosts.Host{Alias: "db1", User: "test"})
		got <- result{v, err}
	}()

	var q *secretQuestion
	select {
	case q = <-p.questions:
	case <-time.After(time.Second):
		t.Fatal("no question arrived")
	}
	if q.prompt != "test@db1's password: " || q.echo {
		t.Fatalf("question = %+v", q)
	}

	q.answer <- secretAnswer{value: "hunt", ok: true}
	select {
	case r := <-got:
		if r.err != nil || r.value != "hunt" {
			t.Fatalf("Password = (%q, %v)", r.value, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("the answer did not release the caller")
	}
}

// esc at the prompt fails the attempt with a readable reason.
func TestSecretPrompterCancel(t *testing.T) {
	p := &secretPrompter{questions: make(chan *secretQuestion)}

	got := make(chan error, 1)
	go func() {
		_, err := p.Passphrase(context.Background(), "db1", hosts.Host{Alias: "db1"}, "/home/u/.ssh/id_ed25519")
		got <- err
	}()

	q := <-p.questions
	if q.prompt != "Enter passphrase for key '/home/u/.ssh/id_ed25519': " {
		t.Fatalf("prompt = %q", q.prompt)
	}
	q.answer <- secretAnswer{}
	select {
	case err := <-got:
		if !errors.Is(err, errPromptCancelled) {
			t.Fatalf("err = %v, want errPromptCancelled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("the cancel did not release the caller")
	}
}

// A cancelled session withdraws its question instead of hanging.
func TestSecretPrompterHonoursTheContext(t *testing.T) {
	p := &secretPrompter{questions: make(chan *secretQuestion)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.ask(ctx, "x", "x", "password for x", false); err == nil {
		t.Fatal("ask returned no error under a cancelled context")
	}
}

// The event-loop wiring: every question becomes a UI message the moment it
// arrives, and each answer releases exactly the session it names (issue #182).
func TestSecretQuestionsReachTheUIConcurrently(t *testing.T) {
	m := &Model{
		app:     ui.NewApp(ui.Config{}),
		secrets: &secretPrompter{questions: make(chan *secretQuestion)},
	}

	q1 := &secretQuestion{sessionID: "localhost", prompt: "test@localhost's password: ", answer: make(chan secretAnswer, 1)}
	q2 := &secretQuestion{sessionID: "localhost#2", prompt: "test@localhost's password: ", answer: make(chan secretAnswer, 1)}
	model, cmd := m.Update(secretQuestionMsg{q: q1})
	m = model.(*Model)
	if cmd == nil {
		t.Fatal("the first question did not re-arm the pump")
	}
	model, _ = m.Update(secretQuestionMsg{q: q2})
	m = model.(*Model)
	if got := m.app.AuthPending(); got != 2 {
		t.Fatalf("AuthPending() = %d, want both prompts open", got)
	}

	model, _ = m.Update(ui.SecretAnswerMsg{SessionID: "localhost", Value: "hunt", Ok: true})
	m = model.(*Model)
	select {
	case a := <-q1.answer:
		if !a.ok || a.value != "hunt" {
			t.Fatalf("answer = %+v", a)
		}
	default:
		t.Fatal("the answer never reached its session")
	}
	select {
	case <-q2.answer:
		t.Fatal("the answer released the wrong session")
	default:
	}
	if _, still := m.pendingSecrets["localhost#2"]; !still {
		t.Fatal("the unanswered prompt was dropped")
	}
}

// An answer without a question is dropped, not a panic.
func TestStraySecretAnswerIsDropped(t *testing.T) {
	m := &Model{app: ui.NewApp(ui.Config{})}
	if _, cmd := m.Update(ui.SecretAnswerMsg{Value: "x", Ok: true}); cmd != nil {
		t.Fatal("a stray answer produced a command")
	}
}
