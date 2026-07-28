package secret

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// printer builds a command that prints exactly what it is given.
func printer(text string) Command {
	return Command{Argv: []string{"printf", "%s", text}}
}

func TestRunReadsStdout(t *testing.T) {
	v, err := printer(password).Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer v.Wipe()
	if v.Reveal() != password {
		t.Fatalf("Reveal() = %q", v.Reveal())
	}
}

// `pass show` prints a trailing newline; a password with a newline glued to it
// fails authentication in a way that is very hard to see.
func TestRunStripsOneTrailingNewline(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{"unix newline", password + "\n", password},
		{"windows newline", password + "\r\n", password},
		{"two newlines keeps one", password + "\n\n", password + "\n"},
		{"no newline", password, password},
		{"inner newline kept", "a\nb\n", "a\nb"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := printer(tc.out).Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			defer v.Wipe()
			if v.Reveal() != tc.want {
				t.Fatalf("Reveal() = %q, want %q", v.Reveal(), tc.want)
			}
		})
	}
}

// A broken secret command fails that host's authentication cleanly; it never
// hangs the session and never leaks what it printed.
func TestRunFailures(t *testing.T) {
	tests := []struct {
		name string
		cmd  Command
		want string
	}{
		{"exit status", Command{Argv: []string{"sh", "-c", "echo denied >&2; exit 3"}}, "denied"},
		{"no such program", Command{Argv: []string{"lazycssh-no-such-program"}}, "lazycssh-no-such-program"},
		{"empty output", Command{Argv: []string{"true"}}, "no output"},
		{"only a newline", printer("\n"), "no output"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.cmd.Run(context.Background())
			if err == nil {
				t.Fatalf("Run succeeded, returning %d bytes", v.Len())
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// The command's stdout is the credential, so it never becomes an error message
// even when the command then fails.
func TestRunErrorNeverQuotesStdout(t *testing.T) {
	cmd := Command{Argv: []string{"sh", "-c", "printf '" + password + "'; exit 1"}}
	_, err := cmd.Run(context.Background())
	if err == nil {
		t.Fatal("Run succeeded for a failing command")
	}
	if strings.Contains(err.Error(), password) {
		t.Fatalf("the error leaked the credential: %v", err)
	}
}

func TestRunTimesOut(t *testing.T) {
	cmd := Command{Argv: []string{"sleep", "30"}, Timeout: 100 * time.Millisecond}

	start := time.Now()
	_, err := cmd.Run(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Run returned no error for a command that never finishes")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("error = %v, want a timeout", err)
	}
	if elapsed > 5*time.Second {
		t.Fatalf("Run took %s; the command was not killed", elapsed)
	}
}

func TestRunHonoursACancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Command{Argv: []string{"sleep", "30"}}).Run(ctx); err == nil {
		t.Fatal("Run ignored a cancelled context")
	}
}

func TestRunCapsOutput(t *testing.T) {
	cmd := Command{Argv: []string{"sh", "-c", "head -c 200000 /dev/zero | tr '\\000' 'a'"}}
	v, err := cmd.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer v.Wipe()
	if v.Len() > maxOutput {
		t.Fatalf("Run kept %d bytes, want at most %d", v.Len(), maxOutput)
	}
}

// The argv is executed directly, so metacharacters in a password entry's name
// cannot turn into a command.
func TestRunDoesNotUseAShell(t *testing.T) {
	v, err := Command{Argv: []string{"printf", "%s", "a; rm -rf /"}}.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	defer v.Wipe()
	if v.Reveal() != "a; rm -rf /" {
		t.Fatalf("Reveal() = %q", v.Reveal())
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		cmd     Command
		wantErr bool
	}{
		{"empty command is allowed", Command{}, false},
		{"program only", Command{Argv: []string{"pass"}}, false},
		{"program and arguments", Command{Argv: []string{"pass", "show", "prod/deploy"}}, false},
		{"blank program", Command{Argv: []string{"  "}}, true},
		{"empty argument", Command{Argv: []string{"pass", ""}}, true},
		{"negative timeout", Command{Argv: []string{"pass"}, Timeout: -time.Second}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cmd.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestRunRejectsAnEmptyCommand(t *testing.T) {
	if _, err := (Command{}).Run(context.Background()); err == nil {
		t.Fatal("Run accepted a command with no program")
	}
	if _, err := (Command{Argv: []string{""}}).Run(context.Background()); err == nil {
		t.Fatal("Run accepted a blank program name")
	}
}

func TestEmptyAndString(t *testing.T) {
	if empty := (Command{}).Empty(); !empty {
		t.Fatal("Empty() = false for a zero command")
	}
	if got := (Command{}).String(); got != "" {
		t.Fatalf("String() = %q for a zero command", got)
	}
	cmd := Command{Argv: []string{"pass", "show", "prod/deploy"}}
	if cmd.Empty() {
		t.Fatal("Empty() = true for a configured command")
	}
	if got := cmd.String(); got != "pass show prod/deploy" {
		t.Fatalf("String() = %q", got)
	}
}

func TestErrNoOutputIsMatchable(t *testing.T) {
	_, err := Command{Argv: []string{"true"}}.Run(context.Background())
	if !errors.Is(err, ErrNoOutput) {
		t.Fatalf("error = %v, want ErrNoOutput", err)
	}
}
