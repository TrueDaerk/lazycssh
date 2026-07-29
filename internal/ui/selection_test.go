package ui

import (
	"strings"
	"testing"
)

// selectionApp builds an app on the Hosts panel with a router, a live fleet and
// a command line.
func selectionApp(t *testing.T, names ...string) (App, *fakeFleet, Selector) {
	t.Helper()

	a, fleet, router, _ := statusApp(t, names...)
	router.Attach(fleet)
	// Not the Groups panel: there n and d belong to the groups, deliberately
	// shadowing the global selection keys.
	return pressKey(t, a, "1"), fleet, router
}

// selected returns the toggled hosts, in host order.
func selected(t *testing.T, a App) string {
	t.Helper()

	var out []string
	for _, id := range a.hostIDs() {
		if a.cfg.Targets.IsSelected(id) {
			out = append(out, id)
		}
	}
	return strings.Join(out, ",")
}

// The acceptance criterion: selection is expressible by keystroke in the Hosts
// panel.
func TestSelectionKeys(t *testing.T) {
	a, fleet, _ := selectionApp(t, "web-01", "web-02", "db-01")
	fleet.connect(t, "web-01")
	fleet.fail(t, "db-01")

	a = pressKey(t, a, "a")
	if got := selected(t, a); got != "web-01,web-02,db-01" {
		t.Fatalf("a selected %q", got)
	}

	a = pressKey(t, a, "c")
	if got := selected(t, a); got != "" {
		t.Fatalf("c left %q selected", got)
	}

	a = pressKey(t, a, "alt+ ") // toggle the focused pane's host
	a = pressKey(t, a, "i")
	if got := selected(t, a); got != "web-02,db-01" {
		t.Fatalf("i left %q selected", got)
	}

	a = pressKey(t, a, "c")
	a = pressKey(t, a, "u")
	if got := selected(t, a); got != "web-01" {
		t.Fatalf("u selected %q, want the host that is up", got)
	}

	a = pressKey(t, a, "c")
	a = pressKey(t, a, "d")
	if got := selected(t, a); got != "web-02,db-01" {
		t.Fatalf("d selected %q, want the hosts that are down", got)
	}
}

// The status bar count updates as the selection changes.
func TestSelectionCountReachesTheStatusBar(t *testing.T) {
	a, fleet, _ := selectionApp(t, "web-01", "web-02", "web-03")
	for _, id := range []string{"web-01", "web-02", "web-03"} {
		fleet.connect(t, id)
	}

	a = pressKey(t, a, "a")
	if !strings.Contains(plain(a.View().Content), "3 selected") {
		t.Fatalf("the status bar does not report the selection:\n%s", plain(a.View().Content))
	}

	a = pressKey(t, a, "B") // switch to the selected scope
	if !strings.Contains(plain(a.View().Content), "BROADCAST selected (3/3 up)") {
		t.Fatalf("the scope does not follow the selection:\n%s", plain(a.View().Content))
	}

	a = pressKey(t, a, "c")
	if !strings.Contains(plain(a.View().Content), "BROADCAST selected (0/0 up)") {
		t.Fatalf("clearing did not update the scope:\n%s", plain(a.View().Content))
	}
}

// The other half of the acceptance criterion: selection by pattern from the
// command line.
func TestSelectionByPatternFromTheCommandLine(t *testing.T) {
	a, fleet, _ := selectionApp(t, "web-01", "web-02", "db-01")
	fleet.connect(t, "web-01")

	a = typeCommand(t, a, "/select web-*")
	a, _ = enter(t, a)

	if got := selected(t, a); got != "web-01,web-02" {
		t.Fatalf("/select web-* selected %q", got)
	}
	if !strings.Contains(a.LastDelivery(), "selected 2 matching web-*") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}

	a = typeCommand(t, a, "/deselect web-02")
	a, _ = enter(t, a)
	if got := selected(t, a); got != "web-01" {
		t.Fatalf("/deselect left %q", got)
	}
}

