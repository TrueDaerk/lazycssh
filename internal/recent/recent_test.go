package recent

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// store builds a store over a file in a temporary directory, with the given
// capacity (0 for [DefaultCapacity]).
func store(t *testing.T, capacity int) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "recent"), capacity)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// load reads the list and fails the test rather than the caller.
func load(t *testing.T, s *Store) []string {
	t.Helper()
	hosts, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return hosts
}

func TestNewStoreRejectsAnEmptyPath(t *testing.T) {
	if _, err := NewStore("", 0); err == nil {
		t.Fatal("NewStore accepted an empty path")
	}
}

// A capacity of zero is the default rather than a store that keeps nothing.
func TestNewStoreDefaultsTheCapacity(t *testing.T) {
	if got := store(t, 0).Capacity(); got != DefaultCapacity {
		t.Fatalf("Capacity() = %d, want %d", got, DefaultCapacity)
	}
}

// A machine that has connected to nothing yet has an empty list, not an error.
func TestLoadOfAMissingFileIsEmptyNotAnError(t *testing.T) {
	if hosts := load(t, store(t, 0)); hosts != nil {
		t.Fatalf("Load() = %v, want nil", hosts)
	}
}

// The whole point of the file: what was connected to survives the program.
func TestAddLoadRoundTripIsMostRecentFirst(t *testing.T) {
	s := store(t, 0)
	for _, host := range []string{"web-01", "db-01", "cache-7"} {
		if err := s.Add(host); err != nil {
			t.Fatalf("Add(%q): %v", host, err)
		}
	}

	// A second store over the same file is what a restart looks like.
	restarted, err := NewStore(s.Path(), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	want := []string{"cache-7", "db-01", "web-01"}
	if got := load(t, restarted); !slices.Equal(got, want) {
		t.Fatalf("Load() = %v, want %v", got, want)
	}
}

// Connecting to a host that is already in the list moves it to the front
// instead of putting it on a second row.
func TestAddMovesARepeatToTheFront(t *testing.T) {
	s := store(t, 0)
	for _, host := range []string{"web-01", "db-01", "cache-7", "db-01"} {
		if err := s.Add(host); err != nil {
			t.Fatalf("Add(%q): %v", host, err)
		}
	}

	want := []string{"db-01", "cache-7", "web-01"}
	if got := load(t, s); !slices.Equal(got, want) {
		t.Fatalf("Load() = %v, want %v", got, want)
	}
}

// Reconnecting the host that is already most recent must not rewrite the file:
// a flapping session reports connected over and over.
func TestAddOfTheMostRecentHostDoesNotRewrite(t *testing.T) {
	s := store(t, 0)
	if err := s.Add("web-01"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	before, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if err := s.Add("web-01"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	after, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) || before.Size() != after.Size() {
		t.Fatal("Add rewrote the file for the host that was already most recent")
	}
}

// The list is capped, and what falls off is what was reached longest ago.
func TestAddDropsTheOldestOverCapacity(t *testing.T) {
	s := store(t, 3)
	for _, host := range []string{"a", "b", "c", "d"} {
		if err := s.Add(host); err != nil {
			t.Fatalf("Add(%q): %v", host, err)
		}
	}

	want := []string{"d", "c", "b"}
	if got := load(t, s); !slices.Equal(got, want) {
		t.Fatalf("Load() = %v, want %v", got, want)
	}
}

// Nothing connected is nothing to record, not a failure.
func TestAddIgnoresAnEmptyHost(t *testing.T) {
	s := store(t, 0)
	if err := s.Add("   "); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if _, err := os.Stat(s.Path()); err == nil {
		t.Fatal("Add of an empty host created the file")
	}
}

// A hand-edited file is read the way it would have been written: blank lines
// skipped, repeats collapsed, the cap honoured.
func TestLoadCleansUpAHandEditedFile(t *testing.T) {
	s := store(t, 2)
	body := "web-01\n\n  db-01  \nweb-01\ncache-7\n"
	if err := os.WriteFile(s.Path(), []byte(body), filePerm); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	want := []string{"web-01", "db-01"}
	if got := load(t, s); !slices.Equal(got, want) {
		t.Fatalf("Load() = %v, want %v", got, want)
	}
}

// The file names what this account can reach; it is not world-readable.
func TestAddWritesAPrivateFile(t *testing.T) {
	s := store(t, 0)
	if err := s.Add("web-01"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != filePerm {
		t.Fatalf("file mode = %v, want %v", perm, filePerm)
	}
}

// The path follows XDG_CONFIG_HOME the way every other persisted file does.
func TestDefaultPathFollowsXDGConfigHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := filepath.Join(dir, "lazycssh", "recent"); path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}

// A full list stays at the cap however many hosts are added.
func TestAddKeepsTheListAtTheCap(t *testing.T) {
	s := store(t, 5)
	for i := range 50 {
		if err := s.Add(fmt.Sprintf("host-%02d", i)); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}
	if got := load(t, s); len(got) != 5 {
		t.Fatalf("Load() has %d entries, want 5: %v", len(got), got)
	}
}
