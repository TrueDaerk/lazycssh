package sessionlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var start = time.Date(2026, 8, 9, 14, 5, 9, 0, time.UTC)

func open(t *testing.T) *Run {
	t.Helper()
	r, err := Open(t.TempDir(), start)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestOpenCreatesTimestampedRunDir(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root, start)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	want := filepath.Join(root, "2026-08-09_14-05-09")
	if r.Dir() != want {
		t.Fatalf("Dir() = %q, want %q", r.Dir(), want)
	}
	info, err := os.Stat(r.Dir())
	if err != nil {
		t.Fatalf("stat run dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Errorf("run dir permissions = %o, want 700", perm)
	}
}

func TestOpenCreatesMissingRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "logs", "nested")
	r, err := Open(root, start)
	if err != nil {
		t.Fatalf("Open with missing root: %v", err)
	}
	r.Close()
}

func TestOpenTwiceInTheSameSecondGetsDistinctDirs(t *testing.T) {
	root := t.TempDir()
	a, err := Open(root, start)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	defer a.Close()
	b, err := Open(root, start)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	defer b.Close()

	if a.Dir() == b.Dir() {
		t.Fatalf("both runs share %q", a.Dir())
	}
}

func TestOpenRefusesEmptyRoot(t *testing.T) {
	if _, err := Open("", start); err == nil {
		t.Fatal("Open(\"\") succeeded")
	}
}

func TestHostLogWritesOutput(t *testing.T) {
	r := open(t)
	h := r.HostLog("web-01")

	if _, err := h.Write([]byte("hello\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := h.Write([]byte("world\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if got := read(t, h.Path()); got != "hello\nworld\n" {
		t.Fatalf("log = %q", got)
	}
	info, err := os.Stat(h.Path())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
}

func TestHostLogSameHostAppendsWithReconnectMarker(t *testing.T) {
	r := open(t)
	first := r.HostLog("web-01")
	first.Write([]byte("before\n"))

	second := r.HostLog("web-01")
	if second != first {
		t.Fatal("reconnect opened a second log for the same host")
	}
	second.Write([]byte("after\n"))

	got := read(t, first.Path())
	want := "before\n" + reconnectMarker + "after\n"
	if got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestSuppressionDropsOutputAndMarksTheGap(t *testing.T) {
	r := open(t)
	h := r.HostLog("web-01")

	h.Write([]byte("visible\n"))
	r.SetSuppressed(true)
	h.Write([]byte("secret\n"))
	h.Write([]byte("more secret\n"))
	r.SetSuppressed(false)
	h.Write([]byte("visible again\n"))

	got := read(t, h.Path())
	want := "visible\n" + pausedMarker + resumedMarker + "visible again\n"
	if got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
	if strings.Contains(got, "secret") {
		t.Fatal("suppressed output reached the disk")
	}
}

func TestQuietHostGetsNoSuppressionMarkers(t *testing.T) {
	r := open(t)
	h := r.HostLog("quiet")

	h.Write([]byte("before\n"))
	r.SetSuppressed(true)
	// No output arrives during the gap.
	r.SetSuppressed(false)
	h.Write([]byte("after\n"))

	if got, want := read(t, h.Path()), "before\nafter\n"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestReconnectMarkerRespectsSuppression(t *testing.T) {
	r := open(t)
	h := r.HostLog("web-01")
	h.Write([]byte("before\n"))

	r.SetSuppressed(true)
	r.HostLog("web-01") // reconnect while paused
	r.SetSuppressed(false)

	if got := read(t, h.Path()); strings.Contains(got, strings.TrimSpace(reconnectMarker)) {
		t.Fatalf("reconnect marker punched through suppression: %q", got)
	}
}

func TestRotationKeepsOneOldFile(t *testing.T) {
	r := open(t)
	r.SetMaxFileSize(10)
	h := r.HostLog("web-01")

	h.Write([]byte("aaaaaaaa\n")) // 9 bytes, fits
	h.Write([]byte("bbbb\n"))     // would pass 10: rotates first
	h.Write([]byte("cccccccc\n")) // fills the fresh file
	h.Write([]byte("dddd\n"))     // rotates again, replacing the first rotation

	if got, want := read(t, h.Path()), "dddd\n"; got != want {
		t.Fatalf("current file = %q, want %q", got, want)
	}
	if got, want := read(t, h.Path()+".1"), "cccccccc\n"; got != want {
		t.Fatalf("rotated file = %q, want %q", got, want)
	}
}

func TestSanitizedFileNames(t *testing.T) {
	cases := map[string]string{
		"web-01":       "web-01",
		"../etc/cron":  "_.._etc_cron",
		".hidden":      "_.hidden",
		"host:22":      "host_22",
		"back\\slash":  "back_slash",
		"ctrl\x01byte": "ctrl_byte",
	}
	for id, want := range cases {
		if got := sanitize(id); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestCollidingNamesGetDistinctFiles(t *testing.T) {
	r := open(t)
	a := r.HostLog("host/1")
	b := r.HostLog("host_1")

	if a.Path() == b.Path() {
		t.Fatalf("both hosts write to %q", a.Path())
	}
	a.Write([]byte("a\n"))
	b.Write([]byte("b\n"))
	if got := read(t, b.Path()); got != "b\n" {
		t.Fatalf("second host's log = %q", got)
	}
}

func TestWriteNeverFailsAndCloseReportsTheLoss(t *testing.T) {
	root := t.TempDir()
	r, err := Open(root, start)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	r.SetMaxFileSize(4)
	h := r.HostLog("web-01")
	h.Write([]byte("one\n"))

	// Kill the directory behind the log's back, then force a rotation so the
	// next append has to rename and reopen, which fails: the write must still
	// report success, because the reader goroutine feeding it must not stall.
	if err := os.RemoveAll(r.Dir()); err != nil {
		t.Fatalf("remove run dir: %v", err)
	}
	if n, err := h.Write([]byte("lost bytes\n")); err != nil || n != 11 {
		t.Fatalf("Write after failure = (%d, %v), want (11, nil)", n, err)
	}

	if h.Err() == nil {
		t.Fatal("the loss is invisible: Err() is nil")
	}
	if err := r.Close(); err == nil {
		t.Fatal("Close reported a clean run after losing output")
	}
}

func TestCloseDropsFurtherWrites(t *testing.T) {
	r := open(t)
	h := r.HostLog("web-01")
	h.Write([]byte("kept\n"))
	if err := h.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := h.Write([]byte("dropped\n")); err != nil {
		t.Fatalf("Write after Close: %v", err)
	}
	if got := read(t, h.Path()); got != "kept\n" {
		t.Fatalf("log = %q", got)
	}
}
