package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/sessions"
)

// sessionsApp builds an app on the Sessions panel over a store in a temporary
// directory.
func sessionsApp(t *testing.T, saved ...*sessions.Session) (App, *sessions.Store) {
	t.Helper()

	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for _, s := range saved {
		if err := store.Save(s); err != nil {
			t.Fatalf("Save %s: %v", s.Name, err)
		}
	}

	a := resize(t, NewApp(Config{
		Hosts:       []string{"web-01", "web-02"},
		RunPatterns: []string{"web-{01..02}.example.com"},
		Sessions:    store,
		Theme:       Options{Dark: true},
	}), 120, 40)

	return pressKey(t, a, "4"), store
}

func savedSession(name string, patterns ...string) *sessions.Session {
	s := &sessions.Session{Version: sessions.FormatVersion, Name: name}
	for _, p := range patterns {
		s.Hosts = append(s.Hosts, sessions.HostEntry{Pattern: p})
	}
	return s
}

func TestSessionsPanelListsSavedSessionsWithHostCounts(t *testing.T) {
	prod := savedSession("prod", "srv1-{01..04}.example.com")
	prod.Description = "the production web tier"
	a, _ := sessionsApp(t, prod, savedSession("staging", "stage-01"))

	view := plain(a.sessionsPanel(60, 20))
	for _, want := range []string{"prod (4 hosts)", "the production web tier", "staging (1 host)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the Sessions panel does not show %q:\n%s", want, view)
		}
	}
}

// The acceptance criterion: launching from here is equivalent to `lazycssh @name`.
// The panel does not dial; it says what the user chose.
func TestEnterLaunchesTheSelectedSession(t *testing.T) {
	a, _ := sessionsApp(t, savedSession("prod", "h1"), savedSession("staging", "h2"))
	a = pressKey(t, a, "j")

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(SessionLaunchMsg)
	if !ok {
		t.Fatalf("the command produced a %T", cmd())
	}
	if msg.Name != "staging" || msg.Merge {
		t.Fatalf("SessionLaunchMsg = %+v", msg)
	}
}

func TestSpaceMergesASession(t *testing.T) {
	a, _ := sessionsApp(t, savedSession("prod", "h1"))

	model, cmd := a.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("space produced no command")
	}
	msg, ok := cmd().(SessionLaunchMsg)
	if !ok {
		t.Fatalf("the command produced a %T", cmd())
	}
	if msg.Name != "prod" || !msg.Merge {
		t.Fatalf("SessionLaunchMsg = %+v", msg)
	}
}

// The acceptance criterion: saving from the panel writes a file that
// `lazycssh @name` reproduces.
func TestSaveFromThePanelWritesASession(t *testing.T) {
	a, store := sessionsApp(t)

	a = pressKey(t, a, "w")
	if !a.Saving() {
		t.Fatal("w did not open the save prompt")
	}
	for _, r := range "prod-web" {
		a = pressKey(t, a, string(r))
	}

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Saving() {
		t.Fatal("the prompt is still open after saving")
	}
	if cmd == nil {
		t.Fatal("saving did not ask the panel to reload")
	}
	if _, ok := cmd().(SessionsChangedMsg); !ok {
		t.Fatalf("saving produced a %T", cmd())
	}

	sess, err := store.Load("prod-web")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// The run's patterns are written, not the hosts they expanded to.
	if got := strings.Join(sess.Patterns(), ","); got != "web-{01..02}.example.com" {
		t.Fatalf("saved patterns = %q", got)
	}
}

