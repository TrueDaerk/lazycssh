package sessions

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

const fullFile = `version: 1
name: prod-web
description: the production web tier
defaults:
  user: deploy
  port: 2222
  identity_file: ~/.ssh/id_prod
  jump_host: bastion.example.com
hosts:
  - pattern: srv1-{01..40}.example.com
  - pattern: canary.example.com
    user: root
    port: 22
broadcast: selected
working_set: first 20
`

func TestDecodeFullFile(t *testing.T) {
	s, err := Decode(strings.NewReader(fullFile))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if s.Name != "prod-web" || s.Version != 1 {
		t.Fatalf("header = %d %q", s.Version, s.Name)
	}
	if len(s.Hosts) != 2 {
		t.Fatalf("hosts = %d, want 2", len(s.Hosts))
	}
	// Patterns stay unexpanded on disk, which is the point of storing them.
	if s.Hosts[0].Pattern != "srv1-{01..40}.example.com" {
		t.Fatalf("pattern = %q", s.Hosts[0].Pattern)
	}
	if got := s.Options(0); got.User != "deploy" || got.Port != 2222 ||
		got.IdentityFile != "~/.ssh/id_prod" || got.JumpHost != "bastion.example.com" {
		t.Fatalf("defaults did not fill in: %+v", got)
	}
	if got := s.Options(1); got.User != "root" || got.Port != 22 {
		t.Fatalf("entry did not override the defaults: %+v", got)
	}
	if got := s.Options(1).JumpHost; got != "bastion.example.com" {
		t.Fatalf("entry lost an unset default: %q", got)
	}

	mode, err := s.Mode()
	if err != nil || mode != broadcast.ModeSelected {
		t.Fatalf("Mode() = %v, %v", mode, err)
	}
	sel, err := s.Selector()
	if err != nil {
		t.Fatalf("Selector: %v", err)
	}
	if sel.Kind() != workingset.KindRange || sel.String() != "first-20" {
		t.Fatalf("Selector() = %v", sel)
	}

	count, err := s.HostCount()
	if err != nil {
		t.Fatalf("HostCount: %v", err)
	}
	if count != 41 {
		t.Fatalf("HostCount() = %d, want 41", count)
	}
}

func TestDefaultsWhenSessionIsSilent(t *testing.T) {
	s, err := Decode(strings.NewReader("version: 1\nname: a\nhosts:\n  - pattern: h1\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	mode, err := s.Mode()
	if err != nil || mode != broadcast.ModeAll {
		t.Fatalf("Mode() = %v, %v, want all", mode, err)
	}
	sel, err := s.Selector()
	if err != nil || sel != nil {
		t.Fatalf("Selector() = %v, %v, want nil", sel, err)
	}
	if got := s.Options(0); !got.empty() {
		t.Fatalf("Options() = %+v, want zero", got)
	}
	if got := s.Options(99); !got.empty() {
		t.Fatalf("Options(out of range) = %+v", got)
	}
}

// The acceptance criterion: load, save, load again produces an identical file.
func TestRoundTrip(t *testing.T) {
	for _, src := range []string{
		fullFile,
		"version: 1\nname: minimal\nhosts:\n    - pattern: h1\n",
	} {
		first, err := Decode(strings.NewReader(src))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}

		var buf bytes.Buffer
		if err := Encode(&buf, first); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		encoded := buf.String()

		second, err := Decode(strings.NewReader(encoded))
		if err != nil {
			t.Fatalf("Decode round two: %v\n%s", err, encoded)
		}

		var again bytes.Buffer
		if err := Encode(&again, second); err != nil {
			t.Fatalf("Encode round two: %v", err)
		}
		if again.String() != encoded {
			t.Fatalf("round trip changed the file:\nfirst:\n%s\nsecond:\n%s", encoded, again.String())
		}
	}
}

func TestEncodeOmitsAnEmptyDefaultsBlock(t *testing.T) {
	s := &Session{Version: 1, Name: "a", Hosts: []HostEntry{{Pattern: "h1"}}}
	var buf bytes.Buffer
	if err := Encode(&buf, s); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if strings.Contains(buf.String(), "defaults") {
		t.Fatalf("empty defaults block was written:\n%s", buf.String())
	}
}

// Unknown keys are an error so a typo cannot silently drop a host.
func TestDecodeRejectsUnknownKeys(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"top level typo", "version: 1\nname: a\nhostz:\n  - pattern: h1\n"},
		{"host entry typo", "version: 1\nname: a\nhosts:\n  - patern: h1\n"},
		{"defaults typo", "version: 1\nname: a\ndefaults:\n  usr: root\nhosts:\n  - pattern: h1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(tc.yaml)); err == nil {
				t.Fatal("Decode accepted an unknown key")
			}
		})
	}
}

// A session file never carries a credential, and saying so is a refusal with its
// own error rather than a generic parse failure.
func TestDecodeRejectsInlineCredentials(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"top level password", "version: 1\nname: a\npassword: hunter2\nhosts:\n  - pattern: h1\n"},
		{"per host password", "version: 1\nname: a\nhosts:\n  - pattern: h1\n    password: hunter2\n"},
		{"passphrase", "version: 1\nname: a\nhosts:\n  - pattern: h1\n    passphrase: hunter2\n"},
		{"uppercase key", "version: 1\nname: a\nSecret: hunter2\nhosts:\n  - pattern: h1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tc.yaml))
			if !errors.Is(err, ErrInlinePassword) {
				t.Fatalf("Decode error = %v, want ErrInlinePassword", err)
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Fatalf("the error quoted the secret: %v", err)
			}
		})
	}
}

