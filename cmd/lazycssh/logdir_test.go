package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/program"
)

func TestRunWithLogDirOpensTheSessionLog(t *testing.T) {
	root := filepath.Join(t.TempDir(), "logs")
	var stdout, stderr bytes.Buffer
	var launched program.Config
	code := run([]string{"--log-dir", root, "a.example.com"},
		&stdout, &stderr, captureLaunch(&launched))

	if code != exitOK {
		t.Fatalf("run = %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if launched.Logs == nil {
		t.Fatal("the TUI was launched without the session log")
	}
	if got := launched.Logs.Dir(); !strings.HasPrefix(got, root) {
		t.Errorf("run directory %q is not under %q", got, root)
	}
	if _, err := os.Stat(launched.Logs.Dir()); err != nil {
		t.Errorf("run directory was not created: %v", err)
	}
	// The one line a user pipes: where the logs went.
	if !strings.Contains(stdout.String(), launched.Logs.Dir()) {
		t.Errorf("stdout = %q, want it to name %q", stdout.String(), launched.Logs.Dir())
	}
}

func TestRunWithoutLogDirDoesNotLog(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var launched program.Config
	code := run([]string{"a.example.com"}, &stdout, &stderr, captureLaunch(&launched))

	if code != exitOK {
		t.Fatalf("run = %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if launched.Logs != nil {
		t.Fatal("session logging is on without the flag")
	}
	if strings.Contains(stdout.String(), "session logs") {
		t.Errorf("stdout mentions session logs without the flag: %q", stdout.String())
	}
}

func TestRunWithAnUnusableLogDirFailsBeforeTheTUI(t *testing.T) {
	// A file where the root directory should be: MkdirAll refuses.
	root := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--log-dir", root, "a.example.com"},
		&stdout, &stderr, noLaunch(t))

	if code != exitError {
		t.Fatalf("run = %d, want %d", code, exitError)
	}
	if stderr.Len() == 0 {
		t.Error("the failure is silent")
	}
}
