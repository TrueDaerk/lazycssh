// Package history is the persistent broadcast command history: the commands
// typed into the command line, oldest first, kept across runs so Up/Down has
// something to walk on a fresh start.
//
// It is deliberately separate from internal/commandlog, which is the in-memory
// audit trail of what was actually sent this run. History is about recalling
// what was typed, not proving what a host received; it survives on disk so a
// command can be found and resent tomorrow.
//
// Only commands typed into the command line ever reach here. Raw broadcast
// keystrokes - single characters echoed straight to a pane, which is how a
// sudo password or an interactive prompt is answered - never pass through this
// package. Keep it that way: a history file is a convenience, not a place a
// credential can leak to.
package history

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// DefaultCapacity bounds how many entries the file keeps. The oldest entries
// are dropped first, the same rule internal/commandlog uses for the in-memory
// log.
const DefaultCapacity = 1000

// File permissions. A history entry is a command the user chose to type, not
// a secret, but it is still local shell history in spirit and kept as
// unreadable to other accounts as internal/sessions keeps its files.
const (
	dirPerm  fs.FileMode = 0o700
	filePerm fs.FileMode = 0o600
)

// DefaultPath is where the persistent command history lives:
// `$XDG_CONFIG_HOME/lazycssh/history`, falling back to `~/.config/lazycssh/history`.
func DefaultPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "lazycssh", "history"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("history: locate home directory: %w", err)
	}
	return filepath.Join(home, ".config", "lazycssh", "history"), nil
}

// Store reads and writes one history file.
//
// The zero value is not usable; construct one with [NewStore] or
// [DefaultStore]. The file is not created until something is appended, so
// loading an empty history does not litter the user's home directory.
type Store struct {
	path     string
	capacity int
}

// NewStore builds a store over a file. A capacity of zero means
// [DefaultCapacity].
func NewStore(path string, capacity int) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("history: empty store path")
	}
	if capacity <= 0 {
		capacity = DefaultCapacity
	}
	return &Store{path: path, capacity: capacity}, nil
}

// DefaultStore builds a store over [DefaultPath] with [DefaultCapacity].
func DefaultStore() (*Store, error) {
	path, err := DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewStore(path, 0)
}

// Path is the file the store reads and writes.
func (s *Store) Path() string { return s.path }

// Load returns the saved commands, oldest first. A missing file is not an
// error: it is a user who has not sent a command yet.
func (s *Store) Load() ([]string, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("history: open %s: %w", s.path, err)
	}
	defer f.Close()

	var entries []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		entries = append(entries, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("history: read %s: %w", s.path, err)
	}
	return entries, nil
}

// Append adds one command, creating the file and its directory if needed.
//
// A repeat of the last stored command is not appended again - holding enter on
// the same command should not fill the history with it, the same rule the
// command line already applies to its in-memory copy. An empty command is
// silently skipped rather than treated as an error, since nothing sent nothing
// is not a failure.
//
// The write is atomic: a temporary file in the same directory, then a rename,
// so an interrupted append cannot leave a half-written history behind.
func (s *Store) Append(command string) error {
	command = strings.TrimSpace(command)
	if command == "" {
		return nil
	}

	entries, err := s.Load()
	if err != nil {
		return err
	}
	if n := len(entries); n > 0 && entries[n-1] == command {
		return nil
	}
	entries = append(entries, command)
	if over := len(entries) - s.capacity; over > 0 {
		entries = entries[over:]
	}
	return s.write(entries)
}

// write rewrites the whole file with the given entries, oldest first.
func (s *Store) write(entries []string) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("history: create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".history.*.tmp")
	if err != nil {
		return fmt.Errorf("history: create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded

	if err := tmp.Chmod(filePerm); err != nil {
		tmp.Close()
		return fmt.Errorf("history: chmod: %w", err)
	}

	w := bufio.NewWriter(tmp)
	for _, entry := range entries {
		if _, err := w.WriteString(entry); err != nil {
			tmp.Close()
			return fmt.Errorf("history: write %s: %w", s.path, err)
		}
		if _, err := w.WriteString("\n"); err != nil {
			tmp.Close()
			return fmt.Errorf("history: write %s: %w", s.path, err)
		}
	}
	if err := w.Flush(); err != nil {
		tmp.Close()
		return fmt.Errorf("history: flush %s: %w", s.path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("history: close temporary file: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("history: write %s: %w", s.path, err)
	}
	return nil
}
