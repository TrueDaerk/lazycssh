package ui

import (
	"errors"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/sessions"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
	"github.com/TrueDaerk/lazycssh/internal/term"
)

// openTwo opens two sessions over a live fleet on top of the run's own
// "prod-web" session: "front" holding the web hosts, "back" holding the db
// host, "back" in the foreground.
func openTwo(t *testing.T) (App, *fakeFleet) {
	t.Helper()
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "db-01")

	model, _ := a.Update(SessionOpenedMsg{Name: "front", Hosts: []string{"web-01", "web-02"}})
	a = model.(App)
	model, _ = a.Update(SessionOpenedMsg{Name: "back", Hosts: []string{"db-01"}})
	a = model.(App)
	return pressKey(t, a, "3"), fleet
}

func TestSessionsPanelListsOpenSessions(t *testing.T) {
	a, fleet := openTwo(t)
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)

	view := plain(a.sessionsPanel(60, 20, true))
	for _, want := range []string{"front (1/2 up)", "back (0/1 up)"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the Sessions panel does not show %q:\n%s", want, view)
		}
	}
}

// The foreground session is marked with a character as well as a style, so it
// survives a terminal without colour.
func TestForegroundSessionIsMarked(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, SessionName: "prod", Theme: Options{NoColor: true}}), 120, 40)
	if !strings.Contains(plain(a.sessionsPanel(60, 20, true)), "▸ prod") {
		t.Fatalf("the foreground session is not marked:\n%s", plain(a.sessionsPanel(60, 20, true)))
	}
}

// The cursor row gets the strong background highlight only while the panel is
// both selected and the sidebar holds the keyboard, lazygit style; otherwise it
// keeps a muted marker so the position is never lost (issue #222).
func TestSessionCursorHighlightOnlyWhenPanelFocused(t *testing.T) {
	a, _ := openTwo(t)
	th := a.Theme()

	label := "  prod-web (0/3 up)"
	focused := a.sessionsPanel(60, 20, true)
	if !strings.Contains(focused, th.Cursor.Render(label)) {
		t.Fatalf("the focused panel does not use the strong cursor style:\n%s", focused)
	}

	unfocused := a.sessionsPanel(60, 20, false)
	if strings.Contains(unfocused, th.Cursor.Render(label)) {
		t.Fatalf("an unfocused panel still uses the strong cursor style:\n%s", unfocused)
	}
	if !strings.Contains(unfocused, th.CursorMuted.Render(label)) {
		t.Fatalf("an unfocused panel's cursor row is not muted:\n%s", unfocused)
	}

	if plain(focused) != plain(unfocused) {
		t.Fatalf("focus changed the panel's text:\nfocused: %s\nunfocused: %s", plain(focused), plain(unfocused))
	}
}

// The acceptance criterion: enter brings a background session's panes back
// without dialling anything.
func TestEnterForegroundsTheSelectedSession(t *testing.T) {
	a, _ := openTwo(t)
	if a.ActiveSession() != "back" {
		t.Fatalf("setup: ActiveSession() = %q", a.ActiveSession())
	}

	a = pressKey(t, a, "j") // onto "front", the second row
	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.ActiveSession() != "front" {
		t.Fatalf("ActiveSession() = %q after enter", a.ActiveSession())
	}
	if cmd == nil {
		t.Fatal("switching produced no command; the PTYs would keep the old size")
	}
	if _, ok := cmd().(GridChangedMsg); !ok {
		t.Fatalf("switching produced a %T", cmd())
	}
}

func TestSpaceForegroundsTheSelectedSessionToo(t *testing.T) {
	a, _ := openTwo(t)
	a = pressKey(t, a, "j")

	model, _ := a.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	a = model.(App)
	if a.ActiveSession() != "front" {
		t.Fatalf("ActiveSession() = %q after space", a.ActiveSession())
	}
}

// The grid shows only the foreground session's panes; the fleet keeps every
// connection.
func TestGridShowsOnlyTheForegroundSession(t *testing.T) {
	a, _ := openTwo(t)

	if got := strings.Join(a.hostIDs(), ","); got != "db-01" {
		t.Fatalf("visible hosts = %q with back in the foreground", got)
	}
	if got := strings.Join(a.fleetIDs(), ","); got != "web-01,web-02,db-01" {
		t.Fatalf("fleet = %q; a background session must keep its hosts", got)
	}
}

// The acceptance criterion: with a session in the foreground, broadcast all
// reaches only its hosts.
func TestBroadcastIsLimitedToTheForegroundSession(t *testing.T) {
	a, fleet, router, _ := statusApp(t, "web-01", "web-02", "db-01")
	for _, id := range []string{"web-01", "web-02", "db-01"} {
		fleet.connect(t, id)
	}

	model, _ := a.Update(SessionOpenedMsg{Name: "front", Hosts: []string{"web-01", "web-02"}})
	a = model.(App)
	router.Attach(fleetSessions{fleet})

	if got := strings.Join(router.Targets(), ","); got != "web-01,web-02" {
		t.Fatalf("Targets() = %q with front in the foreground", got)
	}

	model, _ = a.Update(SessionOpenedMsg{Name: "back", Hosts: []string{"db-01"}})
	a = model.(App)
	if got := strings.Join(router.Targets(), ","); got != "db-01" {
		t.Fatalf("Targets() = %q with back in the foreground", got)
	}
	_ = a
}

