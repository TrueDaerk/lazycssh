// Package sessions is the on-disk format for a saved run: which hosts, how to
// reach them, and what the run starts out looking like.
//
// The security rule from the root CLAUDE.md applies to everything here: a file
// this package writes never contains a password or a passphrase. Credentials are
// referenced - an agent, an identity file - never embedded.
package sessions

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/hosts"
	"github.com/TrueDaerk/lazycssh/internal/secret"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// FormatVersion is the schema version this build writes. A file carrying a
// higher version is rejected rather than half-understood.
const FormatVersion = 1

// Session is one saved session config.
//
// Host patterns are stored unexpanded, so `srv1-{01..40}.example.com` stays
// readable and stays correct when the fleet grows.
type Session struct {
	// Version is the schema version; see [FormatVersion].
	Version int `yaml:"version"`
	// Name is the identifier `lazycssh @name` takes.
	Name string `yaml:"name"`
	// Description is a free-text note shown in the Sessions panel.
	Description string `yaml:"description,omitempty"`
	// Defaults apply to every host entry that does not override them.
	Defaults HostOptions `yaml:"defaults,omitempty"`
	// Hosts are the targets, in the order they should appear.
	Hosts []HostEntry `yaml:"hosts"`
	// Broadcast is the mode the run starts in; empty means "all".
	Broadcast string `yaml:"broadcast,omitempty"`
	// WorkingSet is the working set definition the run starts with, in the
	// syntax of workingset.ParseSelector - "first 20", "21-40", "web-*".
	WorkingSet string `yaml:"working_set,omitempty"`
}

// HostOptions is how to connect, either as a default or for one entry.
type HostOptions struct {
	// User is the login user.
	User string `yaml:"user,omitempty"`
	// Port is the SSH port; zero means the ssh_config or protocol default.
	Port int `yaml:"port,omitempty"`
	// IdentityFile is a path to a private key. The key itself is never copied
	// into the session file.
	IdentityFile string `yaml:"identity_file,omitempty"`
	// JumpHost is a ProxyJump target.
	JumpHost string `yaml:"jump_host,omitempty"`
	// SecretCommand is a program and its arguments that print the login
	// credential on standard output - `pass show prod/deploy`, `op read ...`.
	// It is how a session says *how* to authenticate without storing the
	// secret. It is an argv, never a shell line, so a password entry with
	// shell metacharacters in its name cannot become a command.
	SecretCommand []string `yaml:"secret_command,omitempty"`
}

// empty reports whether no option is set, so an all-zero defaults block can be
// omitted from the written file.
func (o HostOptions) empty() bool {
	return o.User == "" && o.Port == 0 && o.IdentityFile == "" &&
		o.JumpHost == "" && len(o.SecretCommand) == 0
}

// HostEntry is one host pattern plus its overrides.
type HostEntry struct {
	// Pattern is a host argument, unexpanded: brace patterns are kept as typed.
	Pattern string `yaml:"pattern"`
	// HostOptions override the session defaults for this entry.
	HostOptions `yaml:",inline"`
}

// ErrInlinePassword is returned when a session file carries a credential. It is
// a distinct error because it is a refusal, not a parse failure: lazycssh does
// not read passwords from disk, and downgrading this to a warning would make the
// promise in the security constraints untrue.
var ErrInlinePassword = errors.New("session: passwords are never read from a session file")

// passwordKeys are the top-level and per-host keys that would carry a secret.
// They are rejected explicitly rather than by the unknown-key check, so the
// error says why instead of "unknown field".
var passwordKeys = []string{"password", "passphrase", "secret", "key_passphrase"}

// Decode reads a session from YAML. Unknown keys are an error: a typo in
// `pattern` would otherwise silently drop a host from the run.
func Decode(r io.Reader) (*Session, error) {
	// The document is read twice: once as a node tree, to refuse credential
	// keys with an error that says why, and once strictly into the struct.
	// yaml.Node.Decode does not honour KnownFields, so the strict pass has to
	// go through a decoder of its own.
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("session: read: %w", err)
	}

	var raw yaml.Node
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("session: parse: %w", err)
	}
	if raw.Kind == 0 {
		return nil, fmt.Errorf("session: file is empty")
	}
	if err := rejectPasswordKeys(&raw); err != nil {
		return nil, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var s Session
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("session: file is empty")
		}
		return nil, fmt.Errorf("session: parse: %w", err)
	}
	if err := s.Validate(); err != nil {
		return nil, err
	}
	return &s, nil
}

// rejectPasswordKeys walks the document for credential-looking keys at any
// depth. A user who writes one gets an explicit refusal rather than a file that
// looks like it works.
func rejectPasswordKeys(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := strings.ToLower(n.Content[i].Value)
			for _, bad := range passwordKeys {
				if key == bad {
					return fmt.Errorf("session: line %d: %q: %w",
						n.Content[i].Line, n.Content[i].Value, ErrInlinePassword)
				}
			}
		}
	}
	for _, child := range n.Content {
		if err := rejectPasswordKeys(child); err != nil {
			return err
		}
	}
	return nil
}

// Encode writes a session as YAML.
func Encode(w io.Writer, s *Session) error {
	if s == nil {
		return fmt.Errorf("session: nil session")
	}
	if err := s.Validate(); err != nil {
		return err
	}

	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(s); err != nil {
		return fmt.Errorf("session %s: encode: %w", s.Name, err)
	}
	return enc.Close()
}

