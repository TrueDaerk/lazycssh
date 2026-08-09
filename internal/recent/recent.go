// Package recent is the on-disk list of hosts this machine has connected to,
// most recent first.
//
// It is the memory the host picker needs to offer a machine that is not in
// `~/.ssh/config` and was reached once by typing it (issue #254). It stores
// names only: no user, no port option, no credential - the same rule the rest
// of the persisted state follows.
package recent

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// DefaultCapacity is how many hosts are kept. Enough that a fleet's worth of
// machines survives a week of other work, small enough that the file stays a
// file the user can read.
const DefaultCapacity = 200

// File permissions. The list names the machines this account reaches - an
// inventory, not a secret, but not world-readable either. Same reasoning as
// internal/sessions.
const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// DefaultPath is where the list lives: `$XDG_CONFIG_HOME/lazycssh/recent`,
// falling back to `~/.config/lazycssh/recent`.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lazycssh", "recent"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("recent: locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lazycssh", "recent"), nil
}

// Store reads and writes the recent-host list in one file.
//
// The zero value is not usable; construct one with [NewStore] or
// [DefaultStore]. The file is not created until something is added, so a run
// that connects to nothing leaves nothing behind.
type Store struct {
	path     string
	capacity int
}

// NewStore builds a store over a file. A capacity of zero or less means
// [DefaultCapacity].
func NewStore(path string, capacity int) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("recent: empty store path")
	}
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Store{path: path, capacity: capacity}, nil
}

// DefaultStore builds a store over [DefaultPath].
func DefaultStore() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewStore(path, 0)
}

// Path is the file the store reads and writes.
func (s *Store) Path() string { return s.path }

// Capacity is how many hosts the store keeps.
func (s *Store) Capacity() int { return s.capacity }

// Load returns the hosts, most recent first. A missing file is not an error:
// it is a machine that has not connected to anything yet.
//
// Blank lines are skipped and the list is deduplicated on read as well as on
// write, so a hand-edited file cannot put the same host on two picker rows.
func (s *Store) Load() ([]string, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("recent: open %s: %w", s.path, err)
	}
	defer f.Close()

	var hosts []string
	seen := make(map[string]bool)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		host := strings.TrimSpace(scanner.Text())
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		hosts = append(hosts, host)
		if len(hosts) == s.capacity {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("recent: read %s: %w", s.path, err)
	}
	return hosts, nil
}

// Add records a host as the most recent one. A host already in the list moves
// to the front rather than appearing twice, and the list is truncated to the
// capacity from the back, so what falls off is what was reached longest ago.
//
// An empty name is not an error: there is nothing to record.
func (s *Store) Add(host string) error {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}

	hosts, err := s.Load()
	if err != nil {
		return err
	}
	if len(hosts) > 0 && hosts[0] == host {
		// Already the most recent one: reconnecting in a loop must not
		// rewrite the file on every state change.
		return nil
	}
	if i := slices.Index(hosts, host); i >= 0 {
		hosts = slices.Delete(hosts, i, i+1)
	}
	hosts = append([]string{host}, hosts...)
	if len(hosts) > s.capacity {
		hosts = hosts[:s.capacity]
	}
	return s.write(hosts)
}

// write rewrites the whole file, most recent first.
//
// The write is atomic: a temporary file in the same directory, then a rename,
// so an interrupted add cannot leave a half-written list behind.
func (s *Store) write(hosts []string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("recent: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".recent.*.tmp")
	if err != nil {
		return fmt.Errorf("recent: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded

	if err := tmp.Chmod(filePerm); err != nil {
		tmp.Close()
		return fmt.Errorf("recent: chmod: %w", err)
	}

	w := bufio.NewWriter(tmp)
	for _, host := range hosts {
		if _, err := w.WriteString(host + "\n"); err != nil {
			tmp.Close()
			return fmt.Errorf("recent: write %s: %w", s.path, err)
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return fmt.Errorf("recent: flush %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("recent: close temporary file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("recent: write %s: %w", s.path, err)
	}
	return nil
}
