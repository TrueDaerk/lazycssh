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
		accept, err := p.ConfirmHostKey(context.Background(), "web-01",
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

	if _, err := p.ConfirmHostKey(ctx, "web-01", hosts.Host{Alias: "web-01"}, "ssh-ed25519", "SHA256:abc"); err == nil {
		t.Fatal("ConfirmHostKey returned no error under a cancelled context")
	}
}

// The event-loop wiring: every question becomes a UI message the moment it
// arrives - two sessions may prompt at once (issue #182) - and each answer
// releases exactly the session it names.
func TestHostKeyQuestionsReachTheUIConcurrently(t *testing.T) {
	m := &Model{
		app:      ui.NewApp(ui.Config{}),
		prompter: &keyPrompter{questions: make(chan *keyQuestion)},
	}

	q1 := &keyQuestion{sessionID: "localhost", host: hosts.Host{Alias: "localhost"},
		keyType: "ssh-ed25519", fingerprint: "SHA256:abc", answer: make(chan bool, 1)}
	q2 := &keyQuestion{sessionID: "localhost#2", host: hosts.Host{Alias: "localhost"},
		keyType: "ssh-ed25519", fingerprint: "SHA256:def", answer: make(chan bool, 1)}
	model, cmd := m.Update(keyQuestionMsg{q: q1})
	m = model.(*Model)
	if cmd == nil {
		t.Fatal("the first question did not re-arm the pump")
	}
	model, _ = m.Update(keyQuestionMsg{q: q2})
	m = model.(*Model)
	if got := m.app.AuthPending(); got != 2 {
		t.Fatalf("AuthPending() = %d, want both questions open", got)
	}

	model, _ = m.Update(ui.HostKeyAnswerMsg{SessionID: "localhost#2", Accept: true})
	m = model.(*Model)
	select {
	case accept := <-q2.answer:
		if !accept {
			t.Fatal("the answer arrived as a rejection")
		}
	default:
		t.Fatal("the answer never reached its session")
	}
	select {
	case <-q1.answer:
		t.Fatal("the answer released the wrong session")
	default:
	}
	if _, still := m.pendingKeys["localhost"]; !still {
		t.Fatal("the unanswered question was dropped")
	}
	if _, gone := m.pendingKeys["localhost#2"]; gone {
		t.Fatal("the answered question survived")
	}
}

// An answer without a question is dropped, not a panic: the session it
// belonged to may have given up meanwhile.
func TestStrayHostKeyAnswerIsDropped(t *testing.T) {
	m := &Model{app: ui.NewApp(ui.Config{})}
	if _, cmd := m.Update(ui.HostKeyAnswerMsg{SessionID: "web-01", Accept: true}); cmd != nil {
		t.Fatal("a stray answer produced a command")
	}
}
