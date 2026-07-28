package hosts

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// TestExpandMatchesBash checks the parity claim directly instead of trusting the
// hand-written expectations elsewhere in this package.
//
// It is skipped unless bash 4 or newer is on PATH: bash 3.2, which is what macOS
// still ships, supports neither range steps nor zero padding, so comparing
// against it would assert the wrong thing. CI runs on Linux with bash 5, so the
// parity claim is verified there on every pull request.
//
// Only well-formed patterns are compared. Where a pattern is malformed lazycssh
// deliberately errors while bash leaves it as a literal; that divergence is
// covered by TestExpandRejectsMalformedPatterns.
func TestExpandMatchesBash(t *testing.T) {
	bash := requireBash(t, 4)

	patterns := []string{
		"srv1-{01..40}.example.com",
		"{web,db}-{1..3}.example.com",
		"srv-{a,b,c}",
		"srv{0..20..5}",
		"srv{1..10..4}",
		"srv{9..1..4}",
		"srv{3..1}",
		"srv{08..100}",
		"srv{8..11}",
		"n{-2..1}",
		"srv-{a..e}",
		"srv-{e..a..2}",
		"{a,b}{1,2}{x,y}",
		"srv-{a,{b,c}}",
		"{web-{1..2},db}",
		"{a,{b,{c,d}}}",
		"srv{,-backup}",
		"srv{7..7}",
		"{a,a}",
		"root@srv{1..2}.example.com:2222",
	}

	for _, pattern := range patterns {
		t.Run(pattern, func(t *testing.T) {
			want := bashExpand(t, bash, pattern)

			got, err := Expand(pattern)
			if err != nil {
				t.Fatalf("Expand(%q) returned error: %v", pattern, err)
			}
			assertEqual(t, pattern, got, want)
		})
	}
}

// requireBash returns the path to a bash of at least the given major version, or
// skips the test.
func requireBash(t *testing.T, minMajor int) string {
	t.Helper()

	path, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not found on PATH; skipping the bash parity check")
	}

	out, err := exec.Command(path, "-c", "echo $BASH_VERSINFO").Output()
	if err != nil {
		t.Skipf("cannot determine the bash version: %v", err)
	}
	major, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		t.Skipf("cannot parse the bash version %q: %v", out, err)
	}
	if major < minMajor {
		t.Skipf("bash %d is too old for range steps and zero padding; need %d or newer", major, minMajor)
	}
	return path
}

// bashExpand asks bash itself what a pattern expands to, one result per line so
// results containing spaces cannot be misread.
func bashExpand(t *testing.T, bash, pattern string) []string {
	t.Helper()

	out, err := exec.Command(bash, "-c", "printf '%s\\n' "+pattern).Output()
	if err != nil {
		t.Fatalf("bash failed to expand %q: %v", pattern, err)
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}