// A selection instruction is for lazycssh: nothing reaches the hosts and
// nothing is recorded as a command.
func TestSelectionCommandsAreNotSentToHosts(t *testing.T) {
	a, sender, router, log := cmdApp(t, "web-01", "web-02")

	a = typeCommand(t, a, "/select all")
	a, cmd := enter(t, a)

	if len(sender.sent) != 0 {
		t.Fatalf("a selection instruction reached the hosts: %q", sender.sent)
	}
	if cmd != nil {
		t.Fatal("a selection instruction reported a send")
	}
	if log.Len() != 0 {
		t.Fatal("a selection instruction was recorded as a command")
	}
	if router.SelectionCount() != 2 {
		t.Fatalf("SelectionCount() = %d", router.SelectionCount())
	}
	// It is still in the command line history, because retyping it is common.
	if got := strings.Join(a.History(), ","); got != "/select all" {
		t.Fatalf("History() = %q", got)
	}
}

// A command that merely starts with the word "select" is a shell builtin and
// must reach the hosts unchanged.
func TestSelectAsAShellCommandIsSent(t *testing.T) {
	a, sender, router, _ := cmdApp(t, "web-01")

	a = typeCommand(t, a, "select opt in a b c; do echo $opt; done")
	a, _ = enter(t, a)

	if len(sender.sent) != 1 {
		t.Fatalf("the shell command was intercepted: %q", sender.sent)
	}
	if router.SelectionCount() != 0 {
		t.Fatalf("the shell command changed the selection: %d", router.SelectionCount())
	}
}

func TestSelectionCommandVariants(t *testing.T) {
	tests := []struct {
		line string
		want string
	}{
		{"/select all", "web-01,web-02,db-01"},
		{"/select", "web-01,web-02,db-01"},
		{"/select up", "web-01"},
		{"/select down", "web-02,db-01"},
		{"/select db-*", "db-01"},
		{"/select none", ""},
		{"/SELECT web-01", "web-01"},
	}

	for _, tc := range tests {
		t.Run(tc.line, func(t *testing.T) {
			a, fleet, _ := selectionApp(t, "web-01", "web-02", "db-01")
			fleet.connect(t, "web-01")

			a = typeCommand(t, a, tc.line)
			a, _ = enter(t, a)

			if got := selected(t, a); got != tc.want {
				t.Fatalf("%q selected %q, want %q", tc.line, got, tc.want)
			}
		})
	}
}

func TestSelectionCommandErrors(t *testing.T) {
	a, _, _ := selectionApp(t, "web-01")

	tests := []struct {
		line string
		want string
	}{
		{"/select web-[01", "pattern"},
		{"/deselect", "needs a pattern"},
		{"/nonsense", "unknown command"},
	}
	for _, tc := range tests {
		a = typeCommand(t, a, tc.line)
		a, _ = enter(t, a)
		if !strings.Contains(a.LastDelivery(), tc.want) {
			t.Fatalf("%q reported %q, want it to mention %q", tc.line, a.LastDelivery(), tc.want)
		}
	}
}

func TestSelectionWithoutARouter(t *testing.T) {
	a := resize(t, NewApp(Config{Hosts: []string{"h1"}, Theme: Options{Dark: true}}), 120, 40)
	a = pressKey(t, a, "2")

	// The keys are consumed rather than falling through to something unrelated.
	for _, k := range []string{"a", "i", "c", "u", "d"} {
		a = pressKey(t, a, k)
	}
	if a.Panel() != PanelGroups {
		t.Fatalf("a selection key changed the panel to %v", a.Panel())
	}

	a = typeCommand(t, a, "/select all")
	a, _ = enter(t, a)
	if !strings.Contains(a.LastDelivery(), "no host selection yet") {
		t.Fatalf("LastDelivery() = %q", a.LastDelivery())
	}
}

// The working set is one of the things a selection can be built from.
func TestSelectTheWorkingSet(t *testing.T) {
	a, _, ws := func() (App, *fakeFleet, interface{ ApplySpec(string, []string) error }) {
		a, fleet, router, ws := statusApp(t, "web-01", "web-02", "web-03", "web-04")
		router.Attach(fleet)
		return pressKey(t, a, "2"), fleet, ws
	}()

	if err := ws.ApplySpec("first 2", nil); err != nil {
		t.Fatalf("ApplySpec: %v", err)
	}

	a = typeCommand(t, a, "/select set")
	a, _ = enter(t, a)

	if got := selected(t, a); got != "web-01,web-02" {
		t.Fatalf("/select set selected %q", got)
	}
}
