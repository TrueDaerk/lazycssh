package sessions

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/workingset"
)

// ErrExists is returned by [Store.Create] when a session of that name is
// already saved. It exists so the caller can ask before replacing something the
// user spent effort on; only the caller can ask.
var ErrExists = errors.New("session: already exists")

// Run describes the live run being saved.
//
// It is the arguments as the user gave them, not what they expanded to: saving
// forty resolved hostnames would produce a file that is unreadable and wrong the
// moment the fleet changes.
type Run struct {
	// Name is the session name to save under.
	Name string
	// Description is an optional note.
	Description string
	// Patterns are the host arguments as typed, in order.
	Patterns []string
	// Defaults are the connection options that apply to every host.
	Defaults HostOptions
	// Overrides are per-pattern connection options, keyed by pattern. A
	// pattern with no entry inherits the defaults.
	Overrides map[string]HostOptions
	// Broadcast is the mode the run is in.
	Broadcast broadcast.Mode
	// WorkingSet is the active working set definition, or nil to save none.
	WorkingSet workingset.Selector
}

// FromRun builds a session from the live run.
//
// A manual working set is dropped rather than saved: it is a list of session
// identifiers from this run, and writing it into a file that will be opened
// against a different fleet would restore a selection that no longer means
// anything. Everything else round-trips.
func FromRun(run Run) (*Session, error) {
	if err := ValidateName(run.Name); err != nil {
		return nil, err
	}
	if len(run.Patterns) == 0 {
		return nil, fmt.Errorf("session %s: no hosts to save", run.Name)
	}

	s := &Session{
		Version:     FormatVersion,
		Name:        run.Name,
		Description: run.Description,
		Defaults:    run.Defaults,
	}

	for _, pattern := range run.Patterns {
		if strings.TrimSpace(pattern) == "" {
			return nil, fmt.Errorf("session %s: empty host pattern", run.Name)
		}
		s.Hosts = append(s.Hosts, HostEntry{
			Pattern:     pattern,
			HostOptions: withoutDefaults(run.Overrides[pattern], run.Defaults),
		})
	}

	if run.Broadcast != broadcast.ModeAll {
		if !run.Broadcast.Valid() {
			return nil, fmt.Errorf("session %s: invalid broadcast mode %d", run.Name, int(run.Broadcast))
		}
		s.Broadcast = run.Broadcast.String()
	}
	if sel := run.WorkingSet; sel != nil && sel.Kind() != workingset.KindAll &&
		sel.Kind() != workingset.KindManual {
		s.WorkingSet = sel.String()
	}

	if err := s.Validate(); err != nil {
		return nil, err
	}
	return s, nil
}

// withoutDefaults drops the options that only repeat the session defaults, so a
// saved file says what differs rather than restating the defaults on every host.
func withoutDefaults(o, defaults HostOptions) HostOptions {
	if o.User == defaults.User {
		o.User = ""
	}
	if o.Port == defaults.Port {
		o.Port = 0
	}
	if o.IdentityFile == defaults.IdentityFile {
		o.IdentityFile = ""
	}
	if o.JumpHost == defaults.JumpHost {
		o.JumpHost = ""
	}
	if sameArgv(o.SecretCommand, defaults.SecretCommand) {
		o.SecretCommand = nil
	}
	return o
}

// sameArgv compares two commands element by element.
func sameArgv(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Create saves a session and refuses to replace an existing one, returning
// [ErrExists]. The Sessions panel calls this first and asks before falling back
// to [Store.Save], so overwriting is always a decision rather than a side
// effect.
func (s *Store) Create(sess *Session) error {
	if sess == nil {
		return fmt.Errorf("session: nil session")
	}
	if err := ValidateName(sess.Name); err != nil {
		return err
	}
	if s.Exists(sess.Name) {
		return fmt.Errorf("session %q: %w", sess.Name, ErrExists)
	}
	return s.Save(sess)
}

// SaveRun builds a session from the live run and writes it. overwrite decides
// what happens when the name is taken: false returns [ErrExists] and writes
// nothing.
func (s *Store) SaveRun(run Run, overwrite bool) (*Session, error) {
	sess, err := FromRun(run)
	if err != nil {
		return nil, err
	}
	if overwrite {
		if err := s.Save(sess); err != nil {
			return nil, err
		}
		return sess, nil
	}
	if err := s.Create(sess); err != nil {
		return nil, err
	}
	return sess, nil
}

// hostname is the machine name used to describe a saved run when the user gives
// no description. It is deliberately not part of the schema - it goes into the
// free-text description, where it cannot be mistaken for configuration.
func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}

// DescribeRun renders the default description offered when saving: where the
// run was started from and how many patterns it holds. It never names a user's
// credentials, because a description ends up in a file.
func DescribeRun(run Run) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d host pattern", len(run.Patterns)))
	if len(run.Patterns) != 1 {
		b.WriteString("s")
	}
	if h := hostname(); h != "" {
		b.WriteString(" saved from ")
		b.WriteString(h)
	}
	return b.String()
}
