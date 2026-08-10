// Package version holds the single source of truth for the lazycssh version.
//
// The [Version] constant is authoritative. The git tag on main must match it
// after a merge; see wiki/contributing/versioning.md.
package version

import "runtime/debug"

// Version is the semantic version of this build, without a leading "v".
const Version = "0.10.35"

// revisionLength is the number of hex characters of the VCS revision kept in
// [String]. Seven matches the short hash git itself prints.
const revisionLength = 7

// String returns the version, enriched with VCS information when the binary was
// built from a git checkout. It renders as "0.1.0", "0.1.0 (abc1234)" or
// "0.1.0 (abc1234-dirty)".
func String() string {
	return format(vcsInfo())
}

// format renders the version string from raw VCS data. It is separate from
// [vcsInfo] so the rendering can be tested without a build stamp, which test
// binaries do not carry.
func format(rev string, modified, ok bool) string {
	if !ok || rev == "" {
		return Version
	}
	if len(rev) > revisionLength {
		rev = rev[:revisionLength]
	}
	if modified {
		return Version + " (" + rev + "-dirty)"
	}
	return Version + " (" + rev + ")"
}

// vcsInfo extracts the revision and the dirty flag from the build info embedded
// by the Go toolchain. ok is false when the binary carries no VCS stamp, which
// is the case for test binaries and for builds from a source tarball.
func vcsInfo() (rev string, modified, ok bool) {
	info, available := debug.ReadBuildInfo()
	if !available {
		return "", false, false
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if rev == "" {
		return "", false, false
	}
	return rev, modified, true
}
