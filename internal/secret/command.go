package secret

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Defaults for a command that does not configure them.
const (
	// DefaultTimeout bounds how long a secret command may take. A password
	// manager that is waiting for a fingerprint gets a while; a command that
	// hangs must not take the host's connection attempt down with it.
	DefaultTimeout = 30 * time.Second
	// maxOutput caps how much is read from a secret command. A credential is
	// short; anything larger is a misconfiguration - the wrong command, or one
	// that dumps a file - and reading it all would only put more of it in
	// memory.
	maxOutput = 64 << 10
	// killDelay is how long a command gets to exit after its context is
	// cancelled before it is killed outright.
	killDelay = 2 * time.Second
)

// ErrNoOutput is returned when a secret command succeeds but prints nothing.
var ErrNoOutput = errors.New("secret command produced no output")

// Command runs an external program and reads a credential from its standard
// output, so a user can delegate to `pass`, `op`, `security find-generic-password`
// or anything else, and lazycssh never stores the secret itself.
//
// The program is executed directly, never through a shell: Argv is a program and
// its arguments, so a password entry containing shell metacharacters cannot turn
// into a command.
type Command struct {
	// Argv is the program and its arguments. Empty means no command.
	Argv []string
	// Dir is the working directory; empty means the process's own.
	Dir string
	// Timeout bounds the run; zero means [DefaultTimeout].
	Timeout time.Duration
}

// Empty reports whether no command is configured.
func (c Command) Empty() bool { return len(c.Argv) == 0 }

// String renders the command for the UI. It shows the program and its
// arguments, which are configuration rather than credential material - the
// secret is what comes back on stdout, and that never reaches here.
func (c Command) String() string {
	if c.Empty() {
		return ""
	}
	return strings.Join(c.Argv, " ")
}

// Validate rejects a command that cannot run.
func (c Command) Validate() error {
	if c.Empty() {
		return nil
	}
	if strings.TrimSpace(c.Argv[0]) == "" {
		return fmt.Errorf("secret command: empty program name")
	}
	for i, arg := range c.Argv {
		if arg == "" && i > 0 {
			return fmt.Errorf("secret command %q: argument %d is empty", c.Argv[0], i)
		}
	}
	if c.Timeout < 0 {
		return fmt.Errorf("secret command %q: negative timeout", c.Argv[0])
	}
	return nil
}

// Run executes the command and returns what it printed, with one trailing
// newline stripped - `pass show` prints one, and a password with a newline glued
// to it fails authentication in a way that is very hard to see.
//
// A failure is always a failure: the command is killed when the timeout expires
// rather than being left to hang, so a broken secret command fails that host's
// authentication cleanly instead of stalling its session forever.
//
// The command's standard error is used for the error message; its standard
// output never is, because that is the credential.
func (c Command) Run(ctx context.Context) (*Value, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	if c.Empty() {
		return nil, fmt.Errorf("secret command: no command configured")
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.Argv[0], c.Argv[1:]...)
	cmd.Dir = c.Dir
	cmd.Stdin = nil
	// A command that ignores its context still dies: after WaitDelay the
	// process is killed and Wait returns, so a session can never be pinned by
	// a hung password manager.
	cmd.WaitDelay = killDelay

	var stderr bytes.Buffer
	cmd.Stderr = &limitedWriter{w: &stderr, remaining: maxOutput}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("secret command %q: %w", c.Argv[0], err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("secret command %q: %w", c.Argv[0], err)
	}

	out, readErr := io.ReadAll(io.LimitReader(stdout, maxOutput))
	// Anything past the cap is drained rather than left in the pipe: a command
	// that keeps writing would block forever on a full pipe, and Wait with it.
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()

	switch {
	case ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded):
		wipe(out)
		return nil, fmt.Errorf("secret command %q: timed out after %s", c.Argv[0], timeout)
	case waitErr != nil:
		wipe(out)
		return nil, fmt.Errorf("secret command %q: %w%s", c.Argv[0], waitErr, stderrSuffix(stderr.String()))
	case readErr != nil:
		wipe(out)
		return nil, fmt.Errorf("secret command %q: read output: %w", c.Argv[0], readErr)
	}

	out = trimTrailingNewline(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("secret command %q: %w", c.Argv[0], ErrNoOutput)
	}
	return New(out), nil
}

// trimTrailingNewline removes one trailing "\n" or "\r\n" in place.
func trimTrailingNewline(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\n' {
		b = b[:n-1]
	}
	if n := len(b); n > 0 && b[n-1] == '\r' {
		b = b[:n-1]
	}
	return b
}

// wipe overwrites a buffer that turned out not to be returned to the caller.
func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// stderrSuffix appends the command's diagnostics to an error, trimmed and
// single-line, or nothing when it said nothing.
func stderrSuffix(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.ReplaceAll(s, "\n", "; ")
	return ": " + s
}

// limitedWriter caps how much of a command's diagnostics is kept.
type limitedWriter struct {
	w         io.Writer
	remaining int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	total := len(p)
	if l.remaining <= 0 {
		return total, nil
	}
	if len(p) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.w.Write(p)
	l.remaining -= n
	return total, err
}
