package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

func TestNewThemeUsesTheRequestedPalette(t *testing.T) {
	dark := NewTheme(Options{Dark: true})
	light := NewTheme(Options{})

	if dark.Palette == light.Palette {
		t.Fatal("the dark and light palettes are identical")
	}
	if dark.Palette != DarkPalette() {
		t.Fatal("Dark: true did not select the dark palette")
	}
	if light.Palette != LightPalette() {
		t.Fatal("Dark: false did not select the light palette")
	}
}

// Colour is never the only carrier of meaning: on a terminal without it, focus
// still has a distinct border and the insecure marker is still reverse video.
func TestNoColorStillDistinguishesFocusAndDanger(t *testing.T) {
	th := NewTheme(Options{NoColor: true})

	if !th.NoColor {
		t.Fatal("NoColor was not recorded")
	}
	if th.PanelFocused.GetBorderStyle() == th.Panel.GetBorderStyle() {
		t.Fatal("the focused panel border is not distinguishable without colour")
	}
	if th.PaneFocused.GetBorderStyle() == th.Pane.GetBorderStyle() {
		t.Fatal("the focused pane border is not distinguishable without colour")
	}
	if !th.StatusInsecure.GetReverse() {
		t.Fatal("the insecure marker is invisible without colour")
	}
	if !th.Cursor.GetReverse() {
		t.Fatal("the list cursor is invisible without colour")
	}
	if !th.StateFailed.GetBold() {
		t.Fatal("a failed host is not marked without colour")
	}
}

func TestNoColorSetsNoForeground(t *testing.T) {
	th := NewTheme(Options{NoColor: true, Dark: true})

	// Rendering must not emit a colour escape sequence when colour is off.
	for name, rendered := range map[string]string{
		"base":    th.Base.Render("x"),
		"muted":   th.Muted.Render("x"),
		"failed":  th.StateFailed.Render("x"),
		"warning": th.StatusWarning.Render("x"),
	} {
		if strings.Contains(rendered, "38;2") || strings.Contains(rendered, "38;5") {
			t.Fatalf("%s emitted a colour sequence with NoColor: %q", name, rendered)
		}
	}
}

func TestStateStyleForEveryState(t *testing.T) {
	th := NewTheme(Options{Dark: true})

	states := []ssh.State{
		ssh.StatePending, ssh.StateDialing, ssh.StateAuthenticating,
		ssh.StateConnected, ssh.StateFailed, ssh.StateClosed,
	}
	for _, state := range states {
		if got := th.State(state).Render(state.String()); got == "" {
			t.Fatalf("state %v rendered nothing", state)
		}
	}

	// An unknown state falls back rather than panicking.
	if got := th.State(ssh.State(42)).Render("x"); got == "" {
		t.Fatal("an unknown state rendered nothing")
	}

	// Connected and failed must not look the same; that is the whole point of
	// the Hosts panel.
	if th.State(ssh.StateConnected).Render("x") == th.State(ssh.StateFailed).Render("x") {
		t.Fatal("connected and failed hosts render identically")
	}
}

func TestFrameHelpers(t *testing.T) {
	th := NewTheme(Options{Dark: true})

	// With colour available, focus is a colour change at the same border
	// weight, lazygit style.
	if th.PanelFrame(true).GetBorderStyle() != th.PanelFrame(false).GetBorderStyle() {
		t.Fatal("the focused panel changed border weight; focus is colour only")
	}
	if th.PanelFrame(true).GetBorderTopForeground() == th.PanelFrame(false).GetBorderTopForeground() {
		t.Fatal("PanelFrame does not distinguish focus")
	}
	if th.PaneFrame(true, false).GetBorderStyle() != th.PaneFrame(false, false).GetBorderStyle() {
		t.Fatal("the focused pane changed border weight; focus is colour only")
	}
	if th.PaneFrame(true, false).GetBorderTopForeground() == th.PaneFrame(false, false).GetBorderTopForeground() {
		t.Fatal("PaneFrame does not distinguish focus")
	}
	if th.PaneFrame(false, true).GetBorderTopForeground() == th.PaneFrame(false, false).GetBorderTopForeground() {
		t.Fatal("PaneFrame does not distinguish a failing host")
	}
	if th.PaneFrame(true, true).GetBorderStyle() != th.PaneFrame(true, false).GetBorderStyle() {
		t.Fatal("a failing focused pane lost the focus border")
	}
	if th.PaneFrame(true, true).GetBorderTopForeground() == th.PaneFrame(true, false).GetBorderTopForeground() {
		t.Fatal("a focused failing pane lost the failure colour")
	}
	if th.PanelTitle(true).Render("x") == th.PanelTitle(false).Render("x") {
		t.Fatal("PanelTitle does not distinguish focus")
	}
}

func TestExitStatusStyle(t *testing.T) {
	th := NewTheme(Options{Dark: true})

	if th.ExitStatus(0).Render("0") == th.ExitStatus(1).Render("0") {
		t.Fatal("a failing exit status renders like a successful one")
	}
	if th.ExitStatus(255).Render("x") != th.ExitStatus(1).Render("x") {
		t.Fatal("two failing exit statuses render differently")
	}
}

// The rule this file exists for: every style lives in theme.go. A view that
// builds its own has forked the theme, and the next change to the palette will
// miss it.
func TestNoStyleLiteralsOutsideTheTheme(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	for _, e := range entries {
		name := e.Name()
		// theme.go owns the styles; this file names them to check for them.
		if e.IsDir() || !strings.HasSuffix(name, ".go") ||
			name == "theme.go" || name == "theme_test.go" {
			continue
		}
		body, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("ReadFile %s: %v", name, err)
		}
		if strings.Contains(string(body), "lipgloss.NewStyle(") {
			t.Errorf("%s builds a lipgloss style; every style belongs in theme.go", name)
		}
		if strings.Contains(string(body), "lipgloss.Color(") {
			t.Errorf("%s names a colour; every colour belongs in the palette in theme.go", name)
		}
	}
}
