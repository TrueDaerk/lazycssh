package sessions

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// store builds a store over a temporary directory.
func store(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// sample is a valid session with the given name.
func sample(name string) *Session {
	return &Session{
		Version: FormatVersion,
		Name:    name,
		Hosts:   []HostEntry{{Pattern: "srv1-{01..04}.example.com"}},
	}
}

func TestNewStoreRejectsAnEmptyDirectory(t *testing.T) {
	if _, err := NewStore(""); err == nil {
		t.Fatal("NewStore accepted an empty directory")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := store(t)
	want := sample("prod-web")
	want.Description = "the production web tier"
	want.Broadcast = "selected"
	want.WorkingSet = "first 2"

	if err := s.Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("prod-web")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Name != want.Name || got.Description != want.Description ||
		got.Broadcast != want.Broadcast || got.WorkingSet != want.WorkingSet ||
		len(got.Hosts) != 1 || got.Hosts[0].Pattern != want.Hosts[0].Pattern {
		t.Fatalf("loaded %+v, want %+v", got, want)
	}
}

func TestSaveFillsInTheFormatVersion(t *testing.T) {
	s := store(t)
	sess := sample("a")
	sess.Version = 0
	if err := s.Save(sess); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if sess.Version != FormatVersion {
		t.Fatalf("Version = %d after Save, want %d", sess.Version, FormatVersion)
	}
}

// The file names hosts, users and key paths; it is not world-readable.
func TestSavePermissions(t *testing.T) {
	s := store(t)
	if err := s.Save(sample("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	path, err := s.Path("a")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != filePerm {
		t.Fatalf("file mode = %v, want %v", got, filePerm)
	}
}

func TestSaveCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "sessions")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := s.Save(sample("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != dirPerm {
		t.Fatalf("directory mode = %v, want %v", got, dirPerm)
	}
}

func TestSaveLeavesNoTemporaryFileBehind(t *testing.T) {
	s := store(t)
	if err := s.Save(sample("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	entries, err := os.ReadDir(s.Dir())
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "a.yaml" {
		t.Fatalf("directory holds %v", entries)
	}
}

func TestSaveRejectsInvalidSessions(t *testing.T) {
	s := store(t)
	if err := s.Save(nil); err == nil {
		t.Fatal("Save accepted nil")
	}
	if err := s.Save(&Session{Name: "a"}); err == nil {
		t.Fatal("Save accepted a session with no hosts")
	}
	if err := s.Save(&Session{Name: "../evil", Hosts: []HostEntry{{Pattern: "h"}}}); err == nil {
		t.Fatal("Save accepted a name that escapes the directory")
	}
}

func TestSaveOverwrites(t *testing.T) {
	s := store(t)
	if err := s.Save(sample("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	second := sample("a")
	second.Description = "replaced"
	if err := s.Save(second); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := s.Load("a")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Description != "replaced" {
		t.Fatalf("Description = %q", got.Description)
	}
}

func TestList(t *testing.T) {
	s := store(t)
	if got, err := s.List(); err != nil || got != nil {
		t.Fatalf("List() on a missing directory = %v, %v", got, err)
	}

	for _, name := range []string{"prod", "dev", "staging"} {
		if err := s.Save(sample(name)); err != nil {
			t.Fatalf("Save %s: %v", name, err)
		}
	}
	// Files that are not sessions are ignored rather than reported.
	if err := os.WriteFile(filepath.Join(s.Dir(), "notes.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Mkdir(filepath.Join(s.Dir(), "sub.yaml"), 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	got, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if strings.Join(got, ",") != "dev,prod,staging" {
		t.Fatalf("List() = %v", got)
	}
}

func TestExists(t *testing.T) {
	s := store(t)
	if s.Exists("a") {
		t.Fatal("Exists reported a session that was never saved")
	}
	if err := s.Save(sample("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !s.Exists("a") {
		t.Fatal("Exists missed a saved session")
	}
	if s.Exists("../evil") {
		t.Fatal("Exists accepted an invalid name")
	}
}

func TestLoadMissingSession(t *testing.T) {
	s := store(t)
	_, err := s.Load("nope")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Load error = %v, want ErrNotFound", err)
	}
}

// `lazycssh @a` must never open the session called `b`.
func TestLoadRejectsANameMismatch(t *testing.T) {
	s := store(t)
	path := filepath.Join(s.Dir(), "a.yaml")
	body := "version: 1\nname: b\nhosts:\n  - pattern: h1\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := s.Load("a")
	if err == nil || !strings.Contains(err.Error(), "declares the name") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestLoadReportsTheFileInParseErrors(t *testing.T) {
	s := store(t)
	path := filepath.Join(s.Dir(), "a.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nname: a\nhostz: []\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err := s.Load("a")
	if err == nil || !strings.Contains(err.Error(), "a.yaml") {
		t.Fatalf("Load error = %v, want the path", err)
	}
}

func TestRemove(t *testing.T) {
	s := store(t)
	if err := s.Save(sample("a")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := s.Remove("a"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if s.Exists("a") {
		t.Fatal("Remove left the file behind")
	}
	if err := s.Remove("a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remove error = %v, want ErrNotFound", err)
	}
	if err := s.Remove("../evil"); err == nil {
		t.Fatal("Remove accepted an invalid name")
	}
}

func TestValidateName(t *testing.T) {
	valid := []string{"prod", "prod-web", "prod_web", "prod.web", "a1"}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Fatalf("ValidateName(%q) = %v", name, err)
		}
	}

	invalid := []string{"", "../evil", "a/b", ".hidden", "with space", "web*", strings.Repeat("a", 65)}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Fatalf("ValidateName(%q) accepted it", name)
		}
	}
}

func TestDefaultDirFollowsXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg")
	dir, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if dir != filepath.Join("/tmp/xdg", "lazycssh", "sessions") {
		t.Fatalf("DefaultDir() = %q", dir)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	dir, err = DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	if dir != filepath.Join(home, ".config", "lazycssh", "sessions") {
		t.Fatalf("DefaultDir() = %q", dir)
	}
}

func TestDefaultStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := DefaultStore()
	if err != nil {
		t.Fatalf("DefaultStore: %v", err)
	}
	if !strings.HasSuffix(s.Dir(), filepath.Join("lazycssh", "sessions")) {
		t.Fatalf("Dir() = %q", s.Dir())
	}
}
