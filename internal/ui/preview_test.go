package ui

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// preview is the focused panel's preview - title and body - as mainPreview
// would draw it into an empty grid. The content is the panel's answer either
// way; where it is allowed on screen is TestPreviewOnlyTakesAnEmptyGrid's
// business (issue #290).
func preview(t *testing.T, a App) string {
	t.Helper()
	if a.focus != AreaSidebar || !hasPreview(a.panel) {
		t.Fatal("the focused panel does not preview")
	}
	title, body := a.panelPreview(a.panel, max(1, a.layout.Main.Width-4), max(1, a.layout.Main.Height-2))
	return plain(title + "\n" + body)
}

// withoutHosts empties the grid of a fleetless fixture, which is where the
// main-area preview lives since issue #290.
func withoutHosts(t *testing.T, a App) App {
	t.Helper()
	a.cfg.Hosts = nil
	a.open, a.active = nil, -1
	if len(a.hostIDs()) != 0 {
		t.Fatalf("the fixture still has hosts: %v", a.hostIDs())
	}
	return a
}

// The Groups cursor drives the main area: moving it, with no action at all,
// changes what the preview describes (issue #218).
func TestGroupsPreviewFollowsTheCursor(t *testing.T) {
	prod := savedGroup("prod", "srv1-{01..04}.example.com")
	prod.Description = "the production web tier"
	a, _ := groupsStoreApp(t, prod, savedGroup("staging", "stage-01.example.com"))

	view := preview(t, a)
	for _, want := range []string{"Group — prod", "the production web tier", "srv1-{01..04}.example.com"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the preview does not show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "stage-01.example.com") {
		t.Fatalf("the preview shows a group the cursor is not on:\n%s", view)
	}

	view = preview(t, pressKey(t, a, "j"))
	if !strings.Contains(view, "Group — staging") || !strings.Contains(view, "stage-01.example.com") {
		t.Fatalf("the preview did not follow the cursor:\n%s", view)
	}
	if strings.Contains(view, "srv1-{01..04}.example.com") {
		t.Fatalf("the preview still shows the previous group:\n%s", view)
	}
}

// A group whose file could not be read says why in the preview: the list only
// has room for "(unreadable)".
func TestGroupsPreviewShowsTheReadError(t *testing.T) {
	a, _ := groupsStoreApp(t)
	a.panels.groups.rows = []groupRow{{Name: "broken", Hosts: -1, Err: errRead}}

	view := preview(t, a)
	if !strings.Contains(view, "Group — broken") || !strings.Contains(view, errRead.Error()) {
		t.Fatalf("the preview does not explain the unreadable group:\n%s", view)
	}
}

// An empty Groups panel previews nothing rather than the last selection.
func TestGroupsPreviewWithoutGroups(t *testing.T) {
	a, _ := groupsStoreApp(t)
	if got := preview(t, a); !strings.Contains(got, "no group selected") {
		t.Fatalf("an empty Groups panel previews something:\n%s", got)
	}
}

// The Sessions cursor previews the session's hosts and their states, from the
// fleet snapshot alone.
func TestSessionsPreviewShowsHostsAndStates(t *testing.T) {
	a, fleet := openTwo(t)
	fleet.connect(t, "web-01")
	a = syncFleet(t, a)

	// The cursor starts on the run's own session; front is the next row.
	a = pressKey(t, a, "j")
	view := preview(t, a)
	for _, want := range []string{"Session — front", "1/2 up", "web-01", "connected", "web-02", "pending"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the preview does not show %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "db-01") {
		t.Fatalf("the preview shows another session's host:\n%s", view)
	}

	view = preview(t, pressKey(t, a, "j"))
	if !strings.Contains(view, "Session — back") || !strings.Contains(view, "db-01") {
		t.Fatalf("the preview did not follow the cursor:\n%s", view)
	}
	if strings.Contains(view, "web-02") {
		t.Fatalf("the preview still shows the previous session:\n%s", view)
	}
}

// The Command log preview answers "what exactly did I send": the whole
// command, when it went, and how far it reached.
func TestCommandLogPreviewShowsTheWholeEntry(t *testing.T) {
	const long = "systemctl restart nginx && journalctl -u nginx --since -5m"

	a, log := logApp(t, 0)
	log.Record(long, broadcast.ModeFleet, hostIDs(40))
	log.Record("uptime", broadcast.ModeAll, hostIDs(2))

	first := preview(t, a)
	second := preview(t, pressKey(t, a, "j"))
	if first == second {
		t.Fatalf("moving the cursor did not change the preview:\n%s", first)
	}

	both := first + "\n" + second
	for _, want := range []string{long, "40 hosts", "fleet", "uptime", "2 hosts", "2026-07-28 14:05:09"} {
		if !strings.Contains(both, want) {
			t.Fatalf("no cursor position previews %q:\n%s", want, both)
		}
	}
	// One entry at a time: the preview is the cursor row, not the log.
	if strings.Contains(first, long) && strings.Contains(first, "uptime") {
		t.Fatalf("the preview shows two entries at once:\n%s", first)
	}
}

