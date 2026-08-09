package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/sessions"
)

// groupsStoreApp builds an app on the Groups panel over a store in a temporary
// directory.
func groupsStoreApp(t *testing.T, saved ...*sessions.Session) (App, *sessions.Store) {
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

	return pressKey(t, a, "2"), store
}

// groupsApp is the three-group fixture the mouse tests share: enough rows for
// a click on the third one.
func groupsApp(t *testing.T) (App, *sessions.Store) {
	t.Helper()
	return groupsStoreApp(t,
		savedGroup("alpha", "h1"), savedGroup("beta", "h2"), savedGroup("gamma", "h3"))
}

func savedGroup(name string, patterns ...string) *sessions.Session {
	s := &sessions.Session{Version: sessions.FormatVersion, Name: name}
	for _, p := range patterns {
		s.Hosts = append(s.Hosts, sessions.HostEntry{Pattern: p})
	}
	return s
}

// typeInto feeds a string character by character, the way a user types it.
func typeInto(t *testing.T, a App, s string) App {
	t.Helper()
	for _, r := range s {
		a = press(t, a, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return a
}

func TestGroupsPanelListsSavedGroupsWithHostCounts(t *testing.T) {
	prod := savedGroup("prod", "srv1-{01..04}.example.com")
	prod.Description = "the production web tier"
	a, _ := groupsStoreApp(t, prod, savedGroup("staging", "stage-01"))

	view := plain(a.groupsPanel(60, 20))
	for _, want := range []string{"prod (4 hosts)", "the production web tier", "staging (1 host)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the Groups panel does not show %q:\n%s", want, view)
		}
	}
}

// The acceptance criterion: opening from here is equivalent to `lazycssh @name`.
// The panel does not dial; it says what the user chose.
func TestEnterOpensTheSelectedGroup(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "h1"), savedGroup("staging", "h2"))
	a = pressKey(t, a, "j")

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(GroupOpenMsg)
	if !ok {
		t.Fatalf("the command produced a %T", cmd())
	}
	if msg.Name != "staging" {
		t.Fatalf("GroupOpenMsg = %+v", msg)
	}
}

func TestSpaceOpensTheSelectedGroupToo(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "h1"))

	model, cmd := a.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("space produced no command")
	}
	if msg, ok := cmd().(GroupOpenMsg); !ok || msg.Name != "prod" {
		t.Fatalf("space produced %T %+v", cmd(), cmd())
	}
}

// The acceptance criterion: a group created in the dialog survives a restart -
// it is a file the store can read back, patterns as typed.
func TestNCreatesAGroup(t *testing.T) {
	a, store := groupsStoreApp(t)

	a = pressKey(t, a, "n")
	if !a.GroupDialogOpen() {
		t.Fatal("n did not open the new-group dialog")
	}
	a = typeInto(t, a, "web")
	a = pressKey(t, a, "enter")
	a = typeInto(t, a, "web-{01..03}.example.com db-01")

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.GroupDialogOpen() {
		t.Fatal("the dialog is still open after a successful save")
	}
	if cmd == nil {
		t.Fatal("saving did not ask the panel to reload")
	}
	if _, ok := cmd().(SessionsChangedMsg); !ok {
		t.Fatalf("saving produced a %T", cmd())
	}

	sess, err := store.Load("web")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.Join(sess.Patterns(), ","); got != "web-{01..03}.example.com,db-01" {
		t.Fatalf("saved patterns = %q", got)
	}
}

func TestNewGroupRefusesATakenName(t *testing.T) {
	a, store := groupsStoreApp(t, savedGroup("prod", "h1"))

	a = pressKey(t, a, "n")
	a = typeInto(t, a, "prod")
	a = pressKey(t, a, "enter")

	if !a.GroupDialogOpen() {
		t.Fatal("a taken name closed the dialog")
	}
	if !strings.Contains(plain(a.groupsPanel(60, 20)), "already exists") {
		t.Fatalf("the panel does not report the taken name:\n%s", plain(a.groupsPanel(60, 20)))
	}
	sess, err := store.Load("prod")
	if err != nil || len(sess.Hosts) != 1 {
		t.Fatalf("the existing group was touched: %v, %v", sess, err)
	}
}

