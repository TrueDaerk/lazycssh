package sessions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fileExtension is the suffix of a session file.
const fileExtension = ".yaml"

// File permissions. A session file names hosts, users and key paths - an
// inventory of what this machine can reach - so it is not world-readable even
// though it holds no secret.
const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// ErrNotFound is returned when no session file carries the requested name.
var ErrNotFound = errors.New("session: not found")

// ValidateName rejects names that cannot be a file, that could escape the
// session directory, or that would be unusable on the command line.
func ValidateName(name string) error {
	if name == "" {
		return fmt.Errorf("session: empty name")
	}
	if len(name) > 64 {
		return fmt.Errorf("session %q: name is longer than 64 characters", name)
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("session %q: name may not start with a dot", name)
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return fmt.Errorf("session %q: %q is not allowed in a session name "+
				"(letters, digits, dot, dash and underscore only)", name, r)
		}
	}
	return nil
}

// DefaultDir is where session files live: `$XDG_CONFIG_HOME/lazycssh/sessions`,
// falling back to `~/.config/lazycssh/sessions`.
func DefaultDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lazycssh", "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("session: locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lazycssh", "sessions"), nil
}

// Store reads and writes session files in one directory.
//
// The zero value is not usable; construct one with [NewStore] or
// [DefaultStore]. The directory is not created until something is saved, so
// listing an empty configuration does not litter the user's home directory.
type Store struct {
	dir string
}

// NewStore builds a store over a directory.
func NewStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("session: empty store directory")
	}
	return &Store{dir: dir}, nil
}

// DefaultStore builds a store over [DefaultDir].
func DefaultStore() (*Store, error) {
	dir, err := DefaultDir()
	if err != nil {
		return nil, err
	}
	return NewStore(dir)
}

// Dir is the directory the store reads and writes.
func (s *Store) Dir() string { return s.dir }

// Path is the file a named session lives in.
func (s *Store) Path(name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.dir, name+fileExtension), nil
}

// List returns the session names in lexical order. A missing directory is not
// an error: it is a user who has not saved anything yet.
func (s *Store) List() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("session: read %s: %w", s.dir, err)
	}

	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), fileExtension) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), fileExtension)
		if ValidateName(name) != nil {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// Exists reports whether a session file is there.
func (s *Store) Exists(name string) bool {
	path, err := s.Path(name)
	if err != nil {
		return false
	}
	_, err = os.Stat(path)
	return err == nil
}

// Load reads one session. A file whose `name` disagrees with its filename is an
// error: `lazycssh @a` must not open the session called `b`.
func (s *Store) Load(name string) (*Session, error) {
	path, err := s.Path(name)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("session %q: %w", name, ErrNotFound)
		}
		return nil, fmt.Errorf("session %q: open: %w", name, err)
	}
	defer f.Close()

	sess, err := Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if sess.Name != name {
		return nil, fmt.Errorf("session %q: file %s declares the name %q", name, path, sess.Name)
	}
	return sess, nil
}

// Save writes a session, creating the directory if needed. It overwrites
// silently; asking before replacing an existing session is the caller's job,
// because only the caller can ask.
//
// The write is atomic: a temporary file in the same directory, then a rename, so
// an interrupted save cannot leave a half-written session behind.
func (s *Store) Save(sess *Session) error {
	if sess == nil {
		return fmt.Errorf("session: nil session")
	}
	if sess.Version == 0 {
		sess.Version = FormatVersion
	}
	if err := sess.Validate(); err != nil {
		return err
	}
	path, err := s.Path(sess.Name)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, dirPerm); err != nil {
		return fmt.Errorf("session %s: create %s: %w", sess.Name, s.dir, err)
	}

	tmp, err := os.CreateTemp(s.dir, "."+sess.Name+".*"+fileExtension)
	if err != nil {
		return fmt.Errorf("session %s: create temporary file: %w", sess.Name, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded

	if err := tmp.Chmod(filePerm); err != nil {
		tmp.Close()
		return fmt.Errorf("session %s: chmod: %w", sess.Name, err)
	}
	if err := Encode(tmp, sess); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("session %s: close: %w", sess.Name, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("session %s: write %s: %w", sess.Name, path, err)
	}
	return nil
}

// Remove deletes a session file.
func (s *Store) Remove(name string) error {
	path, err := s.Path(name)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("session %q: %w", name, ErrNotFound)
		}
		return fmt.Errorf("session %q: remove: %w", name, err)
	}
	return nil
}