// fleetSessions adapts fakeFleet to the router's transport interface.
type fleetSessions struct{ f *fakeFleet }

func (s fleetSessions) Connected(id string) bool {
	sess, ok := s.f.sessions[id]
	return ok && sess.State() == ssh.StateConnected
}

func (s fleetSessions) AltScreen(id string) bool {
	sess, ok := s.f.sessions[id]
	return ok && sess.State() == ssh.StateConnected && sess.Terminal().IsAltScreen()
}

func (s fleetSessions) Writer(id string) (io.Writer, bool) {
	sess, present := s.f.sessions[id]
	if !present {
		return nil, false
	}
	return sess, sess.State() == ssh.StateConnected
}

func (s fleetSessions) SendKey(id string, k term.KeyEvent) bool {
	sess, present := s.f.sessions[id]
	return present && sess.SendKey(k)
}

// A host that leaves the fleet leaves its sessions; a session that loses its
// last host closes.
func TestClosedHostsLeaveTheirSessions(t *testing.T) {
	a, fleet := openTwo(t)

	// The fleet is the source of truth for who is in the run; the message
	// only says it changed.
	fleet.ids = []string{"web-01", "web-02"}
	delete(fleet.sessions, "db-01")
	model, _ := a.Update(HostsChangedMsg{Hosts: fleet.IDs()})
	a = model.(App)

	if got := strings.Join(a.OpenSessionNames(), ","); got != "prod-web,front" {
		t.Fatalf("open sessions = %q after db-01 left", got)
	}
	if a.ActiveSession() != "front" {
		t.Fatalf("ActiveSession() = %q; losing the foreground must fall back to what is open", a.ActiveSession())
	}
}

// The CLI hosts of an unnamed run live in the ad hoc session; a run started
// from a saved group carries that name.
func TestCLIHostsOpenASession(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1", "h2"}, Theme: Options{Dark: true}}), 120, 40)
	if a.ActiveSession() != "adhoc" {
		t.Fatalf("ActiveSession() = %q for an unnamed run", a.ActiveSession())
	}

	named := resize(t, NewApp(Config{Hosts: []string{"h1"}, SessionName: "prod", Theme: Options{Dark: true}}), 120, 40)
	if named.ActiveSession() != "prod" {
		t.Fatalf("ActiveSession() = %q for a named run", named.ActiveSession())
	}
}

// A host connected at runtime joins the session the user is looking at.
func TestConnectedHostsJoinTheForegroundSession(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)

	model, _ := a.Update(HostsChangedMsg{Hosts: []string{"h1", "h2"}})
	a = model.(App)

	if got := strings.Join(a.hostIDs(), ","); got != "h1,h2" {
		t.Fatalf("visible hosts = %q after connecting h2", got)
	}
	if got := strings.Join(a.OpenSessionNames(), ","); got != "adhoc" {
		t.Fatalf("open sessions = %q; a runtime connect must not open a new one", got)
	}
}

