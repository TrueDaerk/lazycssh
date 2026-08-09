// Command lazycssh opens SSH sessions to many hosts at once and broadcasts
// keyboard input to all of them.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/program"
	"github.com/TrueDaerk/lazycssh/internal/sessionlog"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/version"
)

// Exit codes. Anything the user can trigger by mistyping a flag is [exitUsage];
// [exitError] is reserved for failures during a run.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

const usage = `lazycssh - terminal UI for parallel SSH

Usage:
  lazycssh [flags] [<host|@session>...]

Hosts may use brace expansion, for example srv1-{01..40}.example.com.
An argument starting with @ names a saved session; extra hosts on the command
line are merged into it. Without arguments the interface opens on the saved
sessions, ready to launch one.

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, launchTUI))
}

// launchTUI builds the program and runs the bubbletea event loop until the
// user quits. It is passed into run rather than called there, so the flag and
// plan handling is testable without a terminal.
func launchTUI(cfg program.Config) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m, err := program.Build(ctx, cfg)
	if err != nil {
		return err
	}

	_, runErr := tea.NewProgram(m).Run()

	// Stop dialling before closing: a session that connects after CloseAll
	// would leak its goroutines.
	cancel()
	closeErr := m.Manager().CloseAll()
	m.Manager().Wait()

	if runErr != nil {
		return runErr
	}
	return closeErr
}

// run is the testable entry point: it takes the arguments without the program
// name, writes everything to the given writers, and returns the process exit
// code instead of calling os.Exit.
func run(args []string, stdout, stderr io.Writer, launch func(program.Config) error) int {
	fs := flag.NewFlagSet("lazycssh", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}

	showVersion := fs.Bool("version", false, "print the version and exit")
	insecure := fs.Bool("insecure-ignore-host-key", false,
		"accept any host key without checking known_hosts (dangerous)")
	listSessions := fs.Bool("list-sessions", false, "list the saved sessions and exit")
	sessionDir := fs.String("sessions-dir", "",
		"directory holding saved sessions (default $XDG_CONFIG_HOME/lazycssh/sessions)")
	logDir := fs.String("log-dir", "",
		"write every host's session output to files in a new run directory under DIR (off by default)")

	if err := fs.Parse(args); err != nil {
		// flag already reported the error and printed the usage.
		return exitUsage
	}

	if *showVersion {
		fmt.Fprintln(stdout, version.String())
		return exitOK
	}

	store, err := openStore(*sessionDir)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}

	if *listSessions {
		return printSessions(store, stdout, stderr)
	}

	// No host arguments is not an error: the TUI opens on the saved sessions,
	// the way lazygit opens on the repository it is standing in.
	resolved, err := resolvePlan(fs.Args(), store)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}

	if *insecure {
		// Weakening host key verification is announced, every run. Once the TUI
		// exists the status bar carries this for the whole session as well.
		fmt.Fprintln(stderr,
			"WARNING: --insecure-ignore-host-key is set. Host keys are not verified and "+
				"a machine-in-the-middle cannot be detected.")
	}

	// The status bar carries the name of the last session named, which is the
	// one whose settings won during plan resolution.
	sessionName := ""
	if names := resolved.SessionNames(); len(names) > 0 {
		sessionName = names[len(names)-1]
	}

	// Session logging is opt-in per run: no flag, no file. Opening the run
	// directory before the TUI starts means an unwritable path fails here,
	// readably, instead of losing output behind a running interface.
	var logs *sessionlog.Run
	if *logDir != "" {
		var err error
		logs, err = sessionlog.Open(*logDir, time.Now())
		if err != nil {
			fmt.Fprintln(stderr, err)
			return exitError
		}
	}

	if err := launch(program.Config{
		Patterns:    resolved.Patterns,
		SessionName: sessionName,
		Broadcast:   resolved.Broadcast,
		WorkingSet:  resolved.WorkingSet,
		Store:       store,
		Insecure:    *insecure,
		Logs:        logs,
	}); err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}

	if logs != nil {
		if err := logs.Close(); err != nil {
			// Logging was explicitly asked for; ending with an incomplete log
			// and saying nothing would be worse than a non-zero exit.
			fmt.Fprintln(stderr, err)
			return exitError
		}
		fmt.Fprintf(stdout, "session logs: %s\n", logs.Dir())
	}
	return exitOK
}

// openStore builds the session store, from an explicit directory or the default
// location.
func openStore(dir string) (*sessions.Store, error) {
	if dir != "" {
		return sessions.NewStore(dir)
	}
	return sessions.DefaultStore()
}

// printSessions lists the saved sessions with their host counts, which is what
// a user reaches for after mistyping a session name.
func printSessions(store *sessions.Store, stdout, stderr io.Writer) int {
	names, err := store.List()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitError
	}
	if len(names) == 0 {
		fmt.Fprintf(stderr, "no sessions are saved in %s\n", store.Dir())
		return exitOK
	}

	for _, name := range names {
		sess, err := store.Load(name)
		if err != nil {
			// One unreadable file must not hide the rest of the list.
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			continue
		}
		count, err := sess.HostCount()
		if err != nil {
			fmt.Fprintf(stderr, "%s: %v\n", name, err)
			continue
		}
		line := fmt.Sprintf("%s\t%d hosts", sess.Name, count)
		if sess.Description != "" {
			line += "\t" + sess.Description
		}
		fmt.Fprintln(stdout, line)
	}
	return exitOK
}
