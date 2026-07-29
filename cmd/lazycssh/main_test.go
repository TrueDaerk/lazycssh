package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/program"
	"github.com/TrueDaerk/lazycssh/internal/version"
)

// captureLaunch records the program config run hands over instead of starting
// a TUI, which tests cannot do: there is no terminal here.
func captureLaunch(got *program.Config) func(program.Config) error {
	return func(cfg program.Config) error {
		*got = cfg
		return nil
	}
}

// noLaunch fails the test if the TUI would have started.
func noLaunch(t *testing.T) func(program.Config) error {
	return func(program.Config) error {
		t.Error("the TUI was launched, want it not to be")
		return nil
	}
}

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout string   // exact match when non-empty
		wantStderr []string // substrings that must be present
		emptyErr   bool     // stderr must be empty
	}{
		{
			name:       "version flag prints the version and exits cleanly",
			args:       []string{"--version"},
			wantCode:   exitOK,
			wantStdout: version.String() + "\n",
			emptyErr:   true,
		},
		{
			name:       "single dash version behaves the same",
			args:       []string{"-version"},
			wantCode:   exitOK,
			wantStdout: version.String() + "\n",
			emptyErr:   true,
		},
		{
			name:     "no arguments starts the TUI with an empty run",
			args:     nil,
			wantCode: exitOK,
			emptyErr: true,
		},
		{
			name:       "unknown flag fails with the usage code",
			args:       []string{"--nope"},
			wantCode:   exitUsage,
			wantStderr: []string{"nope"},
		},
		{
			name:       "the insecure flag warns loudly before anything else",
			args:       []string{"--insecure-ignore-host-key", "a.example.com"},
			wantCode:   exitOK,
			wantStderr: []string{"WARNING", "insecure-ignore-host-key", "machine-in-the-middle"},
		},
		{
			name:     "host key verification is not weakened without the flag",
			args:     []string{"a.example.com"},
			wantCode: exitOK,
		},
		{
			name:       "version wins over host arguments",
			args:       []string{"--version", "a.example.com"},
			wantCode:   exitOK,
			wantStdout: version.String() + "\n",
			emptyErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			var launched program.Config
			code := run(tt.args, &stdout, &stderr, captureLaunch(&launched))

			if code != tt.wantCode {
				t.Errorf("run(%q) = %d, want %d (stderr: %q)", tt.args, code, tt.wantCode, stderr.String())
			}
			if tt.wantStdout != "" && stdout.String() != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.wantStdout)
			}
			if tt.emptyErr && stderr.Len() != 0 {
				t.Errorf("stderr = %q, want it empty", stderr.String())
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr.String(), want)
				}
			}
			if tt.name == "host key verification is not weakened without the flag" &&
				strings.Contains(stderr.String(), "WARNING") {
				t.Error("a run without the flag printed the insecure warning")
			}
		})
	}
}

// TestRunWritesNothingToStdoutOnFailure guards the contract that machine
// readable output never mixes with diagnostics: anything a user might pipe goes
// to stdout, everything else to stderr.
func TestRunLaunchesTheTUIWithTheResolvedPlan(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var launched program.Config
	code := run([]string{"--insecure-ignore-host-key", "a.example.com", "b.example.com"},
		&stdout, &stderr, captureLaunch(&launched))

	if code != exitOK {
		t.Fatalf("run = %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := len(launched.Patterns); got != 2 {
		t.Errorf("the TUI was launched with %d patterns, want 2", got)
	}
	if !launched.Insecure {
		t.Error("the insecure flag did not reach the program config")
	}
	if launched.Store == nil {
		t.Error("the TUI was launched without a session store")
	}
}

func TestRunWithoutArgumentsLaunchesAnEmptyRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var launched program.Config
	code := run(nil, &stdout, &stderr, captureLaunch(&launched))

	if code != exitOK {
		t.Fatalf("run = %d, want %d (stderr %q)", code, exitOK, stderr.String())
	}
	if got := len(launched.Patterns); got != 0 {
		t.Errorf("the TUI was launched with %d patterns, want none", got)
	}
	if launched.Store == nil {
		t.Error("an empty run was launched without a session store; there is nothing to pick from")
	}
}

func TestRunReportsALaunchFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"a.example.com"}, &stdout, &stderr,
		func(program.Config) error { return errors.New("the terminal went away") })

	if code != exitError {
		t.Fatalf("run = %d, want %d", code, exitError)
	}
	if !strings.Contains(stderr.String(), "the terminal went away") {
		t.Errorf("stderr = %q, want the launch error in it", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want nothing", stdout.String())
	}
}

func TestRunWritesNothingToStdoutOnFailure(t *testing.T) {
	for _, args := range [][]string{{"--nope"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr, noLaunch(t)); code == exitOK {
			t.Fatalf("run(%q) unexpectedly succeeded", args)
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%q) wrote %q to stdout, want nothing", args, stdout.String())
		}
	}
}