// An empty log previews the panel's own answer rather than a blank box.
func TestCommandLogPreviewWithoutEntries(t *testing.T) {
	a, _ := logApp(t, 0)
	if got := preview(t, a); !strings.Contains(got, "nothing sent yet") {
		t.Fatalf("an empty log previews something:\n%s", got)
	}
}

// The acceptance criterion of issue #290: with hosts on screen, walking the
// focus through every side panel leaves the grid exactly as it was. A pane goes
// away when its session ends, not when the user looks elsewhere.
func TestFocusChangesKeepTheGrid(t *testing.T) {
	a, fleet, _, _ := statusApp(t, "web-01", "web-02", "web-03")
	fleet.connect(t, "web-01")
	fleet.sessions["web-01"].Emit("hello from web-01\r\n")
	a = syncFleet(t, a)

	want := a.hostIDs()
	for _, panel := range Panels() {
		b := pressKey(t, a, strconv.Itoa(panel.Number()))
		if b.Focus() != AreaSidebar || b.Panel() != panel {
			t.Fatalf("[%d] did not focus %s", panel.Number(), panel.Title())
		}
		if got := b.hostIDs(); !slices.Equal(got, want) {
			t.Fatalf("focusing %s renders panes %v, want %v", panel.Title(), got, want)
		}
		view := plain(b.View().Content)
		for _, host := range want {
			if !strings.Contains(view, host) {
				t.Fatalf("focusing %s hid %s:\n%s", panel.Title(), host, view)
			}
		}
		if !strings.Contains(view, "hello from web-01") {
			t.Fatalf("focusing %s hid the live output:\n%s", panel.Title(), view)
		}
	}
}

// The grid only gives the main area up when it has nothing to draw: the
// argumentless start still previews the cursor row, and one connected host
// takes the area back (issue #218 inside issue #290).
func TestPreviewOnlyTakesAnEmptyGrid(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "srv-01.example.com"))

	if _, ok := a.mainPreview(); ok {
		t.Fatal("a panel preview took the main area from a grid with hosts")
	}
	if !strings.Contains(plain(a.View().Content), "web-01") {
		t.Fatalf("the grid is not on screen:\n%s", plain(a.View().Content))
	}

	empty := withoutHosts(t, a)
	if _, ok := empty.mainPreview(); !ok {
		t.Fatal("an empty grid did not hand the main area to the preview")
	}
	if !strings.Contains(plain(empty.View().Content), "Group — prod") {
		t.Fatalf("the empty grid does not preview the cursor row:\n%s", plain(empty.View().Content))
	}
}

// The detail the grid no longer gives its area up for is still reachable: p
// floats the cursor row's preview over the panes, and any key closes it again.
func TestRowPreviewFloatsOverTheGrid(t *testing.T) {
	a, fleet := openTwo(t)
	fleet.connect(t, "web-01")
	a = pressKey(t, syncFleet(t, a), "j")

	a = pressKey(t, a, "p")
	if !a.PreviewVisible() {
		t.Fatal("p did not open the preview")
	}
	view := plain(a.View().Content)
	if !strings.Contains(view, "Session — front") || !strings.Contains(view, "connected") {
		t.Fatalf("the popup does not show the cursor row:\n%s", view)
	}
	// A popup, not a takeover: the sidebar and panes around it stay drawn.
	if !strings.Contains(view, "Sessions [3]") || !strings.Contains(view, "db-01") {
		t.Fatalf("the popup replaced the frame instead of floating over it:\n%s", view)
	}

	a = pressKey(t, a, "j")
	if a.PreviewVisible() {
		t.Fatal("a key did not close the preview")
	}
}

// A panel with no preview leaves p alone rather than opening an empty box.
func TestRowPreviewNeedsAPreviewingPanel(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")
	if a.Panel() != PanelStatus {
		t.Fatalf("the fixture starts on %s", a.Panel().Title())
	}
	if a = pressKey(t, a, "p"); a.PreviewVisible() {
		t.Fatal("the Status panel opened a row preview")
	}
}

