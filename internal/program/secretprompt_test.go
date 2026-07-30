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
		v, err := p.Password(context.Background(), hosts.Host{Alias: "db1", User: "test"})
		got <- result{v, err}
	}()

	var q *secretQuestion
	select {
	case q = <-p.questions:
	case <-time.After(time.Second):
		t.Fatal("no question arrived")
	}
	if q.prompt != "password for test@db1" || q.echo {
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
		_, err := p.Passphrase(context.Background(), hosts.Host{Alias: "db1"}, "/home/u/.ssh/id_ed25519")
		got <- err
	}()

	q := <-p.questions
	if q.prompt != "passphrase for /home/u/.ssh/id_ed25519" {
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

	if _, err := p.ask(ctx, "x", "password for x", false); err == nil {
		t.Fatal("ask returned no error under a cancelled context")
	}
}

// The event-loop wiring: the question becomes the UI message, the UI's answer
// releases the session and re-arms the pump.
func TestSecretQuestionReachesTheUIAndBack(t *testing.T) {
	m := &Model{
		app:     ui.NewApp(ui.Config{}),
		secrets: &secretPrompter{questions: make(chan *secretQuestion)},
	}

	q := &secretQuestion{prompt: "password for test@db1", answer: make(chan secretAnswer, 1)}
	model, _ := m.Update(secretQuestionMsg{q: q})
	m = model.(*Model)
	if !m.app.SecretPromptOpen() {
		t.Fatal("the prompt did not open in the UI")
	}

	model, cmd := m.Update(ui.SecretAnswerMsg{Value: "hunt", Ok: true})
	m = model.(*Model)
	select {
	case a := <-q.answer:
		if !a.ok || a.value != "hunt" {
			t.Fatalf("answer = %+v", a)
		}
	default:
		t.Fatal("the answer never reached the session")
	}
	if m.pendingSecret != nil {
		t.Fatal("pendingSecret survived the answer")
	}
	if cmd == nil {
		t.Fatal("the answer did not re-arm the secret pump")
	}
}

// An answer without a question is dropped, not a panic.
func TestStraySecretAnswerIsDropped(t *testing.T) {
	m := &Model{app: ui.NewApp(ui.Config{})}
	if _, cmd := m.Update(ui.SecretAnswerMsg{Value: "x", Ok: true}); cmd != nil {
		t.Fatal("a stray answer produced a command")
	}
}