// The acceptance criterion: overwriting an existing session asks first.
func TestOverwriteAsksFirst(t *testing.T) {
	existing := savedSession("prod", "old-host.example.com")
	a, store := sessionsApp(t, existing)

	a = pressKey(t, a, "w")
	for _, r := range "prod" {
		a = pressKey(t, a, string(r))
	}
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if !a.Saving() {
		t.Fatal("the overwrite question closed without being answered")
	}
	if !strings.Contains(plain(a.sessionsPanel(60, 20)), "overwrite") {
		t.Fatalf("the panel does not ask:\n%s", plain(a.sessionsPanel(60, 20)))
	}

	// Nothing has been written yet.
	sess, err := store.Load("prod")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.Join(sess.Patterns(), ","); got != "old-host.example.com" {
		t.Fatalf("the file was replaced before the question was answered: %q", got)
	}

	// Answering no leaves it alone.
	a = pressKey(t, a, "n")
	if a.Saving() {
		t.Fatal("answering no left the prompt open")
	}
	sess, _ = store.Load("prod")
	if got := strings.Join(sess.Patterns(), ","); got != "old-host.example.com" {
		t.Fatalf("answering no still overwrote: %q", got)
	}

	// Answering yes writes it.
	a = pressKey(t, a, "w")
	for _, r := range "prod" {
		a = pressKey(t, a, string(r))
	}
	model, _ = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, _ = model.(App)
	a = pressKey(t, a, "y")

	sess, err = store.Load("prod")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.Join(sess.Patterns(), ","); got != "web-{01..02}.example.com" {
		t.Fatalf("answering yes did not overwrite: %q", got)
	}
}

func TestEscapeAbandonsTheSave(t *testing.T) {
	a, store := sessionsApp(t)

	a = pressKey(t, a, "w")
	for _, r := range "prod" {
		a = pressKey(t, a, string(r))
	}
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Saving() {
		t.Fatal("escape left the prompt open")
	}
	if store.Exists("prod") {
		t.Fatal("escape wrote the session anyway")
	}
}

// The save prompt owns the keyboard: a session called "x" has to be nameable.
func TestSavePromptOwnsTheKeyboard(t *testing.T) {
	a, _ := sessionsApp(t)
	a = pressKey(t, a, "w")
	a = pressKey(t, a, "1")

	if a.Panel() != PanelSessions {
		t.Fatalf("a keystroke meant for the name changed the panel to %v", a.Panel())
	}
	if !strings.Contains(plain(a.sessionsPanel(60, 20)), "save as: 1") {
		t.Fatalf("the name did not take the keystroke:\n%s", plain(a.sessionsPanel(60, 20)))
	}
}

func TestSaveWithAnInvalidNameReportsTheError(t *testing.T) {
	a, store := sessionsApp(t)

	a = pressKey(t, a, "w")
	for _, r := range "not a name" {
		a = pressKey(t, a, string(r))
	}
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.SaveError() == nil {
		t.Fatal("an invalid session name was accepted")
	}
	if store.Exists("not a name") {
		t.Fatal("an invalid name was written")
	}
	if !strings.Contains(plain(a.sessionsPanel(60, 20)), "not allowed") {
		t.Fatalf("the panel does not report the error:\n%s", plain(a.sessionsPanel(60, 20)))
	}
}

func TestSaveWithAnEmptyNameDoesNothing(t *testing.T) {
	a, store := sessionsApp(t)
	a = pressKey(t, a, "w")

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Saving() || cmd != nil {
		t.Fatal("an empty name was treated as a save")
	}
	if names, err := store.List(); err != nil || len(names) != 0 {
		t.Fatalf("List() = %v, %v", names, err)
	}
}

// One unreadable file is one unreadable row, not an empty panel: hiding the
// other sessions would make a typo look like data loss.
func TestUnreadableSessionBecomesOneRow(t *testing.T) {
	a, store := sessionsApp(t, savedSession("prod", "h1"))

	path := filepath.Join(store.Dir(), "broken.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nname: broken\nhostz: []\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	model, _ := a.Update(SessionsChangedMsg{})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}

	view := plain(a.sessionsPanel(60, 20))
	if !strings.Contains(view, "broken (unreadable)") {
		t.Fatalf("the unreadable session is not listed:\n%s", view)
	}
	if !strings.Contains(view, "prod (1 host)") {
		t.Fatalf("the readable sessions vanished:\n%s", view)
	}
}

func TestSessionsPanelWithoutAStore(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "4")

	if got := plain(a.sessionsPanel(60, 20)); !strings.Contains(got, "no session directory") {
		t.Fatalf("sessionsPanel() = %q", got)
	}
	if a.SelectedSession() != "" {
		t.Fatalf("SelectedSession() = %q", a.SelectedSession())
	}
	// Launching and saving must not panic without a store.
	a = pressKey(t, a, "w")
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
}