// Validate checks everything that can be checked without a network: the schema
// version, the name, the host patterns, and the two definitions that are parsed
// by other packages.
func (s *Session) Validate() error {
	switch {
	case s.Version == 0:
		return fmt.Errorf("session: missing version (this build writes version %d)", FormatVersion)
	case s.Version > FormatVersion:
		return fmt.Errorf("session %s: version %d is newer than this build understands (%d)",
			s.Name, s.Version, FormatVersion)
	case s.Version < 0:
		return fmt.Errorf("session %s: invalid version %d", s.Name, s.Version)
	}

	if err := ValidateName(s.Name); err != nil {
		return err
	}
	if len(s.Hosts) == 0 {
		return fmt.Errorf("session %s: no hosts", s.Name)
	}

	for i, h := range s.Hosts {
		if strings.TrimSpace(h.Pattern) == "" {
			return fmt.Errorf("session %s: host %d: empty pattern", s.Name, i+1)
		}
		if _, err := hosts.Expand(h.Pattern); err != nil {
			return fmt.Errorf("session %s: host %q: %w", s.Name, h.Pattern, err)
		}
		if h.Port < 0 || h.Port > 65535 {
			return fmt.Errorf("session %s: host %q: invalid port %d", s.Name, h.Pattern, h.Port)
		}
		if err := (secret.Command{Argv: h.SecretCommand}).Validate(); err != nil {
			return fmt.Errorf("session %s: host %q: %w", s.Name, h.Pattern, err)
		}
	}
	if s.Defaults.Port < 0 || s.Defaults.Port > 65535 {
		return fmt.Errorf("session %s: invalid default port %d", s.Name, s.Defaults.Port)
	}
	if err := (secret.Command{Argv: s.Defaults.SecretCommand}).Validate(); err != nil {
		return fmt.Errorf("session %s: defaults: %w", s.Name, err)
	}

	if s.Broadcast != "" {
		if _, err := broadcast.ParseMode(s.Broadcast); err != nil {
			return fmt.Errorf("session %s: %w", s.Name, err)
		}
	}
	if s.WorkingSet != "" {
		// A saved working set cannot refer to the live selection: there is no
		// selection before the run starts.
		if _, err := workingset.ParseSelector(s.WorkingSet, nil); err != nil {
			return fmt.Errorf("session %s: working_set: %w", s.Name, err)
		}
	}
	return nil
}

// Mode is the broadcast mode the run starts in.
func (s *Session) Mode() (broadcast.Mode, error) {
	if s.Broadcast == "" {
		return broadcast.ModeAll, nil
	}
	return broadcast.ParseMode(s.Broadcast)
}

// Selector is the working set the run starts with, or nil when the session does
// not narrow it.
func (s *Session) Selector() (workingset.Selector, error) {
	if s.WorkingSet == "" {
		return nil, nil
	}
	return workingset.ParseSelector(s.WorkingSet, nil)
}

// Patterns returns the host patterns as typed, which is what the host expander
// takes.
func (s *Session) Patterns() []string {
	out := make([]string, 0, len(s.Hosts))
	for _, h := range s.Hosts {
		out = append(out, h.Pattern)
	}
	return out
}

// Options returns the connection options for one host entry, with the session
// defaults filled in where the entry is silent.
func (s *Session) Options(i int) HostOptions {
	if i < 0 || i >= len(s.Hosts) {
		return s.Defaults
	}
	o := s.Hosts[i].HostOptions
	if o.User == "" {
		o.User = s.Defaults.User
	}
	if o.Port == 0 {
		o.Port = s.Defaults.Port
	}
	if o.IdentityFile == "" {
		o.IdentityFile = s.Defaults.IdentityFile
	}
	if o.JumpHost == "" {
		o.JumpHost = s.Defaults.JumpHost
	}
	if len(o.SecretCommand) == 0 {
		o.SecretCommand = s.Defaults.SecretCommand
	}
	return o
}

// SecretCommand returns the credential command for one host entry, with the
// session default filled in. An empty command means there is none, and
// authentication falls back to the agent, an identity file or a prompt.
func (s *Session) SecretCommand(i int) secret.Command {
	return secret.Command{Argv: s.Options(i).SecretCommand}
}

// HostCount expands every pattern and reports how many hosts the session opens.
// It is what the Sessions panel shows next to the name.
func (s *Session) HostCount() (int, error) {
	expanded, err := hosts.ExpandAll(s.Patterns())
	if err != nil {
		return 0, fmt.Errorf("session %s: %w", s.Name, err)
	}
	return len(expanded), nil
}

// MarshalYAML omits an all-empty defaults block, so a saved file carries only
// what the user actually set.
func (s Session) MarshalYAML() (any, error) {
	type alias Session // avoids recursing into this method
	out := alias(s)
	if out.Defaults.empty() {
		// yaml.v3 has no omitempty for structs; encoding through a shadow type
		// with the field dropped is the only way to leave it out.
		type withoutDefaults struct {
			Version     int         `yaml:"version"`
			Name        string      `yaml:"name"`
			Description string      `yaml:"description,omitempty"`
			Hosts       []HostEntry `yaml:"hosts"`
			Broadcast   string      `yaml:"broadcast,omitempty"`
			WorkingSet  string      `yaml:"working_set,omitempty"`
		}
		return withoutDefaults{
			Version:     out.Version,
			Name:        out.Name,
			Description: out.Description,
			Hosts:       out.Hosts,
			Broadcast:   out.Broadcast,
			WorkingSet:  out.WorkingSet,
		}, nil
	}
	return out, nil
}