// The popup stays inside the main area at every size the frame survives: it
// floats over the grid, so it must not spill into the sidebar or past the
// bottom of the terminal.
func TestRowPreviewStaysInsideMain(t *testing.T) {
	base, _ := groupsStoreApp(t, savedGroup("prod", "srv-{01..40}.example.com"))
	a := pressKey(t, base, "p")

	for _, size := range [][2]int{{200, 60}, {120, 40}, {80, 24}, {60, 12}, {40, 8}, {30, 5}} {
		a = resize(t, a, size[0], size[1])
		if a.Layout().TooSmall {
			continue
		}
		box, x, y, ok := a.previewOverlay()
		if !ok {
			t.Fatalf("%dx%d: the popup is gone", size[0], size[1])
		}
		r := a.Layout().Main
		if lipgloss.Width(box) > r.Width || x < r.X || x+lipgloss.Width(box) > r.X+r.Width {
			t.Fatalf("%dx%d: the popup spans columns %d..%d of a main area at %d..%d",
				size[0], size[1], x, x+lipgloss.Width(box), r.X, r.X+r.Width)
		}
		if lipgloss.Height(box) > r.Height || y < r.Y || y+lipgloss.Height(box) > r.Y+r.Height {
			t.Fatalf("%dx%d: the popup spans rows %d..%d of a main area at %d..%d",
				size[0], size[1], y, y+lipgloss.Height(box), r.Y, r.Y+r.Height)
		}
		if got, want := strings.Count(plain(a.View().Content), "\n")+1, a.Layout().Height; got != want {
			t.Fatalf("%dx%d: the frame is %d lines tall, want %d", size[0], size[1], got, want)
		}
	}
}

// Without hosts and without a previewing panel, the empty state still says what
// to do next rather than showing a blank main area.
func TestEmptyStateSurvivesTheGridPriority(t *testing.T) {
	base, _ := groupsStoreApp(t)
	a := withoutHosts(t, pressKey(t, base, "1"))
	if _, ok := a.mainPreview(); ok {
		t.Fatal("the Status panel previews")
	}
	if !strings.Contains(plain(a.View().Content), "no hosts") {
		t.Fatalf("the empty state is gone:\n%s", plain(a.View().Content))
	}
}

// The preview lives inside Layout.Main at every size: it never spills into the
// sidebar's columns or past the broadcast bar, and a terminal too small for the
// interface still gets the too-small line.
func TestPreviewStaysInsideMain(t *testing.T) {
	base, _ := groupsStoreApp(t, savedGroup("prod", "srv-{01..40}.example.com"))
	a := withoutHosts(t, base)

	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 12}, {40, 8}, {30, 5}, {24, 4}, {20, 3}} {
		a = resize(t, a, size[0], size[1])
		view := a.View().Content
		if a.Layout().TooSmall {
			if !strings.Contains(plain(view), "terminal too small") {
				t.Fatalf("%dx%d: the too-small guard did not fire:\n%s", size[0], size[1], plain(view))
			}
			continue
		}
		preview, ok := a.mainPreview()
		if !ok {
			t.Fatalf("%dx%d: the Groups panel does not preview", size[0], size[1])
		}
		if w := lipgloss.Width(preview); w > a.Layout().Main.Width {
			t.Fatalf("%dx%d: the preview is %d columns wide in a %d-column main area",
				size[0], size[1], w, a.Layout().Main.Width)
		}
		if h := lipgloss.Height(preview); h > a.Layout().Main.Height {
			t.Fatalf("%dx%d: the preview is %d lines tall in a %d-line main area",
				size[0], size[1], h, a.Layout().Main.Height)
		}
		if got, want := strings.Count(plain(view), "\n")+1, a.Layout().Height; got != want {
			t.Fatalf("%dx%d: the frame is %d lines tall, want %d", size[0], size[1], got, want)
		}
	}
}

// A preview that does not fit says how much it is hiding: a clipped host list
// must not read as the whole group.
func TestPreviewCountsTheRowsItCannotShow(t *testing.T) {
	base, _ := groupsStoreApp(t, savedGroup("prod", "a", "b", "c", "d", "e", "f", "g", "h"))
	a := resize(t, withoutHosts(t, base), 120, 14)

	view := plain(a.View().Content)
	if !strings.Contains(view, "more") {
		t.Fatalf("the clipped preview does not say what it hid:\n%s", view)
	}
}

// errRead is the failure a group row carries when its file could not be read.
var errRead = errors.New("open prod.yaml: permission denied")

// A click on the preview is a click on the grid's area, not on a pane: the pane
// arithmetic still says a slot is under the pointer, but nothing is drawn
// there, so the click must not close a pane or start typing into one.
func TestClickOnThePreviewDoesNotHitAPane(t *testing.T) {
	base, _ := groupsStoreApp(t, savedGroup("prod", "web-01"))
	a := withoutHosts(t, base)
	before := a.FocusedHost()

	a, _ = click(t, a, a.Layout().Main.X+2, a.Layout().Main.Y+1)
	if a.FocusedHost() != before {
		t.Fatalf("the click moved the pane focus to %q, want %q", a.FocusedHost(), before)
	}
	if _, ok := a.mainPreview(); !ok {
		t.Fatalf("the click took the empty grid's preview away:\n%s", plain(a.View().Content))
	}
}
