// Package clipboard writes to the clipboard of the machine lazycssh is
// running on, as the fallback for terminals that do not act on OSC 52.
//
// Copying out of a pane is primarily an OSC 52 sequence (see internal/ui):
// the terminal is asked to put the text on its own clipboard, which is the
// only mechanism that works when lazycssh itself runs over SSH. Not every
// terminal does it, and the failure is silent — macOS Terminal.app has no
// OSC 52 support at all, and iTerm2 keeps it behind "Applications in terminal
// may access clipboard", off by default. To the user that reads as "cmd+c
// does nothing" (issue #307).
//
// So a local run also writes the text to the OS clipboard directly, through
// the platform's own tool (pbcopy, wl-copy, xclip/xsel, clip.exe). A run
// inside an SSH session deliberately does not: there the OS clipboard belongs
// to the far machine, not to the user sitting in front of the terminal, and
// OSC 52 is the only path that can reach them.
package clipboard

import (
	"fmt"
	"os"

	"github.com/atotto/clipboard"
)

// Writer puts text on a clipboard. It is the interface internal/ui consumes;
// nil means OSC 52 alone.
type Writer interface {
	Write(text string) error
}

// System is the [Writer] backed by the machine's own clipboard tool.
type System struct{}

// Write puts text on the OS clipboard.
func (System) Write(text string) error {
	if err := clipboard.WriteAll(text); err != nil {
		return fmt.Errorf("write to the system clipboard: %w", err)
	}
	return nil
}

// New returns the writer to install, or nil when there is none to install:
// no usable clipboard tool on this machine, or a run inside an SSH session,
// where the local clipboard is the wrong machine's. A nil return is a nil
// interface, not a typed nil, so a caller can assign it straight into an
// interface field.
//
// env is the environment lookup, [os.Getenv] when nil.
func New(env func(string) string) Writer {
	if env == nil {
		env = os.Getenv
	}
	if RemoteSession(env) || clipboard.Unsupported {
		return nil
	}
	return System{}
}

// RemoteSession reports whether this process is itself running inside an SSH
// session. sshd sets these for every interactive login, and none of them
// survive into a purely local shell.
func RemoteSession(env func(string) string) bool {
	if env == nil {
		env = os.Getenv
	}
	return env("SSH_CONNECTION") != "" || env("SSH_CLIENT") != "" || env("SSH_TTY") != ""
}