func TestSessionsPanelWithNoneSaved(t *testing.T) {
	a, _ := sessionsApp(t)
	if got := plain(a.sessionsPanel(60, 20)); !strings.Contains(got, "no saved sessions") {
		t.Fatalf("sessionsPanel() = %q", got)
	}
}

func TestSessionCursorMovesAndLeavesThePanelAtTheEnds(t *testing.T) {
	a, _ := sessionsApp(t, savedSession("a", "h1"), savedSession("b", "h2"))

	a = pressKey(t, a, "j")
	if a.SelectedSession() != "b" {
		t.Fatalf("SelectedSession() = %q", a.SelectedSession())
	}
	a = pressKey(t, a, "j")
	if a.Panel() != PanelCommandLog {
		t.Fatalf("Panel() = %v after moving off the bottom", a.Panel())
	}
}

func TestSaveRunReportsAStoreFailure(t *testing.T) {
	a, _ := sessionsApp(t)
	a.cfg.Sessions = failingStore{}

	a = pressKey(t, a, "w")
	for _, r := range "prod" {
		a = pressKey(t, a, string(r))
	}
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.SaveError() == nil {
		t.Fatal("a failing store was reported as a successful save")
	}
}

// failingStore fails every write, which is what a read-only home directory
// looks like from here.
type failingStore struct{}

func (failingStore) List() ([]string, error)                { return nil, nil }
func (failingStore) Load(string) (*sessions.Session, error) { return nil, errors.New("nope") }
func (failingStore) Exists(string) bool                     { return false }
func (failingStore) SaveRun(sessions.Run, bool) (*sessions.Session, error) {
	return nil, errors.New("read-only file system")
}

// S opens the save prompt from anywhere at the app level - saving must not
// require finding the Sessions panel first.
func TestQuickSaveOpensThePromptFromAnywhere(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	if a.Panel() == PanelSessions {
		t.Fatal("setup: already on the Sessions panel")
	}

	a = pressKey(t, a, "S")
	if !a.Saving() {
		t.Fatal("S did not open the save prompt")
	}
}

// While typing, S is a keystroke for the host like any other letter.
func TestQuickSaveIsForwardedWhileTyping(t *testing.T) {
	a, fleet := typingApp(t, "web-01")
	a = press(t, a, tea.KeyPressMsg{Code: 'S', Text: "S"})

	if a.Saving() {
		t.Fatal("S opened the save prompt while typing")
	}
	if got := string(fleet.sessions["web-01"].Written()); got != "S" {
		t.Fatalf("the host received %q", got)
	}
}

// Saving an empty run says so without discarding the prompt or the typed
// name: the user may connect a host and press enter again.
func TestSavingAnEmptyRunKeepsThePrompt(t *testing.T) {
	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	a := resize(t, NewApp(Config{Sessions: store, Theme: Options{Dark: true}}), 120, 40)

	a = pressKey(t, a, "S")
	for _, r := range "prod" {
		a = pressKey(t, a, string(r))
	}
	a = pressKey(t, a, "enter")

	if a.SaveError() == nil {
		t.Fatal("an empty run saved without an error")
	}
	if !a.Saving() || a.saveInput.Value() != "prod" {
		t.Fatalf("the prompt did not survive: saving=%v value=%q", a.Saving(), a.saveInput.Value())
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("an empty run wrote a session: %v", list)
	}
}
