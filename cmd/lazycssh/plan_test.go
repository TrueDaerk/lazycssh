package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/program"
	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// sessionStore builds a store in a temporary directory holding the given
// sessions.
func sessionStore(t *testing.T, saved ...*sessions.Session) *sessions.Store {
	t.Helper()
	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, s := range saved {
		if err := store.Save(s); err != nil {
			t.Fatalf("Save %s: %v", s.Name, err)
		}
	}
	return store
}

func session(name string, patterns ...string) *sessions.Session {
	s := &sessions.Session{Version: sessions.FormatVersion, Name: name}
	for _, p := range patterns {
		s.Hosts = append(s.Hosts, sessions.HostEntry{Pattern: p})
	}
	return s
}

func TestResolvePlanHostsOnly(t *testing.T) {
	store := sessionStore(t)
	got, err := resolvePlan([]string{"a.example.com", "b.example.com"}, store)
	if err != nil {
		t.Fatalf("resolvePlan: %v", err)
	}
	if strings.Join(got.Patterns, ",") != "a.example.com,b.example.com" {
		t.Fatalf("Patterns = %v", got.Patterns)
	}
	if len(got.Sessions) != 0 {
		t.Fatalf("Sessions = %v", got.SessionNames())
	}
	if got.Broadcast != broadcast.ModeAll || got.WorkingSet != nil {
		t.Fatalf("defaults changed: %v %v", got.Broadcast, got.WorkingSet)
	}
}

// The acceptance criterion: `lazycssh @name extra-host.example.com` connects to
// both, in the order they were given.
func TestResolvePlanMergesExtraHosts(t *testing.T) {
	store := sessionStore(t, session("prod-web", "srv1-{01..04}.example.com"))

	got, err := resolvePlan([]string{"@prod-web", "extra.example.com"}, store)
	if err != nil {
		t.Fatalf("resolvePlan: %v", err)
	}
	if strings.Join(got.Patterns, ",") != "srv1-{01..04}.example.com,extra.example.com" {
		t.Fatalf("Patterns = %v", got.Patterns)
	}
	if strings.Join(got.SessionNames(), ",") != "prod-web" {
		t.Fatalf("SessionNames() = %v", got.SessionNames())
	}

	// The order of the arguments is the order of the panes.
	got, err = resolvePlan([]string{"first.example.com", "@prod-web"}, store)
	if err != nil {
		t.Fatalf("resolvePlan: %v", err)
	}
	if got.Patterns[0] != "first.example.com" {
		t.Fatalf("argument order was not preserved: %v", got.Patterns)
	}
}

func TestResolvePlanAppliesSessionSettings(t *testing.T) {
	s := session("prod", "h1")
	s.Broadcast = "selected"
	s.WorkingSet = "first 20"
	store := sessionStore(t, s)

	got, err := resolvePlan([]string{"@prod"}, store)
	if err != nil {
		t.Fatalf("resolvePlan: %v", err)
	}
	if got.Broadcast != broadcast.ModeSelected {
		t.Fatalf("Broadcast = %v", got.Broadcast)
	}
	if got.WorkingSet == nil || got.WorkingSet.Kind() != workingset.KindRange ||
		got.WorkingSet.String() != "first-20" {
		t.Fatalf("WorkingSet = %v", got.WorkingSet)
	}
}

// The last session named is the one the user meant.
func TestResolvePlanLaterSessionWins(t *testing.T) {
	first := session("a", "h1")
	first.Broadcast = "selected"
	first.WorkingSet = "first 5"
	second := session("b", "h2")
	second.Broadcast = "single"
	store := sessionStore(t, first, second)

	got, err := resolvePlan([]string{"@a", "@b"}, store)
	if err != nil {
		t.Fatalf("resolvePlan: %v", err)
	}
	if strings.Join(got.Patterns, ",") != "h1,h2" {
		t.Fatalf("Patterns = %v", got.Patterns)
	}
	if got.Broadcast != broadcast.ModeSingle {
		t.Fatalf("Broadcast = %v, want the later session's", got.Broadcast)
	}
	// The later session sets no working set, so the earlier one's survives
	// rather than being silently cleared.
	if got.WorkingSet == nil || got.WorkingSet.String() != "first-5" {
		t.Fatalf("WorkingSet = %v", got.WorkingSet)
	}
}

