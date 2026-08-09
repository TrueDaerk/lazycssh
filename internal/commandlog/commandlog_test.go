package commandlog

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// hostIDs is a fleet of n synthetic host identifiers, the target set of a
// recorded command in tests that only care about the count.
func hostIDs(n int) []string {
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, fmt.Sprintf("host-%02d", i+1))
	}
	return out
}

// fixedClock returns a clock that always reports the same instant, so rendered
// timestamps can be asserted without sleeping.
func fixedClock() func() time.Time {
	at := time.Date(2026, 7, 28, 14, 5, 9, 0, time.UTC)
	return func() time.Time { return at }
}

func testLog(t *testing.T, capacity int) *Log {
	t.Helper()
	l := New(capacity)
	l.SetClock(fixedClock())
	return l
}

// The acceptance criterion: a command sent to 40 hosts appears once with its
// target count, not 40 times.
func TestOneCommandIsOneEntry(t *testing.T) {
	l := testLog(t, 0)

	if !l.Record("systemctl restart nginx", broadcast.ModeAll, hostIDs(40)) {
		t.Fatal("the command was not recorded")
	}
	if l.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", l.Len())
	}

	entry, ok := l.Last()
	if !ok {
		t.Fatal("Last() reported nothing")
	}
	if entry.Targets() != 40 || entry.Command != "systemctl restart nginx" {
		t.Fatalf("entry = %+v", entry)
	}
	if got := entry.String(); !strings.Contains(got, "40 hosts") || !strings.Contains(got, "[all]") {
		t.Fatalf("String() = %q", got)
	}
	if !strings.Contains(entry.String(), "14:05:09") {
		t.Fatalf("String() = %q, want the time it was sent", entry.String())
	}
}

// The other acceptance criterion: passwords typed in single mode never appear.
func TestSingleModeIsNeverRecorded(t *testing.T) {
	const password = "hunter2-typed-at-a-sudo-prompt"

	l := testLog(t, 0)
	if l.Record(password, broadcast.ModeSingle, hostIDs(1)) {
		t.Fatal("single mode input was recorded")
	}
	if l.Len() != 0 {
		t.Fatalf("Len() = %d after single mode input", l.Len())
	}

	// And it is not hiding in a rendered entry either.
	l.Record("uptime", broadcast.ModeAll, hostIDs(3))
	for _, entry := range l.Entries() {
		if strings.Contains(entry.String(), password) {
			t.Fatalf("an entry leaked single mode input: %q", entry.String())
		}
	}
}

func TestEmptyCommandsAreNotRecorded(t *testing.T) {
	l := testLog(t, 0)
	for _, command := range []string{"", "   ", "\n", "\r\n"} {
		if l.Record(command, broadcast.ModeAll, hostIDs(3)) {
			t.Fatalf("Record(%q) recorded an empty command", command)
		}
	}
	if l.Len() != 0 {
		t.Fatalf("Len() = %d", l.Len())
	}
}

func TestTrailingNewlineIsStripped(t *testing.T) {
	l := testLog(t, 0)
	l.Record("uptime\r\n", broadcast.ModeAll, hostIDs(2))

	entry, _ := l.Last()
	if entry.Command != "uptime" {
		t.Fatalf("Command = %q", entry.Command)
	}
}

func TestEntriesAreOldestFirst(t *testing.T) {
	l := testLog(t, 0)
	l.Record("first", broadcast.ModeAll, hostIDs(1))
	l.Record("second", broadcast.ModeSelected, hostIDs(2))
	l.Record("third", broadcast.ModeFleet, hostIDs(3))

	entries := l.Entries()
	if len(entries) != 3 {
		t.Fatalf("Entries() = %d entries", len(entries))
	}
	if entries[0].Command != "first" || entries[2].Command != "third" {
		t.Fatalf("order = %q, %q, %q",
			entries[0].Command, entries[1].Command, entries[2].Command)
	}
	if entries[1].Mode != broadcast.ModeSelected {
		t.Fatalf("mode = %v", entries[1].Mode)
	}
}

func TestEntriesReturnsACopy(t *testing.T) {
	l := testLog(t, 0)
	l.Record("uptime", broadcast.ModeAll, hostIDs(1))

	entries := l.Entries()
	entries[0].Command = "mutated"

	if got, _ := l.Last(); got.Command != "uptime" {
		t.Fatalf("Entries() aliased the log: %q", got.Command)
	}
}

// A long run must not grow without limit, and dropping has to be visible rather
// than silent: an audit trail that quietly forgets is worse than one that says
// it forgot.
func TestCapacityDropsTheOldest(t *testing.T) {
	l := testLog(t, 3)
	for _, command := range []string{"one", "two", "three", "four", "five"} {
		l.Record(command, broadcast.ModeAll, hostIDs(1))
	}

	if l.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", l.Len())
	}
	if l.Dropped() != 2 {
		t.Fatalf("Dropped() = %d, want 2", l.Dropped())
	}
	entries := l.Entries()
	if entries[0].Command != "three" || entries[2].Command != "five" {
		t.Fatalf("kept %q .. %q", entries[0].Command, entries[2].Command)
	}
}

