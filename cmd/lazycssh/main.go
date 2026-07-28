// Command lazycssh opens SSH sessions to many hosts at once and broadcasts
// keyboard input to all of them.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

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
  lazycssh [flags] <host>...

Hosts may use brace expansion, for example srv1-{01..40}.example.com.

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

	if err := fs.Parse(args); err != nil {
		// flag already reported the error and printed the usage.
		return exitUsage
	}

	if *showVersion {
		fmt.Fprintln(stdout, version.String())
		return exitOK
	}

	hosts := fs.Args()
	if len(hosts) == 0 {
		fs.Usage()
		return exitUsage
	}

	// The transport and the TUI do not exist yet; see the SSH transport and TUI
	// shell epics. Report honestly rather than pretending to connect.
	fmt.Fprintf(stderr, "lazycssh %s: connecting is not implemented yet (%d host arguments given)\n",
		version.String(), len(hosts))
	return exitError
}
