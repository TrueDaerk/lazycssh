package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// pressExportKey presses alt+w and, when it produced a Cmd, runs it and
// folds the resulting PaneExportedMsg back through Update - the same shape
// a real program loop takes, so the status line assertion sees what a user
// would see.
func pressExportKey(t *testing.T, a App) App {
	t.Helper()

	model, cmd := a.Update(keyMsgFor(t, "alt+w"))
	next, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	if cmd == nil {
		return next
	}
	msg, ok := cmd().(PaneExportedMsg)
	if !ok {
		t.Fatalf("the export Cmd produced a %T, want PaneExportedMsg", msg)
	}
	model, _ = next.Update(msg)
	next, ok = model.(App)
	if !ok {
		t.Fatalf("Update returned a %T, want App", model)
	}
	return next
}

// alt+w writes the focused pane's whole retained scrollback to a file named
// after the host and the moment of export, ANSI stripped, and the status
// line reports the path (issue #252).
func TestAltWExportsScrollbackToFile(t *testing.T) {
	t.Chdir(t.TempDir())

	a, fleet, _, _ := statusApp(t, "web-01")
	for i := 1; i <= 5; i++ {
		fleet.sessions["web-01"].Emitf("line-%02d\n", i)
	}
	fleet.sessions["web-01"].Emit("\x1b[31mred-line\x1b[0m\n")
	a = focusGrid(t, a)
	a.now = func() time.Time { return time.Date(2026, 8, 9, 14, 5, 9, 0, time.UTC) }

	a = pressExportKey(t, a)

	if !strings.Contains(a.lastDelivery, "web-01") || !strings.Contains(a.lastDelivery, "wrote") {
		t.Fatalf("the status line does not report the export: %q", a.lastDelivery)
	}
	wantName := "lazycssh-web-01-2026-08-09_14-05-09.log"
	if !strings.Contains(a.lastDelivery, wantName) {
		t.Fatalf("the status line does not carry the file name: %q", a.lastDelivery)
	}

	got, err := os.ReadFile(wantName)
	if err != nil {
		t.Fatalf("the export file was not written: %v", err)
	}
	content := string(got)
	if !strings.Contains(content, "line-01") || !strings.Contains(content, "red-line") {
		t.Fatalf("the export misses scrollback lines: %q", content)
	}
	if strings.Contains(content, "\x1b") {
		t.Fatalf("the export carries ANSI sequences: %q", content)
	}
}

// An empty pane reports "nothing to export" and writes no file.
func TestExportOnAnEmptyPane(t *testing.T) {
	t.Chdir(t.TempDir())

	a, _, _, _ := statusApp(t, "web-01")
	a = focusGrid(t, a)

	a = pressExportKey(t, a)
	if !strings.Contains(a.lastDelivery, "nothing to export") {
		t.Fatalf("the status line says %q", a.lastDelivery)
	}

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("an empty pane wrote a file: %v", entries)
	}
}

// Export works while typing into the pane too, and the chord never reaches
// the host - alt+w must not leak a "w" into the shell.
func TestExportWorksWhileTyping(t *testing.T) {
	t.Chdir(t.TempDir())

	a, fleet := typingApp(t, "web-01")
	fleet.sessions["web-01"].Emit("output\n")

	a = pressExportKey(t, a)
	if !strings.Contains(a.lastDelivery, "wrote") {
		t.Fatalf("alt+w while typing did not export: %q", a.lastDelivery)
	}
	if got := fleet.sessions["web-01"].Written(); strings.Contains(got, "w") {
		t.Fatalf("the chord leaked to the host: %q", got)
	}
}

// exportFileName sanitizes a host id the same way sessionlog sanitizes one:
// path separators, colons and control bytes cannot become directory
// traversal or collide the timestamp component, and a leading dot cannot
// hide the file.
func TestExportFileNameSanitizesTheHostID(t *testing.T) {
	at := time.Date(2026, 8, 9, 14, 5, 9, 0, time.UTC)
	tests := []struct {
		host string
		want string
	}{
		{"web-01", "lazycssh-web-01-2026-08-09_14-05-09.log"},
		{"user@host:22", "lazycssh-user@host_22-2026-08-09_14-05-09.log"},
		{"../etc/passwd", "lazycssh-_.._etc_passwd-2026-08-09_14-05-09.log"},
		{".hidden", "lazycssh-_.hidden-2026-08-09_14-05-09.log"},
	}
	for _, tt := range tests {
		if got := exportFileName(at, tt.host); got != tt.want {
			t.Errorf("exportFileName(%q) = %q, want %q", tt.host, got, tt.want)
		}
		// The result must never contain a path separator - the caller writes
		// it straight into the working directory, unjoined.
		if strings.ContainsAny(exportFileName(at, tt.host), "/\\") {
			t.Errorf("exportFileName(%q) contains a path separator: %q", tt.host, exportFileName(at, tt.host))
		}
	}
}

// The exported file is a plain name, not a path: writing it never escapes
// the working directory even for a maximally hostile host id.
func TestExportFileNameStaysInOneDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}

	name := exportFileName(time.Now(), "../../etc/passwd")
	if err := os.WriteFile(name, []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile(%q): %v", name, err)
	}
	if dir := filepath.Dir(filepath.Join(cwd, name)); dir != cwd {
		t.Fatalf("the export landed outside the working directory: %s", dir)
	}
}