// A malformed pattern must be refused before anything lands on disk, and the
// typed input must survive the telling.
func TestNewGroupRefusesAMalformedPattern(t *testing.T) {
	a, store := groupsStoreApp(t)

	a = pressKey(t, a, "n")
	a = typeInto(t, a, "web")
	a = pressKey(t, a, "enter")
	a = typeInto(t, a, "web-{01")
	a = pressKey(t, a, "enter")

	if !a.GroupDialogOpen() {
		t.Fatal("a malformed pattern closed the dialog")
	}
	if a.groupErr == nil {
		t.Fatal("a malformed pattern produced no error")
	}
	if store.Exists("web") {
		t.Fatal("a malformed pattern was written anyway")
	}
	if a.groupHostsInput.Value() != "web-{01" {
		t.Fatalf("the typed patterns did not survive: %q", a.groupHostsInput.Value())
	}
}

func TestEscapeAbandonsTheGroupDialog(t *testing.T) {
	a, store := groupsStoreApp(t)

	a = pressKey(t, a, "n")
	a = typeInto(t, a, "web")
	a = pressKey(t, a, "esc")

	if a.GroupDialogOpen() {
		t.Fatal("escape left the dialog open")
	}
	if store.Exists("web") {
		t.Fatal("escape wrote the group anyway")
	}
}

// The dialog owns the keyboard: a group called "1" has to be nameable, and a
// host list contains spaces.
func TestGroupDialogOwnsTheKeyboard(t *testing.T) {
	a, _ := groupsStoreApp(t)
	a = pressKey(t, a, "n")
	a = typeInto(t, a, "1b")

	if a.Panel() != PanelGroups {
		t.Fatalf("a keystroke meant for the name changed the panel to %v", a.Panel())
	}
	if !strings.Contains(plain(a.groupsPanel(60, 20)), "new group name: 1b") {
		t.Fatalf("the name did not take the keystrokes:\n%s", plain(a.groupsPanel(60, 20)))
	}
}

// The acceptance criterion: deleting asks first, and no deletes nothing.
func TestDeleteAsksFirst(t *testing.T) {
	a, store := groupsStoreApp(t, savedGroup("prod", "h1"))

	a = pressKey(t, a, "d")
	if a.DeleteGroupPending() != "prod" {
		t.Fatalf("DeleteGroupPending() = %q", a.DeleteGroupPending())
	}
	if !strings.Contains(plain(a.groupsPanel(60, 20)), `delete "prod"? y/n`) {
		t.Fatalf("the panel does not ask:\n%s", plain(a.groupsPanel(60, 20)))
	}

	a = pressKey(t, a, "n") // anything but y withdraws the question
	if a.DeleteGroupPending() != "" {
		t.Fatal("answering no left the question open")
	}
	if !store.Exists("prod") {
		t.Fatal("answering no still deleted the group")
	}

	a = pressKey(t, a, "d")
	model, cmd := a.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if store.Exists("prod") {
		t.Fatal("answering yes did not delete the group")
	}
	if cmd == nil {
		t.Fatal("deleting did not ask the panel to reload")
	}
	if _, ok := cmd().(SessionsChangedMsg); !ok {
		t.Fatalf("deleting produced a %T", cmd())
	}
}

