// Package commandlog is the audit trail of what this run did: which commands
// were sent, in which broadcast mode, and to how many hosts.
//
// It answers "what did I just do to production" - which is the question a tool
// that types on forty machines at once has to be able to answer.
//
// Two rules the type enforces rather than documents:
//
//   - A command sent to forty hosts is **one** entry with a count of forty, not
//     forty entries. The log is about what the user did, not about what the
//     wire carried.
//   - Input sent in [broadcast.ModeSingle] is **never** recorded. Single mode is
//     the mode used to answer a sudo prompt, and a log that captured it would be
//     a plaintext password file that nobody asked for.
//
// The log lives in memory. Nothing here writes to disk; session logging is a
// separate, opt-in feature.
package commandlog

import (
	"fmt"
	"strings"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// DefaultCapacity bounds how many entries are kept. A long run must not grow
// without limit; the oldest entries are dropped first.
const DefaultCapacity = 1000

// Entry is one command as it was sent.
type Entry struct {
	// Command is what was sent, without the trailing newline.
	Command string
	// Mode is the broadcast mode it went out in.
	Mode broadcast.Mode
	// Hosts are the session identifiers that received it, in host order. It is
	// the entry's target set: what "send this to the hosts that missed it" is
	// the complement of. Recorded by identifier rather than by pane, so it
	// survives paging and a changing grid.
	Hosts []string
	// At is when it was sent.
	At time.Time
}

// Targets is how many hosts received the command.
func (e Entry) Targets() int { return len(e.Hosts) }

// Received reports whether a host was one of the entry's targets.
func (e Entry) Received(id string) bool {
	for _, host := range e.Hosts {
		if host == id {
			return true
		}
	}
	return false
}

// Missing returns the hosts that are connected now and were not targets of
// this command, in the order they were given - the set a resend that must not
// double-execute goes to.
//
// The rule, deliberately: **membership is by session identifier, and a host
// that received the command is never missing again.** A session that has since
// reconnected has a fresh shell and arguably lost what the command did, but it
// did receive it, and guessing otherwise would re-run `rm -rf` on a machine the
// user did not ask about. Re-running there is the explicit resend (`enter`),
// one keypress away. A clone or a host that joined the run after the send has
// its own identifier, never was a target, and is therefore missing - which is
// the case this exists for.
func (e Entry) Missing(connected []string) []string {
	return Missing(e.Hosts, connected)
}

// Missing is the set difference behind [Entry.Missing]: connected minus
// received, keeping the order of connected.
func Missing(received, connected []string) []string {
	got := make(map[string]struct{}, len(received))
	for _, id := range received {
		got[id] = struct{}{}
	}

	var out []string
	seen := make(map[string]struct{}, len(connected))
	for _, id := range connected {
		if _, ok := got[id]; ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// String renders the entry the way the Command log panel shows it.
func (e Entry) String() string {
	n := e.Targets()
	return fmt.Sprintf("%s  %s  → %d host%s [%s]",
		e.At.Format("15:04:05"), e.Command, n, plural(n), e.Mode)
}

// plural is the "s" that makes a count read as English.
func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// Log is the in-memory command history of one run.
//
// The zero value is not usable; construct one with [New]. A Log is not safe for
// concurrent use: it is owned by the bubbletea model, and everything that
// touches it runs inside Update.
type Log struct {
	entries  []Entry
	capacity int
	dropped  int
	now      func() time.Time
}

// New builds a log. A capacity of zero means [DefaultCapacity].
func New(capacity int) *Log {
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Log{capacity: capacity, now: time.Now}
}

// SetClock replaces the clock, which exists so the tests can assert on rendered
// timestamps without sleeping.
func (l *Log) SetClock(now func() time.Time) {
	if now != nil {
		l.now = now
	}
}

// Record appends a command, with the hosts that received it.
//
// The hosts are the entry's target set, not only its count: an entry that knows
// who received it can also answer who did not, which is what a resend to the
// hosts that missed it needs (issue #256). The slice is copied, so a caller may
// reuse it.
//
// It reports whether the command was recorded: a command sent in
// [broadcast.ModeSingle] is deliberately not, and neither is an empty one. The
// return value exists so a caller cannot quietly assume a record was made.
func (l *Log) Record(command string, mode broadcast.Mode, hosts []string) bool {
	if mode == broadcast.ModeSingle {
		// Single mode is how a password prompt is answered. Recording it would
		// turn the audit trail into a credential store.
		return false
	}
	command = strings.TrimRight(command, "\r\n")
	if strings.TrimSpace(command) == "" {
		return false
	}

	l.entries = append(l.entries, Entry{
		Command: command,
		Mode:    mode,
		Hosts:   append([]string(nil), hosts...),
		At:      l.now(),
	})

	if over := len(l.entries) - l.capacity; over > 0 {
		l.entries = append([]Entry(nil), l.entries[over:]...)
		l.dropped += over
	}
	return true
}

// Entries returns the commands in the order they were sent, oldest first.
func (l *Log) Entries() []Entry { return append([]Entry(nil), l.entries...) }

// Len is how many entries are kept.
func (l *Log) Len() int { return len(l.entries) }

// Dropped is how many entries fell off the start of a full log.
func (l *Log) Dropped() int { return l.dropped }

// Last returns the most recent entry.
func (l *Log) Last() (Entry, bool) {
	if len(l.entries) == 0 {
		return Entry{}, false
	}
	return l.entries[len(l.entries)-1], true
}

// At returns the entry at an index, oldest first.
func (l *Log) At(i int) (Entry, bool) {
	if i < 0 || i >= len(l.entries) {
		return Entry{}, false
	}
	return l.entries[i], true
}

// Clear empties the log.
func (l *Log) Clear() {
	l.entries = nil
	l.dropped = 0
}