func TestSessionsPanelWithNothingOpen(t *testing.T) {
	a := resize(t, NewApp(Config{Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "3")

	if got := plain(a.sessionsPanel(60, 20, true)); !strings.Contains(got, "no open sessions") {
		t.Fatalf("sessionsPanel() = %q", got)
	}
	if a.SelectedOpenSession() != "" {
		t.Fatalf("SelectedOpenSession() = %q", a.SelectedOpenSession())
	}
	// Enter must not panic with nothing open.
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if _, ok := model.(App); !ok {
		t.Fatalf("Update returned a %T", model)
	}
}

// At the bottom of the list, down is a no-op: it never switches away from the
// Sessions panel (issue #212).
func TestSessionCursorMovesAndStaysInThePanelAtTheEnds(t *testing.T) {
	a, _ := openTwo(t)

	a = pressKey(t, a, "j")
	a = pressKey(t, a, "j")
	if a.SelectedOpenSession() != "back" {
		t.Fatalf("SelectedOpenSession() = %q", a.SelectedOpenSession())
	}
	a = pressKey(t, a, "j")
	if a.SelectedOpenSession() != "back" {
		t.Fatalf("SelectedOpenSession() = %q after running off the bottom", a.SelectedOpenSession())
	}
	if a.Panel() != PanelSessions {
		t.Fatalf("Panel() = %v after moving off the bottom", a.Panel())
	}
}

// --- Saving the run as a group. The prompt lives in the Groups panel. ---

func saveApp(t *testing.T, saved ...*sessions.Session) (App, *sessions.Store) {
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
	return a, store
}

// The acceptance criterion: saving from the panel writes a file that
// `lazycssh @name` reproduces.
func TestSaveWritesAGroup(t *testing.T) {
	a, store := saveApp(t)

	a = pressKey(t, a, "S")
	if !a.Saving() {
		t.Fatal("S did not open the save prompt")
	}
	if a.Panel() != PanelGroups {
		t.Fatalf("the prompt opened on %v, want the Groups panel", a.Panel())
	}
	a = typeInto(t, a, "prod-web")

	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if cmd == nil {
		t.Fatal("enter did not start the write")
	}
	if !a.Saving() {
		t.Fatal("the prompt closed before the write reported back")
	}
	// The write and the reload run in Cmds (issue #225); settle drains them.
	a = settle(t, a, cmd)
	if a.Saving() {
		t.Fatal("the prompt is still open after saving")
	}
	if !strings.Contains(plain(a.panelBody(PanelGroups, 60, 20, true)), "prod-web") {
		t.Fatalf("the panel did not reload the saved group:\n%s", plain(a.panelBody(PanelGroups, 60, 20, true)))
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

// The acceptance criterion: overwriting an existing group asks first.
func TestOverwriteAsksFirst(t *testing.T) {
	existing := savedGroup("prod", "old-host.example.com")
	a, store := saveApp(t, existing)

	a = pressKey(t, a, "S")
	a = typeInto(t, a, "prod")
	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	// The write runs in a Cmd; ErrExists comes back as its result (issue #225).
	a = settle(t, a, cmd)
	if !a.Saving() {
		t.Fatal("the overwrite question closed without being answered")
	}
	if view := plain(a.View().Content); !strings.Contains(view, "overwrite") {
		t.Fatalf("the modal does not ask:\n%s", view)
	}

	// Nothing has been written yet; answering no leaves it alone.
	a = pressKey(t, a, "n")
	if a.Saving() {
		t.Fatal("answering no left the prompt open")
	}
	sess, _ := store.Load("prod")
	if got := strings.Join(sess.Patterns(), ","); got != "old-host.example.com" {
		t.Fatalf("answering no still overwrote: %q", got)
	}

	// Answering yes writes it.
	a = pressKey(t, a, "S")
	a = typeInto(t, a, "prod")
	model, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, _ = model.(App)
	a = settle(t, a, cmd)
	model, cmd = a.Update(keyMsgFor(t, "y"))
	a, _ = model.(App)
	a = settle(t, a, cmd)

	sess, err := store.Load("prod")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := strings.Join(sess.Patterns(), ","); got != "web-{01..02}.example.com" {
		t.Fatalf("answering yes did not overwrite: %q", got)
	}
}

func TestEscapeAbandonsTheSave(t *testing.T) {
	a, store := saveApp(t)

	a = pressKey(t, a, "S")
	a = typeInto(t, a, "prod")
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.Saving() {
		t.Fatal("escape left the prompt open")
	}
	if store.Exists("prod") {
		t.Fatal("escape wrote the group anyway")
	}
}

// The save prompt owns the keyboard: a group called "x" has to be nameable.
func TestSavePromptOwnsTheKeyboard(t *testing.T) {
	a, _ := saveApp(t)
	a = pressKey(t, a, "S")
	a = pressKey(t, a, "1")

	if a.Panel() != PanelGroups {
		t.Fatalf("a keystroke meant for the name changed the panel to %v", a.Panel())
	}
	if view := plain(a.View().Content); !strings.Contains(view, "name: 1") {
		t.Fatalf("the name did not take the keystroke:\n%s", view)
	}
}

func TestSaveWithAnInvalidNameReportsTheError(t *testing.T) {
	a, store := saveApp(t)

	a = pressKey(t, a, "S")
	a = typeInto(t, a, "not a name")
	model, _ := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	if a.SaveError() == nil {
		t.Fatal("an invalid group name was accepted")
	}
	if store.Exists("not a name") {
		t.Fatal("an invalid name was written")
	}
	if !strings.Contains(plain(a.panelBody(PanelGroups, 60, 20, true)), "not allowed") {
		t.Fatalf("the panel does not report the error:\n%s", plain(a.panelBody(PanelGroups, 60, 20, true)))
	}
}

func TestSaveWithAnEmptyNameDoesNothing(t *testing.T) {
	a, store := saveApp(t)
	a = pressKey(t, a, "S")

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

func TestSaveRunReportsAStoreFailure(t *testing.T) {
	a, _ := saveApp(t)
	a.cfg.Sessions = failingStore{}
	a.panels.groups.store = failingStore{}

	a = pressKey(t, a, "S")
	a = typeInto(t, a, "prod")
	model, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a, ok := model.(App)
	if !ok {
		t.Fatalf("Update returned a %T", model)
	}
	// The failure travels back through the write Cmd's result (issue #225).
	a = settle(t, a, cmd)
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
func (failingStore) Remove(string) error { return errors.New("read-only file system") }

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
	a = typeInto(t, a, "prod")
	a = pressKey(t, a, "enter")

	if a.SaveError() == nil {
		t.Fatal("an empty run saved without an error")
	}
	if !a.Saving() || a.panels.groups.saveInput.Value() != "prod" {
		t.Fatalf("the prompt did not survive: saving=%v value=%q", a.Saving(), a.panels.groups.saveInput.Value())
	}
	if list, _ := store.List(); len(list) != 0 {
		t.Fatalf("an empty run wrote a session: %v", list)
	}
}
