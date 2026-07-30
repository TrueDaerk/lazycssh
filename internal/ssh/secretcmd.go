package ssh

import (
	"context"
	"fmt"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/secret"
)

// CommandPrompter answers credential prompts by running a user-configured
// command - `pass show`, `op read`, `security find-generic-password` - instead
// of asking the user, and falls back to another prompter when no command is
// configured for what is being asked.
//
// It is how a session config can say *how* to authenticate without storing the
// secret: the config holds an argv, the password manager holds the password.
//
// A command that fails, times out or prints nothing fails that host's
// authentication with an error. It never falls through to the interactive
// prompter: a user who configured a secret command and is suddenly asked to
// type a password has been told nothing about why, and forty hosts would ask
// forty times.
type CommandPrompter struct {
	// PasswordCommand returns the command that prints the login password for a
	// host. A zero command means none is configured.
	PasswordCommand func(host hosts.Host) secret.Command
	// PassphraseCommand returns the command that prints the passphrase of a
	// private key file. A zero command means none is configured.
	PassphraseCommand func(keyPath string) secret.Command
	// Fallback handles what no command covers. Nil means those prompts fail
	// with [ErrNoPrompter].
	Fallback Prompter
}

// Password runs the configured command, or falls back.
func (p CommandPrompter) Password(ctx context.Context, host hosts.Host) (string, error) {
	cmd := secret.Command{}
	if p.PasswordCommand != nil {
		cmd = p.PasswordCommand(host)
	}
	if cmd.Empty() {
		if p.Fallback == nil {
			return "", fmt.Errorf("password for %s: %w", host.Alias, ErrNoPrompter)
		}
		return p.Fallback.Password(ctx, host)
	}

	value, err := cmd.Run(ctx)
	if err != nil {
		return "", fmt.Errorf("password for %s: %w", host.Alias, err)
	}
	defer value.Wipe()
	return value.Reveal(), nil
}

// Passphrase runs the configured command, or falls back.
func (p CommandPrompter) Passphrase(ctx context.Context, host hosts.Host, keyPath string) (string, error) {
	cmd := secret.Command{}
	if p.PassphraseCommand != nil {
		cmd = p.PassphraseCommand(keyPath)
	}
	if cmd.Empty() {
		if p.Fallback == nil {
			return "", fmt.Errorf("passphrase for %s: %w", keyPath, ErrNoPrompter)
		}
		return p.Fallback.Passphrase(ctx, host, keyPath)
	}

	value, err := cmd.Run(ctx)
	if err != nil {
		return "", fmt.Errorf("passphrase for %s: %w", keyPath, err)
	}
	defer value.Wipe()
	return value.Reveal(), nil
}

// Question always falls back: a keyboard-interactive challenge is a
// conversation, and answering an unknown question with a stored password would
// hand it to whatever the server decided to ask.
func (p CommandPrompter) Question(ctx context.Context, host hosts.Host, question string, echo bool) (string, error) {
	if p.Fallback == nil {
		return "", fmt.Errorf("keyboard-interactive for %s: %w", host.Alias, ErrNoPrompter)
	}
	return p.Fallback.Question(ctx, host, question, echo)
}
