package sessions

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

func sampleRun(name string) Run {
	return Run{
		Name:     name,
		Patterns: []string{"srv1-{01..04}.example.com", "canary.example.com"},
		Defaults: HostOptions{User: "deploy", IdentityFile: "~/.ssh/id_prod"},
		Overrides: map[string]HostOptions{
			"canary.example.com": {User: "root", Port: 2222},
		},
		Broadcast:  broadcast.ModeSelected,
		WorkingSet: workingset.Range{From: 1, To: 2},
	}
}

// The acceptance criterion: saving writes a file that `lazycssh @name`
// reproduces.
func TestFromRunRoundTripsThroughTheStore(t *testing.T) {
	store := store(t)
	run := sampleRun("prod-web")

	saved, err := store.SaveRun(run, false)
	if err != nil {
		t.Fatalf("SaveRun: %v", err)
	}
	if saved.Version != FormatVersion {
		t.Fatalf("Version = %d", saved.Version)
	}

	got, err := store.Load("prod-web")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Patterns are saved as typed, not as the hosts they expanded to.
	if strings.Join(got.Patterns(), ",") != strings.Join(run.Patterns, ",") {
		t.Fatalf("patterns = %v", got.Patterns())
	}
	if opts := got.Options(0); opts.User != "deploy" || opts.IdentityFile != "~/.ssh/id_prod" {
		t.Fatalf("host 0 options = %+v", opts)
	}
	if opts := got.Options(1); opts.User != "root" || opts.Port != 2222 {
		t.Fatalf("host 1 options = %+v", opts)
	}
	mode, err := got.Mode()
	if err != nil || mode != broadcast.ModeSelected {
		t.Fatalf("Mode() = %v, %v", mode, err)
	}
	sel, err := got.Selector()
	if err != nil || sel == nil || sel.String() != "first-2" {
		t.Fatalf("Selector() = %v, %v", sel, err)
	}
}

func TestFromRunOmitsWhatMatchesTheDefaults(t *testing.T) {
	run := Run{
		Name:     "a",
		Patterns: []string{"h1", "h2"},
		Defaults: HostOptions{User: "deploy", SecretCommand: []string{"pass", "show", "x"}},
		Overrides: map[string]HostOptions{
			"h1": {User: "deploy", SecretCommand: []string{"pass", "show", "x"}},
			"h2": {User: "root"},
		},
	}

	s, err := FromRun(run)
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	if !s.Hosts[0].HostOptions.empty() {
		t.Fatalf("host 0 restated the defaults: %+v", s.Hosts[0].HostOptions)
	}
	if s.Hosts[1].User != "root" {
		t.Fatalf("host 1 lost its override: %+v", s.Hosts[1].HostOptions)
	}
	// The override still resolves to the default it did not restate.
	if got := s.Options(1).SecretCommand; strings.Join(got, " ") != "pass show x" {
		t.Fatalf("host 1 secret command = %v", got)
	}
}

func TestFromRunDefaultsAreOmittedFromTheFile(t *testing.T) {
	s, err := FromRun(Run{Name: "a", Patterns: []string{"h1"}, Broadcast: broadcast.ModeAll})
	if err != nil {
		t.Fatalf("FromRun: %v", err)
	}
	if s.Broadcast != "" {
		t.Fatalf("Broadcast = %q, want it left out", s.Broadcast)
	}

	var buf bytes.Buffer
	if err := Encode(&buf, s); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, unwanted := range []string{"broadcast", "working_set", "defaults", "description"} {
		if strings.Contains(buf.String(), unwanted) {
			t.Fatalf("%q was written for a run that did not set it:\n%s", unwanted, buf.String())
		}
	}
}

