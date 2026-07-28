package commandlog

import (
	"strings"
	"testing"
	"time"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

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

	if !l.Record("systemctl restart nginx", broadcast.ModeAll, 40) {
		t.Fatal("the command was not recorded")
	}
	if l.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", l.Len())
	}

	entry, ok := l.Last()
	if !ok {
		t.Fatal("Last() reported nothing")
	}
	if entry.Targets != 40 || entry.Command != "systemctl restart nginx" {
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
	if l.Record(password, broadcast.ModeSingle, 1) {
		t.Fatal("single mode input was recorded")
	}
	if l.Len() != 0 {
		t.Fatalf("Len() = %d after single mode input", l.Len())
	}

	// And it is not hiding in a rendered entry either.
	l.Record("uptime", broadcast.ModeAll, 3)
	for _, entry := range l.Entries() {
		if strings.Contains(entry.String(), password) {
			t.Fatalf("an entry leaked single mode input: %q", entry.String())
		}
	}
}

func TestEmptyCommandsAreNotRecorded(t *testing.T) {
	l := testLog(t, 0)
	for _, command := range []string{"", "   ", "\n", "\r\n"} {
		if l.Record(command, broadcast.ModeAll, 3) {
			t.Fatalf("Record(%q) recorded an empty command", command)
		}
	}
	if l.Len() != 0 {
		t.Fatalf("Len() = %d", l.Len())
	}
}

func TestTrailingNewlineIsStripped(t *testing.T) {
	l := testLog(t, 0)
	l.Record("uptime\r\n", broadcast.ModeAll, 2)

	entry, _ := l.Last()
	if entry.Command != "uptime" {
		t.Fatalf("Command = %q", entry.Command)
	}
}

func TestEntriesAreOldestFirst(t *testing.T) {
	l := testLog(t, 0)
	l.Record("first", broadcast.ModeAll, 1)
	l.Record("second", broadcast.ModeSelected, 2)
	l.Record("third", broadcast.ModeFleet, 3)

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
	l.Record("uptime", broadcast.ModeAll, 1)

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
		l.Record(command, broadcast.ModeAll, 1)
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

	l.Record("uptime", broadcast.ModeAll, 1)
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
	l.Record("one", broadcast.ModeAll, 1)
	l.Record("two", broadcast.ModeAll, 1)
	l.Record("three", broadcast.ModeAll, 1)

	l.Clear()
	if l.Len() != 0 || l.Dropped() != 0 {
		t.Fatalf("after Clear: Len() = %d, Dropped() = %d", l.Len(), l.Dropped())
	}
}

func TestSetClockIgnoresNil(t *testing.T) {
	l := New(0)
	l.SetClock(nil)
	l.Record("uptime", broadcast.ModeAll, 1)

	entry, _ := l.Last()
	if entry.At.IsZero() {
		t.Fatal("a nil clock left the entry without a timestamp")
	}
}
