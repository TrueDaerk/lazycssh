package program

import (
	"context"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// The prompter round trip: the question comes out of the channel, the answer
// releases the blocked caller.
func TestKeyPrompterRoundTrip(t *testing.T) {
	p := &keyPrompter{questions: make(chan *keyQuestion)}

	type result struct {
		accept bool
		err    error
	}
	got := make(chan result, 1)
	go func() {
		accept, err := p.ConfirmHostKey(context.Background(),
			hosts.Host{Alias: "web-01"}, "ssh-ed25519", "SHA256:abc")
		got <- result{accept, err}
	}()

	var q *keyQuestion
	select {
	case q = <-p.questions:
	case <-time.After(time.Second):
		t.Fatal("no question arrived")
	}
	if q.host.Alias != "web-01" || q.keyType != "ssh-ed25519" || q.fingerprint != "SHA256:abc" {
		t.Fatalf("question = %+v", q)
	}

	q.answer <- true
	select {
	case r := <-got:
		if r.err != nil || !r.accept {
			t.Fatalf("ConfirmHostKey = (%v, %v), want accepted", r.accept, r.err)
		}
	case <-time.After(time.Second):
		t.Fatal("the answer did not release the caller")
	}
}

// A cancelled session withdraws its question instead of hanging.
func TestKeyPrompterHonoursTheContext(t *testing.T) {
	p := &keyPrompter{questions: make(chan *keyQuestion)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := p.ConfirmHostKey(ctx, hosts.Host{Alias: "web-01"}, "ssh-ed25519", "SHA256:abc"); err == nil {
		t.Fatal("ConfirmHostKey returned no error under a cancelled context")
	}
}

// The event-loop wiring: a question becomes the UI message, the UI's answer
// releases the session and re-arms the pump for the next question.
func TestHostKeyQuestionReachesTheUIAndBack(t *testing.T) {
	m := &Model{
		app:      ui.NewApp(ui.Config{}),
		prompter: &keyPrompter{questions: make(chan *keyQuestion)},
	}

	q := &keyQuestion{host: hosts.Host{Alias: "web-01"}, keyType: "ssh-ed25519",
		fingerprint: "SHA256:abc", answer: make(chan bool, 1)}
	model, _ := m.Update(keyQuestionMsg{q: q})
	m = model.(*Model)
	if got := m.app.HostKeyQuestionPending(); got != "web-01" {
		t.Fatalf("HostKeyQuestionPending() = %q", got)
	}

	model, cmd := m.Update(ui.HostKeyAnswerMsg{Host: "web-01", Accept: true})
	m = model.(*Model)
	select {
	case accept := <-q.answer:
		if !accept {
			t.Fatal("the answer arrived as a rejection")
		}
	default:
		t.Fatal("the answer never reached the session")
	}
	if m.pendingKey != nil {
		t.Fatal("pendingKey survived the answer")
	}
	if cmd == nil {
		t.Fatal("the answer did not re-arm the prompt pump")
	}
}

// An answer without a question is dropped, not a panic: the session it
// belonged to may have given up meanwhile.
func TestStrayHostKeyAnswerIsDropped(t *testing.T) {
	m := &Model{app: ui.NewApp(ui.Config{})}
	if _, cmd := m.Update(ui.HostKeyAnswerMsg{Host: "web-01", Accept: true}); cmd != nil {
		t.Fatal("a stray answer produced a command")
	}
}
