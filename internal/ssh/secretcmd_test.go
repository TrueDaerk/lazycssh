package ssh

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/secret"
)

const commandSecret = "hunter2-from-the-password-manager"

// printer builds a secret command that prints exactly what it is given.
func printer(text string) secret.Command {
	return secret.Command{Argv: []string{"printf", "%s", text}}
}

func TestCommandPrompterRunsTheConfiguredCommand(t *testing.T) {
	p := CommandPrompter{
		PasswordCommand:   func(hosts.Host) secret.Command { return printer(commandSecret) },
		PassphraseCommand: func(string) secret.Command { return printer("key-" + commandSecret) },
	}

	got, err := p.Password(context.Background(), hosts.Host{Alias: "web-01"})
	if err != nil {
		t.Fatalf("Password: %v", err)
	}
	if got != commandSecret {
		t.Fatalf("Password() = %q", got)
	}

	got, err = p.Passphrase(context.Background(), "~/.ssh/id_prod")
	if err != nil {
		t.Fatalf("Passphrase: %v", err)
	}
	if got != "key-"+commandSecret {
		t.Fatalf("Passphrase() = %q", got)
	}
}

func TestCommandPrompterFallsBackWhenNoCommandIsConfigured(t *testing.T) {
	fallback := FuncPrompter{
		PasswordFunc:   func(context.Context, hosts.Host) (string, error) { return "typed", nil },
		PassphraseFunc: func(context.Context, string) (string, error) { return "typed-passphrase", nil },
		QuestionFunc: func(_ context.Context, _ hosts.Host, q string, _ bool) (string, error) {
			return "answer to " + q, nil
		},
	}
	p := CommandPrompter{Fallback: fallback}

	if got, err := p.Password(context.Background(), hosts.Host{Alias: "web-01"}); err != nil || got != "typed" {
		t.Fatalf("Password() = %q, %v", got, err)
	}
	if got, err := p.Passphrase(context.Background(), "key"); err != nil || got != "typed-passphrase" {
		t.Fatalf("Passphrase() = %q, %v", got, err)
	}
	if got, err := p.Question(context.Background(), hosts.Host{}, "otp?", true); err != nil || got != "answer to otp?" {
		t.Fatalf("Question() = %q, %v", got, err)
	}
}

func TestCommandPrompterWithoutFallbackOrCommand(t *testing.T) {
	p := CommandPrompter{}
	for _, call := range []func() (string, error){
		func() (string, error) { return p.Password(context.Background(), hosts.Host{Alias: "web-01"}) },
		func() (string, error) { return p.Passphrase(context.Background(), "key") },
		func() (string, error) { return p.Question(context.Background(), hosts.Host{}, "q", false) },
	} {
		if _, err := call(); !errors.Is(err, ErrNoPrompter) {
			t.Fatalf("error = %v, want ErrNoPrompter", err)
		}
	}
}

// A failing secret command fails that host's authentication; it never quietly
// turns into a prompt the user was not expecting.
func TestCommandPrompterDoesNotFallBackWhenTheCommandFails(t *testing.T) {
	asked := false
	p := CommandPrompter{
		PasswordCommand: func(hosts.Host) secret.Command {
			return secret.Command{Argv: []string{"sh", "-c", "echo nope >&2; exit 1"}}
		},
		Fallback: FuncPrompter{PasswordFunc: func(context.Context, hosts.Host) (string, error) {
			asked = true
			return "typed", nil
		}},
	}

	if _, err := p.Password(context.Background(), hosts.Host{Alias: "web-01"}); err == nil {
		t.Fatal("Password succeeded despite a failing secret command")
	}
	if asked {
		t.Fatal("a failing secret command fell through to the interactive prompter")
	}
}

// A hung password manager fails one host's authentication, it does not pin the
// session forever.
func TestCommandPrompterTimesOut(t *testing.T) {
	p := CommandPrompter{
		PasswordCommand: func(hosts.Host) secret.Command {
			return secret.Command{Argv: []string{"sleep", "30"}, Timeout: 100 * time.Millisecond}
		},
	}

	start := time.Now()
	_, err := p.Password(context.Background(), hosts.Host{Alias: "web-01"})
	if err == nil {
		t.Fatal("Password succeeded for a command that never finishes")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Password took %s; the command was not killed", elapsed)
	}
	if !strings.Contains(err.Error(), "web-01") {
		t.Fatalf("error %q does not name the host", err)
	}
}

// The error path must not carry the credential the command printed before it
// failed.
func TestCommandPrompterErrorNeverQuotesTheSecret(t *testing.T) {
	p := CommandPrompter{
		PasswordCommand: func(hosts.Host) secret.Command {
			return secret.Command{Argv: []string{"sh", "-c", "printf '" + commandSecret + "'; exit 1"}}
		},
	}
	_, err := p.Password(context.Background(), hosts.Host{Alias: "web-01"})
	if err == nil {
		t.Fatal("Password succeeded for a failing command")
	}
	if strings.Contains(err.Error(), commandSecret) {
		t.Fatalf("the error leaked the credential: %v", err)
	}
}
