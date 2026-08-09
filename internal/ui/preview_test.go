package ui

import (
	"errors"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
)

// preview is the main-area preview as View would draw it, without the frame
// around it.
func preview(t *testing.T, a App) string {
	t.Helper()
	body, ok := a.mainPreview()
	if !ok {
		t.Fatal("the focused panel does not preview")
	}
	return plain(body)
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

// The Status panel and the grid keep the grid: it is the detail view of the
// fleet, and the run's summary is about all of it.
func TestStatusAndGridFocusKeepTheGrid(t *testing.T) {
	a, _, _, _ := statusApp(t, "web-01")

	if !strings.Contains(plain(a.View().Content), "1 web-01") {
		t.Fatalf("the Status panel does not show the grid:\n%s", plain(a.View().Content))
	}

	a = pressKey(t, a, "3")
	if !strings.Contains(plain(a.View().Content), "Session — ") {
		t.Fatalf("the Sessions panel does not preview:\n%s", plain(a.View().Content))
	}

	if !strings.Contains(plain(focusGrid(t, a).View().Content), "1 web-01") {
		t.Fatalf("grid focus does not bring the grid back:\n%s", plain(focusGrid(t, a).View().Content))
	}
}

// The preview lives inside Layout.Main at every size: it never spills into the
// sidebar's columns or past the broadcast bar, and a terminal too small for the
// interface still gets the too-small line.
func TestPreviewStaysInsideMain(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "srv-{01..40}.example.com"))

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
	a, _ := groupsStoreApp(t, savedGroup("prod", "a", "b", "c", "d", "e", "f", "g", "h"))
	a = resize(t, a, 120, 14)

	view := plain(a.View().Content)
	if !strings.Contains(view, "more") {
		t.Fatalf("the clipped preview does not say what it hid:\n%s", view)
	}
}

// errRead is the failure a group row carries when its file could not be read.
var errRead = errors.New("open prod.yaml: permission denied")

// Clicking the preview brings the grid back. The pane arithmetic still says a
// pane is under the pointer, but that pane is not what is drawn there, so the
// click must not close it or start typing into it.
func TestClickOnThePreviewReturnsToTheGrid(t *testing.T) {
	a, _ := groupsStoreApp(t, savedGroup("prod", "web-01"))
	before := a.FocusedHost()

	a, _ = click(t, a, a.Layout().Main.X+2, a.Layout().Main.Y+1)
	if a.Focus() != AreaGrid {
		t.Fatalf("the click left focus on %s", a.Focus())
	}
	if a.FocusedHost() != before {
		t.Fatalf("the click moved the pane focus to %q, want %q", a.FocusedHost(), before)
	}
	if !strings.Contains(plain(a.View().Content), "web-01") {
		t.Fatalf("the grid did not come back:\n%s", plain(a.View().Content))
	}
}