func TestNewClampsCapacity(t *testing.T) {
	for _, capacity := range []int{0, -5} {
		l := New(capacity)
		if l.capacity != DefaultCapacity {
			t.Fatalf("New(%d) capacity = %d", capacity, l.capacity)
		}
	}
}

func TestAtAndLastOnAnEmptyLog(t *testing.T) {
	l := testLog(t, 0)

	if _, ok := l.Last(); ok {
		t.Fatal("Last() found an entry in an empty log")
	}
	if _, ok := l.At(0); ok {
		t.Fatal("At(0) found an entry in an empty log")
	}

	l.Record("uptime", broadcast.ModeAll, hostIDs(1))
	if _, ok := l.At(-1); ok {
		t.Fatal("At(-1) returned an entry")
	}
	if _, ok := l.At(1); ok {
		t.Fatal("At(1) returned an entry past the end")
	}
	if entry, ok := l.At(0); !ok || entry.Command != "uptime" {
		t.Fatalf("At(0) = %+v, %v", entry, ok)
	}
}

func TestClear(t *testing.T) {
	l := testLog(t, 2)
	l.Record("one", broadcast.ModeAll, hostIDs(1))
	l.Record("two", broadcast.ModeAll, hostIDs(1))
	l.Record("three", broadcast.ModeAll, hostIDs(1))

	l.Clear()
	if l.Len() != 0 || l.Dropped() != 0 {
		t.Fatalf("after Clear: Len() = %d, Dropped() = %d", l.Len(), l.Dropped())
	}
}

func TestSetClockIgnoresNil(t *testing.T) {
	l := New(0)
	l.SetClock(nil)
	l.Record("uptime", broadcast.ModeAll, hostIDs(1))

	entry, _ := l.Last()
	if entry.At.IsZero() {
		t.Fatal("a nil clock left the entry without a timestamp")
	}
}

// An entry remembers who it went to, not only how many: that is what lets a
// later send be aimed at the hosts that missed it (issue #256).
func TestEntryKeepsItsTargetSet(t *testing.T) {
	l := testLog(t, 0)
	targets := []string{"web-01", "web-02"}
	l.Record("uptime", broadcast.ModeAll, targets)

	// The caller may reuse the slice it passed in.
	targets[0] = "mutated"

	entry, _ := l.Last()
	if !entry.Received("web-01") || !entry.Received("web-02") {
		t.Fatalf("Hosts = %q, want the slice as it was at the send", entry.Hosts)
	}
	if entry.Received("web-03") {
		t.Fatal("Received reported a host that was never a target")
	}
	if entry.Targets() != 2 {
		t.Fatalf("Targets() = %d, want 2", entry.Targets())
	}
}

// The set difference the "send to missing" action is built on.
func TestMissing(t *testing.T) {
	tests := []struct {
		name      string
		received  []string
		connected []string
		want      []string
	}{{
		name:      "the host that was down at the send and is up now",
		received:  []string{"web-01", "web-02"},
		connected: []string{"web-01", "web-02", "web-03"},
		want:      []string{"web-03"},
	}, {
		name: "a host that joined the run after the send never received it",
		// web-03 was not in the run at all when the command went out.
		received:  []string{"web-01", "web-02"},
		connected: []string{"web-01", "web-02", "web-03", "web-04"},
		want:      []string{"web-03", "web-04"},
	}, {
		name: "a clone is its own host",
		// The clone's identifier is disambiguated, so it was never a target.
		received:  []string{"web-01", "web-02"},
		connected: []string{"web-01", "web-01#2", "web-02"},
		want:      []string{"web-01#2"},
	}, {
		name: "a session that is closed or failed is not a target",
		// Only the connected hosts are offered: a host that cannot take input
		// would swallow the command.
		received:  []string{"web-01"},
		connected: []string{"web-01", "web-03"},
		want:      []string{"web-03"},
	}, {
		name:      "nothing missing",
		received:  []string{"web-01", "web-02"},
		connected: []string{"web-01", "web-02"},
		want:      nil,
	}, {
		name:      "a host that received it and has since reconnected is not missing again",
		received:  []string{"web-01", "web-02"},
		connected: []string{"web-01", "web-02"},
		want:      nil,
	}, {
		name:      "a target that has since left the run is simply not offered",
		received:  []string{"web-01", "web-02"},
		connected: []string{"web-01"},
		want:      nil,
	}, {
		name:      "nothing was ever sent",
		received:  nil,
		connected: []string{"web-01", "web-02"},
		want:      []string{"web-01", "web-02"},
	}, {
		name:      "a repeated host is offered once",
		received:  nil,
		connected: []string{"web-01", "web-01"},
		want:      []string{"web-01"},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Missing(tc.received, tc.connected)
			if len(got) != len(tc.want) {
				t.Fatalf("Missing() = %q, want %q", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Missing() = %q, want %q", got, tc.want)
				}
			}
		})
	}
}

// Entry.Missing is the same difference, taken against the entry's own targets.
func TestEntryMissing(t *testing.T) {
	l := testLog(t, 0)
	l.Record("systemctl restart nginx", broadcast.ModeAll, []string{"web-01", "web-02"})

	entry, _ := l.Last()
	got := entry.Missing([]string{"web-01", "web-02", "web-03"})
	if len(got) != 1 || got[0] != "web-03" {
		t.Fatalf("Missing() = %q, want [web-03]", got)
	}
}
