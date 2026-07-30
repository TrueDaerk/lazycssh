package ui

import "fmt"

// A non-zero exit on 3 of 20 hosts must be findable in one keystroke. The
// transport reports exit codes through the prompt hook (see
// internal/ssh/exit.go); this file is everything the interface does with them:
// the label, the count, and the jump.

// lastExit reads a host's last reported exit status from the fleet snapshot.
// Without a transport, or on a shell that never ran the hook, nothing was
// reported and the second value is false - the honest answer, never a made-up
// zero.
func (a App) lastExit(id string) (int, bool) {
	st, ok := a.hostStates[id]
	if !ok {
		return 0, false
	}
	return st.exit, st.exitReported
}

// commandFailed reports whether a host's last command exited non-zero.
func (a App) commandFailed(id string) bool {
	code, ok := a.lastExit(id)
	return ok && code != 0
}

// failedHosts returns the indexes of the hosts whose last command failed, in
// pane order.
func (a App) failedHosts() []int {
	var out []int
	for i, id := range a.hostIDs() {
		if a.commandFailed(id) {
			out = append(out, i)
		}
	}
	return out
}

// exitLabel renders an exit status for a header or a list row: "ok" for zero,
// "exit N" for a failure, empty while nothing has been reported. The failure is
// bold as well as coloured, so it survives a terminal without colour.
func (a App) exitLabel(id string) string {
	code, ok := a.lastExit(id)
	if !ok {
		return ""
	}
	if code == 0 {
		return a.theme.ExitOK.Render("ok")
	}
	return a.theme.Failure.Render(fmt.Sprintf("exit %d", code))
}

// jumpToNextFailure moves the pane focus to the next host whose last command
// failed, searching forward from the focused pane and wrapping around. The
// wrap is deliberate, unlike pane movement: this is a search, and a failure
// behind the cursor must be as reachable as one ahead of it.
func (a App) jumpToNextFailure() App {
	failed := a.failedHosts()
	if len(failed) == 0 {
		return a
	}
	for _, i := range failed {
		if i > a.paneIndex {
			return a.focusPane(i)
		}
	}
	return a.focusPane(failed[0])
}

// focusPane puts the pane focus on a host and brings its page on screen.
func (a App) focusPane(i int) App {
	a.paneIndex = clamp(i, 0, max(0, len(a.hostIDs())-1))
	a.focus = AreaGrid
	return a.followFocus()
}

// failureSummary renders the status bar count, empty when everything is fine.
func (a App) failureSummary() string {
	n := len(a.failedHosts())
	if n == 0 {
		return ""
	}
	return a.theme.Failure.Render(fmt.Sprintf("%d host%s failed", n, plural(n)))
}
