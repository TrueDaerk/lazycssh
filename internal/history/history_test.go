package history

import (
	"os"
	"path/filepath"
	"testing"
)

// store builds a store over a file in a temporary directory, with the given
// capacity (0 for [DefaultCapacity]).
func store(t *testing.T, capacity int) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "history"), capacity)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func TestNewStoreRejectsAnEmptyPath(t *testing.T) {
	if _, err := NewStore("", 0); err == nil {
		t.Fatal("NewStore accepted an empty path")
	}
}

func TestLoadOfAMissingFileIsEmptyNotAnError(t *testing.T) {
	s := store(t, 0)
	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if entries != nil {
		t.Fatalf("Load() = %v, want nil", entries)
	}
}

func TestAppendLoadRoundTrip(t *testing.T) {
	s := store(t, 0)
	for _, cmd := range []string{"uptime", "df -h", "systemctl status nginx"} {
		if err := s.Append(cmd); err != nil {
			t.Fatalf("Append(%q): %v", cmd, err)
		}
	}

	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"uptime", "df -h", "systemctl status nginx"}
	if !equal(entries, want) {
		t.Fatalf("Load() = %v, want %v", entries, want)
	}
}

func TestAppendSkipsAConsecutiveRepeat(t *testing.T) {
	s := store(t, 0)
	for _, cmd := range []string{"uptime", "uptime", "uptime", "df -h", "df -h"} {
		if err := s.Append(cmd); err != nil {
			t.Fatalf("Append(%q): %v", cmd, err)
		}
	}

	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"uptime", "df -h"}
	if !equal(entries, want) {
		t.Fatalf("Load() = %v, want %v", entries, want)
	}
}

func TestAppendAllowsANonConsecutiveRepeat(t *testing.T) {
	s := store(t, 0)
	for _, cmd := range []string{"uptime", "df -h", "uptime"} {
		if err := s.Append(cmd); err != nil {
			t.Fatalf("Append(%q): %v", cmd, err)
		}
	}

	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"uptime", "df -h", "uptime"}
	if !equal(entries, want) {
		t.Fatalf("Load() = %v, want %v", entries, want)
	}
}

func TestAppendOfAnEmptyCommandIsANoOp(t *testing.T) {
	s := store(t, 0)
	if err := s.Append("   "); err != nil {
		t.Fatalf("Append: %v", err)
	}
	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if entries != nil {
		t.Fatalf("Load() = %v, want nil", entries)
	}
	if _, err := os.Stat(s.Path()); err == nil {
		t.Fatal("Append of an empty command created the file")
	}
}

func TestAppendCapsTheFile(t *testing.T) {
	s := store(t, 3)
	for _, cmd := range []string{"a", "b", "c", "d", "e"} {
		if err := s.Append(cmd); err != nil {
			t.Fatalf("Append(%q): %v", cmd, err)
		}
	}

	entries, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"c", "d", "e"}
	if !equal(entries, want) {
		t.Fatalf("Load() = %v, want %v", entries, want)
	}
}

func TestAppendCreatesTheFileWithRestrictedPermissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permission bits are not enforced for root")
	}
	s := store(t, 0)
	if err := s.Append("uptime"); err != nil {
		t.Fatalf("Append: %v", err)
	}

	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != filePerm {
		t.Fatalf("history file permissions = %o, want %o", perm, filePerm)
	}
}

func TestDefaultPathUsesXDGConfigHomeWhenSet(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/xdg-config")
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := filepath.Join("/xdg-config", "lazycssh", "history"); path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}

func TestDefaultPathFallsBackToHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := filepath.Join(home, ".config", "lazycssh", "history"); path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