// A manual working set is a list of identifiers from this run; restoring it
// against a different fleet would mean nothing.
func TestFromRunDropsAManualWorkingSet(t *testing.T) {
	tests := []struct {
		name string
		sel  workingset.Selector
		want string
	}{
		{"manual", workingset.NewManual([]string{"web-01"}), ""},
		{"all", workingset.All{}, ""},
		{"nil", nil, ""},
		{"range", workingset.Range{From: 21, To: 40}, "21-40"},
		{"pattern", workingset.Pattern{Glob: "web-*"}, "web-*"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, err := FromRun(Run{Name: "a", Patterns: []string{"h1"}, WorkingSet: tc.sel})
			if err != nil {
				t.Fatalf("FromRun: %v", err)
			}
			if s.WorkingSet != tc.want {
				t.Fatalf("WorkingSet = %q, want %q", s.WorkingSet, tc.want)
			}
		})
	}
}

func TestFromRunErrors(t *testing.T) {
	tests := []struct {
		name string
		run  Run
	}{
		{"no name", Run{Patterns: []string{"h1"}}},
		{"bad name", Run{Name: "../evil", Patterns: []string{"h1"}}},
		{"no hosts", Run{Name: "a"}},
		{"blank pattern", Run{Name: "a", Patterns: []string{"  "}}},
		{"malformed pattern", Run{Name: "a", Patterns: []string{"web-{01.."}}},
		{"invalid mode", Run{Name: "a", Patterns: []string{"h1"}, Broadcast: broadcast.Mode(42)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FromRun(tc.run); err == nil {
				t.Fatal("FromRun accepted an unsaveable run")
			}
		})
	}
}

// The acceptance criterion: overwriting asks first. The store refuses, and the
// caller is the one that can ask.
func TestCreateRefusesToOverwrite(t *testing.T) {
	store := store(t)
	if _, err := store.SaveRun(sampleRun("prod"), false); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	second := sampleRun("prod")
	second.Description = "replaced"
	_, err := store.SaveRun(second, false)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("SaveRun error = %v, want ErrExists", err)
	}

	got, err := store.Load("prod")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Description == "replaced" {
		t.Fatal("the refused save overwrote the file anyway")
	}

	if _, err := store.SaveRun(second, true); err != nil {
		t.Fatalf("SaveRun with overwrite: %v", err)
	}
	got, err = store.Load("prod")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Description != "replaced" {
		t.Fatalf("Description = %q after an approved overwrite", got.Description)
	}
}

func TestCreateRejectsInvalidInput(t *testing.T) {
	store := store(t)
	if err := store.Create(nil); err == nil {
		t.Fatal("Create accepted nil")
	}
	if err := store.Create(&Session{Name: "../evil"}); err == nil {
		t.Fatal("Create accepted a name that escapes the directory")
	}
}

func TestSaveRunReportsBuildErrors(t *testing.T) {
	store := store(t)
	if _, err := store.SaveRun(Run{Name: "a"}, true); err == nil {
		t.Fatal("SaveRun accepted a run with no hosts")
	}
}

// A saved file is written by us, so it is the one place where a leaked
// credential would persist across runs.
func TestSavedFileHoldsNoCredential(t *testing.T) {
	const password = "hunter2-should-never-be-written"

	store := store(t)
	run := sampleRun("prod")
	run.Description = "saved by hand"
	run.Defaults.SecretCommand = []string{"pass", "show", "prod/deploy"}
	if _, err := store.SaveRun(run, true); err != nil {
		t.Fatalf("SaveRun: %v", err)
	}

	path, err := store.Path("prod")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(body), password) {
		t.Fatalf("the saved file holds a credential:\n%s", body)
	}
	for _, key := range passwordKeys {
		if strings.Contains(string(body), key+":") {
			t.Fatalf("the saved file holds a %q key:\n%s", key, body)
		}
	}
	// What it does hold is the reference: how to get the credential.
	if !strings.Contains(string(body), "secret_command") {
		t.Fatalf("the saved file lost the credential reference:\n%s", body)
	}
}

func TestDescribeRun(t *testing.T) {
	if got := DescribeRun(Run{Patterns: []string{"h1"}}); !strings.HasPrefix(got, "1 host pattern") {
		t.Fatalf("DescribeRun() = %q", got)
	}
	if got := DescribeRun(Run{Patterns: []string{"h1", "h2"}}); !strings.HasPrefix(got, "2 host patterns") {
		t.Fatalf("DescribeRun() = %q", got)
	}
}