func TestDecodeErrors(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"empty file", "", "empty"},
		{"not yaml", "\tthis: is not yaml\n", "parse"},
		{"missing version", "name: a\nhosts:\n  - pattern: h1\n", "missing version"},
		{"future version", "version: 99\nname: a\nhosts:\n  - pattern: h1\n", "newer than this build"},
		{"negative version", "version: -1\nname: a\nhosts:\n  - pattern: h1\n", "invalid version"},
		{"no name", "version: 1\nhosts:\n  - pattern: h1\n", "empty name"},
		{"traversing name", "version: 1\nname: ../evil\nhosts:\n  - pattern: h1\n", "may not start with a dot"},
		{"bad name", "version: 1\nname: web*\nhosts:\n  - pattern: h1\n", "not allowed"},
		{"no hosts", "version: 1\nname: a\nhosts: []\n", "no hosts"},
		{"empty pattern", "version: 1\nname: a\nhosts:\n  - pattern: \"\"\n", "empty pattern"},
		{"malformed brace", "version: 1\nname: a\nhosts:\n  - pattern: \"web-{01..\"\n", "web-{01.."},
		{"bad port", "version: 1\nname: a\nhosts:\n  - pattern: h1\n    port: 99999\n", "invalid port"},
		{"bad default port", "version: 1\nname: a\ndefaults:\n  port: -2\nhosts:\n  - pattern: h1\n", "invalid default port"},
		{"bad broadcast", "version: 1\nname: a\nbroadcast: everything\nhosts:\n  - pattern: h1\n", "unknown mode"},
		{"bad working set", "version: 1\nname: a\nworking_set: 40-21\nhosts:\n  - pattern: h1\n", "working_set"},
		{"working set needs a live selection", "version: 1\nname: a\nworking_set: selection\nhosts:\n  - pattern: h1\n", "nothing is selected"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Decode(strings.NewReader(tc.yaml))
			if err == nil {
				t.Fatalf("Decode accepted %q", tc.yaml)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestEncodeRejectsInvalidSessions(t *testing.T) {
	var buf bytes.Buffer
	if err := Encode(&buf, nil); err == nil {
		t.Fatal("Encode accepted a nil session")
	}
	if err := Encode(&buf, &Session{Version: 1, Name: "a"}); err == nil {
		t.Fatal("Encode accepted a session with no hosts")
	}
	if buf.Len() != 0 {
		t.Fatalf("a rejected session was partly written: %q", buf.String())
	}
}

func TestPatterns(t *testing.T) {
	s := &Session{Hosts: []HostEntry{{Pattern: "a"}, {Pattern: "b"}}}
	if got := strings.Join(s.Patterns(), ","); got != "a,b" {
		t.Fatalf("Patterns() = %q", got)
	}
}

func TestHostCountReportsMalformedPatterns(t *testing.T) {
	s := &Session{Version: 1, Name: "a", Hosts: []HostEntry{{Pattern: "web-{01.."}}}
	if _, err := s.HostCount(); err == nil {
		t.Fatal("HostCount accepted a malformed pattern")
	}
}

// A session says *how* to authenticate - an argv that prints the credential -
// never the credential itself.
func TestSecretCommand(t *testing.T) {
	const src = `version: 1
name: prod
defaults:
  secret_command: [pass, show, prod/deploy]
hosts:
  - pattern: h1
  - pattern: h2
    secret_command: [op, read, "op://vault/h2/password"]
`
	s, err := Decode(strings.NewReader(src))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got := s.SecretCommand(0).String(); got != "pass show prod/deploy" {
		t.Fatalf("host 0 command = %q", got)
	}
	if got := s.SecretCommand(1).String(); got != "op read op://vault/h2/password" {
		t.Fatalf("host 1 command = %q", got)
	}

	var buf bytes.Buffer
	if err := Encode(&buf, s); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !strings.Contains(buf.String(), "secret_command") {
		t.Fatalf("secret_command was dropped on write:\n%s", buf.String())
	}
}

func TestSecretCommandWithoutOne(t *testing.T) {
	s, err := Decode(strings.NewReader("version: 1\nname: a\nhosts:\n  - pattern: h1\n"))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if !s.SecretCommand(0).Empty() {
		t.Fatalf("SecretCommand() = %q, want none", s.SecretCommand(0))
	}
}

func TestValidateRejectsAMalformedSecretCommand(t *testing.T) {
	tests := []struct {
		name string
		yaml string
	}{
		{"blank program", "version: 1\nname: a\nhosts:\n  - pattern: h1\n    secret_command: [\"  \"]\n"},
		{"empty argument", "version: 1\nname: a\nhosts:\n  - pattern: h1\n    secret_command: [pass, \"\"]\n"},
		{"blank default program", "version: 1\nname: a\ndefaults:\n  secret_command: [\"\"]\nhosts:\n  - pattern: h1\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(tc.yaml)); err == nil {
				t.Fatal("Decode accepted a malformed secret command")
			}
		})
	}
}
