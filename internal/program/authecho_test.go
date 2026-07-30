package program

import (
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

func echoModel(t *testing.T, aliases ...string) *Model {
	t.Helper()
	targets := make([]hosts.Host, len(aliases))
	for i, alias := range aliases {
		targets[i] = hosts.Host{Alias: alias, Addr: "10.0.0.1"}
	}
	mgr, err := ssh.NewManager(ssh.ManagerConfig{
		Hosts:      targets,
		NewSession: func(r ssh.SessionRequest) ssh.Session { return ssh.NewFake(r.ID, r.Host, r.Events) },
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return &Model{app: ui.NewApp(ui.Config{}), mgr: mgr}
}

func scrollbackOf(t *testing.T, m *Model, id string) string {
	t.Helper()
	s, ok := m.mgr.Session(id)
	if !ok {
		t.Fatalf("no session %q", id)
	}
	return s.Scrollback().String()
}

// The host key question and its answer read like ssh in the pane's scrollback.
func TestHostKeyQuestionEchoesLikeATerminal(t *testing.T) {
	m := echoModel(t, "db1")
	q := &keyQuestion{
		sessionID: "db1",
		host:      hosts.Host{Alias: "db1", Addr: "10.0.0.1"}, keyType: "ssh-ed25519",
		fingerprint: "SHA256:x", answer: make(chan bool, 1),
	}
	m.askHostKey(keyQuestionMsg{q: q})

	got := scrollbackOf(t, m, "db1")
	for _, want := range []string{
		"The authenticity of host 'db1 (10.0.0.1)' can't be established.",
		"ssh-ed25519 key fingerprint is SHA256:x.",
		"Are you sure you want to continue connecting (yes/no)? ",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("scrollback is missing %q:\n%s", want, got)
		}
	}

	m.answerHostKey(ui.HostKeyAnswerMsg{SessionID: "db1", Accept: true})
	if got := scrollbackOf(t, m, "db1"); !strings.Contains(got, "(yes/no)? yes") {
		t.Fatalf("the answer is not echoed:\n%s", got)
	}
}

// A masked secret writes its prompt and, after the answer, only a line break -
// the typed value must never reach the scrollback. An echoing answer is shown.
func TestSecretPromptEchoesLikeATerminal(t *testing.T) {
	m := echoModel(t, "db1")
	q := &secretQuestion{sessionID: "db1", host: "db1", prompt: "test@db1's password: ", answer: make(chan secretAnswer, 1)}
	m.askSecret(secretQuestionMsg{q: q})

	if got := scrollbackOf(t, m, "db1"); !strings.Contains(got, "test@db1's password: ") {
		t.Fatalf("the prompt is not in the scrollback:\n%s", got)
	}
	m.answerSecret(ui.SecretAnswerMsg{SessionID: "db1", Value: "hunt", Ok: true})
	if got := scrollbackOf(t, m, "db1"); strings.Contains(got, "hunt") {
		t.Fatalf("the masked secret reached the scrollback:\n%s", got)
	}

	m = echoModel(t, "db1")
	q = &secretQuestion{sessionID: "db1", host: "db1", prompt: "Verification code: ", echo: true, answer: make(chan secretAnswer, 1)}
	m.askSecret(secretQuestionMsg{q: q})
	m.answerSecret(ui.SecretAnswerMsg{SessionID: "db1", Value: "otp", Ok: true})
	if got := scrollbackOf(t, m, "db1"); !strings.Contains(got, "Verification code: otp") {
		t.Fatalf("the echoing answer is not shown:\n%s", got)
	}
}

// Two dials of the same alias are two sessions with two scrollbacks: each
// question echoes into exactly its own (issue #182), and a question no
// session matches writes nowhere instead of panicking.
func TestPromptScrollbackIsPerSession(t *testing.T) {
	m := echoModel(t, "db1", "db1")

	q := &secretQuestion{sessionID: "db1#2", host: "db1", prompt: "test@db1's password: ", answer: make(chan secretAnswer, 1)}
	m.askSecret(secretQuestionMsg{q: q})

	if got := scrollbackOf(t, m, "db1#2"); !strings.Contains(got, "password: ") {
		t.Fatalf("the prompt is missing from its own pane:\n%s", got)
	}
	if got := scrollbackOf(t, m, "db1"); strings.Contains(got, "password") {
		t.Fatalf("the prompt leaked into the first alias pane:\n%s", got)
	}
	if buf := m.promptScrollback("gone"); buf != nil {
		t.Fatal("an unknown session id found a scrollback")
	}
	if buf := m.promptScrollback(""); buf != nil {
		t.Fatal("an empty session id found a scrollback")
	}
	echoPrompt(nil, "x") // must not panic
	echoAnswer(nil, "x")
}
