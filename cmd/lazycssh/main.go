// Command lazycssh opens SSH sessions to many hosts at once and broadcasts
// keyboard input to all of them.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

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
  lazycssh [flags] <host|@session>...

Hosts may use brace expansion, for example srv1-{01..40}.example.com.
An argument starting with @ names a saved session; extra hosts on the command
line are merged into it.

Flags:
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point: it takes the arguments without the program
// name, writes everything to the given writers, and returns the process exit
// code instead of calling os.Exit.
func run(args []string, stdout, stderr io.Writer) int {
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

	args = fs.Args()
	if len(args) == 0 {
		fs.Usage()
		return exitUsage
	}

	resolved, err := resolvePlan(args, store)
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

	// The transport and the TUI do not exist yet; see the SSH transport and TUI
	// shell epics. Report honestly rather than pretending to connect.
	if names := resolved.SessionNames(); len(names) > 0 {
		fmt.Fprintf(stderr, "lazycssh %s: loaded session %s\n",
			version.String(), strings.Join(names, ", "))
	}
	fmt.Fprintf(stderr, "lazycssh %s: connecting is not implemented yet (%d host arguments given)\n",
		version.String(), len(resolved.Patterns))
	return exitError
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