// Deleting a definition must not tear down live connections: the open session
// of that group stays.
func TestDeleteLeavesTheOpenSessionAlone(t *testing.T) {
	a, store := groupsStoreApp(t, savedGroup("prod", "web-01"))

	model, _ := a.Update(SessionOpenedMsg{Name: "prod", Hosts: []string{"web-01"}})
	a = model.(App)
	if a.ActiveSession() != "prod" {
		t.Fatalf("setup: ActiveSession() = %q", a.ActiveSession())
	}

	a = pressKey(t, a, "2") // back onto the Groups panel
	a = pressKey(t, a, "d")
	a = pressKey(t, a, "y")

	if store.Exists("prod") {
		t.Fatal("the group file survived")
	}
	if a.ActiveSession() != "prod" {
		t.Fatalf("deleting the file closed the session: ActiveSession() = %q", a.ActiveSession())
	}
}

// A group whose session is open is marked, with a character as well as a
// style, so it survives a terminal without colour.
func TestOpenGroupIsMarked(t *testing.T) {
	store, err := sessions.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if err := store.Save(savedGroup("prod", "web-01")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	a := resize(t, NewApp(Config{Sessions: store, Theme: Options{NoColor: true}}), 120, 40)
	model, _ := a.Update(SessionOpenedMsg{Name: "prod", Hosts: []string{"web-01"}})
	a = model.(App)

	if !strings.Contains(plain(a.groupsPanel(60, 20)), "▸ prod") {
		t.Fatalf("the open group is not marked:\n%s", plain(a.groupsPanel(60, 20)))
	}
}

func TestGroupsPanelWithoutAStore(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "2")

	if got := plain(a.groupsPanel(60, 20)); !strings.Contains(got, "no group directory") {
		t.Fatalf("groupsPanel() = %q", got)
	}
	if a.SelectedGroup() != "" {
		t.Fatalf("SelectedGroup() = %q", a.SelectedGroup())
	}
	// Opening, creating and deleting must not panic without a store.
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, _ = model.(App)
	a = pressKey(t, a, "n")
	a = typeInto(t, a, "x")
	a = pressKey(t, a, "enter")
	a = typeInto(t, a, "h1")
	a = pressKey(t, a, "enter")
	a = pressKey(t, a, "esc")
	a = pressKey(t, a, "d")
	_ = a
}

// One unreadable file is one unreadable row, not an empty panel.
func TestUnreadableGroupBecomesOneRow(t *testing.T) {
	a, store := groupsStoreApp(t, savedGroup("prod", "h1"))

	path := filepath.Join(store.Dir(), "broken.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nname: broken\nhostz: []\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	model, _ := a.Update(SessionsChangedMsg{})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}

	view := plain(a.groupsPanel(60, 20))
	if !strings.Contains(view, "broken (unreadable)") {
		t.Fatalf("the unreadable group is not listed:\n%s", view)
	}
	if !strings.Contains(view, "prod (1 host)") {
		t.Fatalf("the readable groups vanished:\n%s", view)
	}
}

// At the bottom of the list, down is a no-op: it never switches away from the
// Groups panel (issue #212).
func TestGroupCursorMovesAndStaysInThePanelAtTheEnds(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("a", "h1"), savedGroup("b", "h2"))

	a = pressKey(t, a, "j")
	if a.SelectedGroup() != "b" {
		t.Fatalf("SelectedGroup() = %q", a.SelectedGroup())
	}
	a = pressKey(t, a, "j")
	if a.SelectedGroup() != "b" {
		t.Fatalf("SelectedGroup() = %q after running off the bottom", a.SelectedGroup())
	}
	if a.Panel() != PanelGroups {
		t.Fatalf("Panel() = %v after moving off the bottom", a.Panel())
	}
}

// n outside the Groups panel keeps its global meaning: connect a host.
func TestNOutsideTheGroupsPanelConnects(t *testing.T) {
	a, _ := groupsStoreApp(t)
	a = pressKey(t, a, "1")
	a = pressKey(t, a, "n")

	if a.GroupDialogOpen() {
		t.Fatal("n on the Status panel opened the group dialog")
	}
	if !a.ConnectPromptOpen() {
		t.Fatal("n on the Status panel did not open the connect prompt")
	}
}
