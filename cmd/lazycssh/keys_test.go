package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/program"
	"github.com/TrueDaerk/lazycssh/internal/ui"
)

// writeKeys puts a keymap file in a fresh directory and returns its path.
func writeKeys(t *testing.T, dir, doc string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("creating %s: %v", dir, err)
	}
	path := filepath.Join(dir, ui.KeyMapFileName)
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestLoadKeysReadsAnExplicitFile(t *testing.T) {
	path := writeKeys(t, t.TempDir(), "Retile: ctrl+t\n")

	keys, err := loadKeys(path)
	if err != nil {
		t.Fatalf("loadKeys() = %v", err)
	}
	if got := keys.Retile.Keys(); !slices.Equal(got, []string{"ctrl+t"}) {
		t.Fatalf("Retile = %v, want the override", got)
	}
}

// A file named on the command line is a promise it exists; a silent fallback
// to the defaults would hide a typed path.
func TestLoadKeysRejectsAMissingExplicitFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.yaml")

	if _, err := loadKeys(path); err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("loadKeys(%q) = %v, want the missing file reported", path, err)
	}
}

// Without a flag the run reads the default location, and finding nothing there
// is the normal case: no file, shipped bindings.
func TestLoadKeysFollowsTheDefaultLocation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	keys, err := loadKeys("")
	if err != nil {
		t.Fatalf("loadKeys() with no file = %v", err)
	}
	if got := keys.Retile.Keys(); !slices.Equal(got, ui.DefaultKeyMap().Retile.Keys()) {
		t.Fatalf("Retile = %v without a file, want the default", got)
	}

	writeKeys(t, filepath.Join(dir, "lazycssh"), "Retile: ctrl+t\n")
	keys, err = loadKeys("")
	if err != nil {
		t.Fatalf("loadKeys() = %v", err)
	}
	if got := keys.Retile.Keys(); !slices.Equal(got, []string{"ctrl+t"}) {
		t.Fatalf("Retile = %v, want the file at the default location applied", got)
	}
}

// The keymap reaches the program, which is what makes a rebinding visible in
// the interface.
func TestRunPassesTheKeyMapToTheProgram(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := writeKeys(t, t.TempDir(), "Retile: ctrl+t\n")

	var stdout, stderr bytes.Buffer
	var launched program.Config
	code := run([]string{"--keys-file", path, "a.example.com"}, &stdout, &stderr, captureLaunch(&launched))

	if code != exitOK {
		t.Fatalf("run() = %d, want %d (stderr: %q)", code, exitOK, stderr.String())
	}
	if launched.Keys == nil {
		t.Fatal("the run handed the program no keymap")
	}
	if got := launched.Keys.Retile.Keys(); !slices.Equal(got, []string{"ctrl+t"}) {
		t.Fatalf("Retile = %v, want the file's binding", got)
	}
}

// A broken keymap stops the run on the terminal that started it, naming the
// entry - not while typing to forty machines.
func TestRunReportsABrokenKeyMap(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	path := writeKeys(t, t.TempDir(), "Retile: ctrl+t\nRetiel: ctrl+u\n")

	var stdout, stderr bytes.Buffer
	code := run([]string{"--keys-file", path, "a.example.com"}, &stdout, &stderr, noLaunch(t))

	if code != exitError {
		t.Fatalf("run() = %d, want %d", code, exitError)
	}
	for _, want := range []string{path, "Retiel", "unknown action"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr = %q, want it to name %q", stderr.String(), want)
		}
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want it empty on a failure", stdout.String())
	}
}

// The action names are listable, because a configuration surface nobody can
// enumerate is one nobody can write.
func TestRunListsTheKeyActions(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"--list-key-actions"}, &stdout, &stderr, noLaunch(t))

	if code != exitOK {
		t.Fatalf("run() = %d, want %d (stderr: %q)", code, exitOK, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{"Retile", "ctrl+r", "re-tile the grid"} {
		if !strings.Contains(out, want) {
			t.Errorf("the listing does not carry %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "Prefix\tctrl+a") || !strings.Contains(out, "[fixed") {
		t.Errorf("the listing does not mark the prefix as fixed:\n%s", out)
	}
	if lines := strings.Count(strings.TrimSpace(out), "\n") + 1; lines != len(ui.KeyMapActions()) {
		t.Errorf("the listing has %d lines, want one per action (%d)", lines, len(ui.KeyMapActions()))
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want it empty", stderr.String())
	}
}
