package ui

import (
	"fmt"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// After broadcasting a command the question is "who failed". A non-zero exit on
// 3 of 20 hosts must be findable in one keystroke. The transport reports exit
// codes through the prompt hook (see internal/ssh/exit.go); this file is
// everything the interface does with them.
//
// The indicator is per command, not per session (issue #251). What a pane
// header shows is the status of *the last command this host was sent from the
// command line* - nothing else. A send records each reached target's marker
// sequence ([App.markCommandExits]); until the next marker arrives past that
// mark the host is still running the command and the header greys out, and the
// answer that was on screen before the send never survives it.
//
// Three deliberate silences, all of them "no indicator" rather than a guess:
//
//   - a shell that never emitted a marker - plain POSIX sh, a restricted shell,
//     a profile that overwrites the hook - reports nothing, and a host that has
//     reported nothing shows nothing. A green tick there would be invented;
//   - a host the send did not reach, and a host outside the broadcast scope,
//     keeps no mark: its old answer belongs to an older question;
//   - a reconnect starts a new session with its own counter, so a mark from the
//     shell that died is dropped rather than compared against a smaller number.
//
// Raw keystrokes - the broadcast bar, typing into a focused pane - mark
// nothing. lazycssh cannot tell where a command starts in a stream of typing,
// so it makes no claim about one.

// exitStatus is what the interface knows about a host's last command.
type exitStatus int

const (
	// exitUnknown means there is nothing honest to show.
	exitUnknown exitStatus = iota
	// exitRunning means the command was sent and the shell has not reached its
	// next prompt yet.
	exitRunning
	// exitReported means the shell answered with a code.
	exitReported
)

// Header marks. A success is one character because `ok` on two hundred panes
// buries the three that matter, a failure spells out its code because colour
// is never allowed to be the only signal, and the pending mark is a dot: the
// command is out, the answer is not in.
const (
	exitOKMark      = "✓"
	exitRunningMark = "·"
)

// broadcastHosts is who a command typed now would go to: the current broadcast
// targets, or - with no router to ask - every host in the run, which is how the
// views are tested without a transport.
func (a App) broadcastHosts() []string {
	if a.cfg.Targets != nil {
		return a.cfg.Targets.Targets()
	}
	return a.fleetIDs()
}

// exitMarksFor reads where the given hosts' exit markers stand right now. It
// runs before the command's bytes leave, because a host that answers between
// the write and the read would have its answer counted as the state the send
// found - and the pane would then show the new command as already finished.
//
// The sequences come from the live sessions rather than from the fleet
// snapshot. The snapshot is refreshed by the fleet event an exit produces, so
// it is normally current - but a marker that arrived since the last event
// would leave a stale, too-low mark behind, with the same consequence. This
// runs inside Update, never in a render path.
func (a App) exitMarksFor(hosts []string) map[string]uint64 {
	marks := make(map[string]uint64, len(hosts))
	for _, id := range hosts {
		marks[id] = a.liveExitSeq(id)
	}
	return marks
}

// markCommandExits opens a new per-command status window over the marks the
// send was taken at, minus the hosts it could not reach.
//
// The map is replaced, not extended: hosts outside this send have no answer to
// this question, and a stale tick on a pane the command never reached is
// exactly the misreading this indicator exists to prevent.
func (a App) markCommandExits(marks map[string]uint64, d broadcast.Delivery) App {
	next := make(map[string]uint64, len(marks))
	for id, seq := range marks {
		next[id] = seq
	}
	for _, id := range d.Failed {
		delete(next, id)
	}
	a.cmdExitMarks = next
	return a
}

// liveExitSeq reads a host's exit-marker sequence from the transport. It is
// called from Update, at the send, never from a render path.
func (a App) liveExitSeq(id string) uint64 {
	if a.cfg.Fleet == nil {
		return 0
	}
	session, ok := a.cfg.Fleet.Session(id)
	if !ok {
		return 0
	}
	_, seq := session.LastExit()
	return seq
}

// commandExit is a host's status for the last command it was sent, read from
// the fleet snapshot the way every other render input is.
func (a App) commandExit(id string) (exitStatus, int) {
	mark, tracked := a.cmdExitMarks[id]
	if !tracked {
		return exitUnknown, 0
	}
	st, ok := a.hostStates[id]
	if !ok || st.exitSeq < mark {
		// A reconnect replaced the session, and its marker counter with it:
		// the mark belongs to a shell that no longer exists.
		return exitUnknown, 0
	}
	if st.exitSeq == mark {
		if st.exitSeq == 0 {
			// No marker has ever arrived from this host. The hook may not be
			// armed at all, and a pane greyed out forever would be a claim
			// about a command nobody can confirm ran.
			return exitUnknown, 0
		}
		return exitRunning, 0
	}
	return exitReported, st.exit
}

// commandFailed reports whether a host's last command exited non-zero. A
// command still running has not failed - it has not finished.
func (a App) commandFailed(id string) bool {
	status, code := a.commandExit(id)
	return status == exitReported && code != 0
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

// exitLabel renders a host's command status for its pane header: a tick for a
// success, "exit N" for a failure, a dot while the command is still out, and
// nothing at all when there is nothing to say. The failure is bold as well as
// coloured, and every state has its own glyph, so the header survives a
// terminal without colour.
func (a App) exitLabel(id string) string {
	status, code := a.commandExit(id)
	switch status {
	case exitRunning:
		return a.theme.ExitPending.Render(exitRunningMark)
	case exitReported:
		if code == 0 {
			return a.theme.ExitOK.Render(exitOKMark)
		}
		return a.theme.Failure.Render(fmt.Sprintf("exit %d", code))
	}
	return ""
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
