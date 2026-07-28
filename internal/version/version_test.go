package version

import (
	"regexp"
	"testing"
)

// semver matches the subset of semantic versioning this project uses: three
// numeric components, no leading "v", no pre-release or build metadata.
var semver = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

func TestVersionIsSemver(t *testing.T) {
	if !semver.MatchString(Version) {
		t.Errorf("Version = %q, want a bare x.y.z semver without a leading v", Version)
	}
}

func TestFormat(t *testing.T) {
	tests := []struct {
		name     string
		rev      string
		modified bool
		ok       bool
		want     string
	}{
		{
			name: "no vcs stamp falls back to the bare version",
			ok:   false,
			want: Version,
		},
		{
			name: "empty revision falls back even when ok",
			rev:  "",
			ok:   true,
			want: Version,
		},
		{
			name: "long revision is shortened",
			rev:  "abc1234def5678",
			ok:   true,
			want: Version + " (abc1234)",
		},
		{
			name: "short revision is kept as is",
			rev:  "abc12",
			ok:   true,
			want: Version + " (abc12)",
		},
		{
			name: "revision of exactly the cut length is not truncated",
			rev:  "abc1234",
			ok:   true,
			want: Version + " (abc1234)",
		},
		{
			name:     "dirty tree is marked",
			rev:      "abc1234def5678",
			modified: true,
			ok:       true,
			want:     Version + " (abc1234-dirty)",
		},
		{
			name:     "dirty flag is ignored without a stamp",
			modified: true,
			ok:       false,
			want:     Version,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := format(tt.rev, tt.modified, tt.ok); got != tt.want {
				t.Errorf("format(%q, %v, %v) = %q, want %q", tt.rev, tt.modified, tt.ok, got, tt.want)
			}
		})
	}
}

// TestString only asserts the invariant that holds in every environment: the
// output always starts with the constant. Test binaries carry no VCS stamp, so
// the enriched form cannot be exercised here — that is what TestFormat covers.
func TestString(t *testing.T) {
	got := String()
	if len(got) < len(Version) || got[:len(Version)] != Version {
		t.Errorf("String() = %q, want it to start with %q", got, Version)
	}
}