// A missing session name lists what is available: a user who mistypes wants the
// list, not a second guess.
func TestResolvePlanUnknownSessionListsTheOthers(t *testing.T) {
	store := sessionStore(t, session("prod", "h1"), session("staging", "h2"))

	_, err := resolvePlan([]string{"@prd"}, store)
	if err == nil {
		t.Fatal("resolvePlan accepted an unknown session")
	}
	for _, want := range []string{"prd", "not found", "prod", "staging"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not mention %q", err, want)
		}
	}
}

func TestResolvePlanUnknownSessionWithNoneSaved(t *testing.T) {
	store := sessionStore(t)
	_, err := resolvePlan([]string{"@prod"}, store)
	if err == nil {
		t.Fatal("resolvePlan accepted an unknown session")
	}
	if !strings.Contains(err.Error(), "no sessions are saved") ||
		!strings.Contains(err.Error(), store.Dir()) {
		t.Fatalf("error = %q", err)
	}
}

func TestResolvePlanReportsAnUnreadableSession(t *testing.T) {
	store := sessionStore(t)
	path, err := store.Path("broken")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if err := writeFile(path, "version: 1\nname: broken\nhostz: []\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := resolvePlan([]string{"@broken"}, store); err == nil {
		t.Fatal("resolvePlan accepted an unreadable session file")
	}
}

func TestResolvePlanWithoutAStore(t *testing.T) {
	if _, err := resolvePlan([]string{"@prod"}, nil); err == nil {
		t.Fatal("resolvePlan accepted a session name with no store")
	}
	// Plain hosts still work without a store.
	got, err := resolvePlan([]string{"a.example.com"}, nil)
	if err != nil {
		t.Fatalf("resolvePlan: %v", err)
	}
	if strings.Join(got.Patterns, ",") != "a.example.com" {
		t.Fatalf("Patterns = %v", got.Patterns)
	}
}

func TestListSessionsFlag(t *testing.T) {
	store := sessionStore(t)
	s := session("prod", "srv1-{01..04}.example.com")
	s.Description = "the production web tier"
	if err := store.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--sessions-dir", store.Dir(), "--list-sessions"}, &stdout, &stderr, noLaunch(t)); code != exitOK {
		t.Fatalf("run = %d (stderr %q)", code, stderr.String())
	}
	for _, want := range []string{"prod", "4 hosts", "the production web tier"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout %q does not contain %q", stdout.String(), want)
		}
	}
}

func TestListSessionsWithNoneSaved(t *testing.T) {
	store := sessionStore(t)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--sessions-dir", store.Dir(), "--list-sessions"}, &stdout, &stderr, noLaunch(t)); code != exitOK {
		t.Fatalf("run = %d", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
	if !strings.Contains(stderr.String(), "no sessions are saved") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunLaunchesASession(t *testing.T) {
	store := sessionStore(t, session("prod", "srv1-{01..04}.example.com"))

	var stdout, stderr bytes.Buffer
	var launched program.Config
	code := run([]string{"--sessions-dir", store.Dir(), "@prod", "extra.example.com"},
		&stdout, &stderr, captureLaunch(&launched))

	if code != exitOK {
		t.Fatalf("run = %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := len(launched.Patterns); got != 2 {
		t.Fatalf("the TUI was launched with %d patterns, want the session's plus the extra one", got)
	}
	if launched.SessionName != "prod" {
		t.Fatalf("SessionName = %q, want prod", launched.SessionName)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
}

func TestRunReportsAnUnknownSession(t *testing.T) {
	store := sessionStore(t, session("prod", "h1"))

	var stdout, stderr bytes.Buffer
	code := run([]string{"--sessions-dir", store.Dir(), "@nope"}, &stdout, &stderr, noLaunch(t))

	if code != exitError {
		t.Fatalf("run = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "available sessions: prod") {
		t.Fatalf("stderr = %q", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want nothing", stdout.String())
	}
}

// writeFile is a small helper for the tests that need a file the session store
// itself would never write.
func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}
