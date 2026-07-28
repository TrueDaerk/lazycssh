package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/version"
)

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
			name:       "no arguments prints usage and fails with the usage code",
			args:       nil,
			wantCode:   exitUsage,
			wantStderr: []string{"Usage:", "lazycssh"},
		},
		{
			name:       "unknown flag fails with the usage code",
			args:       []string{"--nope"},
			wantCode:   exitUsage,
			wantStderr: []string{"nope"},
		},
		{
			name:       "hosts are accepted but connecting is not implemented",
			args:       []string{"a.example.com", "b.example.com"},
			wantCode:   exitError,
			wantStderr: []string{"not implemented", "2 host arguments"},
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

			code := run(tt.args, &stdout, &stderr)

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
		})
	}
}

// TestRunWritesNothingToStdoutOnFailure guards the contract that machine
// readable output never mixes with diagnostics: anything a user might pipe goes
// to stdout, everything else to stderr.
func TestRunWritesNothingToStdoutOnFailure(t *testing.T) {
	for _, args := range [][]string{nil, {"--nope"}, {"a.example.com"}} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code == exitOK {
			t.Fatalf("run(%q) unexpectedly succeeded", args)
		}
		if stdout.Len() != 0 {
			t.Errorf("run(%q) wrote %q to stdout, want nothing", args, stdout.String())
		}
	}
}
