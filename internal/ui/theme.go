// Package ui holds the bubbletea models, the layout and everything that draws.
//
// Nothing in this package dials, reads or writes a host. It renders state and
// turns key presses into intent; the transport lives in internal/ssh.
package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// Palette is the colour vocabulary of the interface. Every colour used anywhere
// is named here, so a change is one edit rather than a hunt through the views.
//
// Colours are given as hex. lipgloss downsamples to whatever the terminal
// actually supports - 256 colours, 16 colours - so a truecolour value is a
// description of intent, not a requirement. On a terminal that reports no colour
// at all, [Options.NoColor] drops them entirely and the interface falls back to
// borders, bold and reverse video, which is why no view is allowed to encode
// meaning in colour alone.
type Palette struct {
	Text      color.Color
	Muted     color.Color
	Border    color.Color
	Focus     color.Color
	Accent    color.Color
	Success   color.Color
	Warning   color.Color
	Danger    color.Color
	Highlight color.Color
}

// DarkPalette is the default, because a terminal multiplexer for servers is
// almost always on a dark background.
func DarkPalette() Palette {
	return Palette{
		Text:      lipgloss.Color("#d0d0d0"),
		Muted:     lipgloss.Color("#7a7a7a"),
		Border:    lipgloss.Color("#4a4a4a"),
		Focus:     lipgloss.Color("#7aa2f7"),
		Accent:    lipgloss.Color("#bb9af7"),
		Success:   lipgloss.Color("#9ece6a"),
		Warning:   lipgloss.Color("#e0af68"),
		Danger:    lipgloss.Color("#f7768e"),
		Highlight: lipgloss.Color("#283457"),
	}
}

// LightPalette is the same vocabulary tuned for a light background.
func LightPalette() Palette {
	return Palette{
		Text:      lipgloss.Color("#1c1c1c"),
		Muted:     lipgloss.Color("#6c6c6c"),
		Border:    lipgloss.Color("#b0b0b0"),
		Focus:     lipgloss.Color("#2f5fd0"),
		Accent:    lipgloss.Color("#7a3fbf"),
		Success:   lipgloss.Color("#2e7d32"),
		Warning:   lipgloss.Color("#8a5a00"),
		Danger:    lipgloss.Color("#b3261e"),
		Highlight: lipgloss.Color("#dbe4ff"),
	}
}

// Options describe the terminal the interface is drawn on.
type Options struct {
	// Dark selects the dark palette. Bubbletea reports the terminal's
	// background through a message; the model passes what it learned here.
	Dark bool
	// NoColor drops every colour. Meaning then has to come from borders, bold
	// and markers, which is the reason no view encodes state in colour alone.
	NoColor bool
}

// Theme holds every style the interface draws with.
//
// It is the only place in this package that builds a [lipgloss.Style]. A view
// that wants a style it does not find here adds it here; a style literal
// anywhere else is a defect, and a test enforces that.
type Theme struct {
	// Palette is what the styles were built from, kept so a view can colour a
	// value it computes rather than inventing a colour.
	Palette Palette
	// NoColor reports whether colours were dropped.
	NoColor bool

	// Base is plain body text.
	Base lipgloss.Style
	// Muted is secondary text: counts, hints, anything not the subject.
	Muted lipgloss.Style
	// Title is a panel heading.
	Title lipgloss.Style
	// TitleFocused is the heading of the focused panel.
	TitleFocused lipgloss.Style

	// Panel is the frame around an unfocused panel.
	Panel lipgloss.Style
	// PanelFocused is the frame around the focused panel. The border is both
	// coloured and thicker, so focus survives a terminal without colour.
	PanelFocused lipgloss.Style

	// Pane is the frame around an unfocused host pane.
	Pane lipgloss.Style
	// PaneFocused is the frame around the focused host pane.
	PaneFocused lipgloss.Style
	// PaneFailed frames a pane whose last command exited non-zero.
	PaneFailed lipgloss.Style
	// PaneFocusedFailed frames the focused pane of a failing host: the thick
	// border keeps saying "focus", the colour keeps saying "failed".
	PaneFocusedFailed lipgloss.Style
	// PaneHeader is the per-pane status line.
	PaneHeader lipgloss.Style

	// StatusBar is the bar along the bottom.
	StatusBar lipgloss.Style
	// StatusWarning marks a state that weakens a default: the fleet broadcast
	// mode, session logging.
	StatusWarning lipgloss.Style
	// StatusInsecure marks host key verification being off. It is reverse
	// video on purpose: it must be visible on a terminal with no colour.
	StatusInsecure lipgloss.Style

	// Selected marks a host in the selection.
	Selected lipgloss.Style
	// Cursor marks the row under the cursor in a list.
	Cursor lipgloss.Style

	// Key renders a key binding in the help.
	Key lipgloss.Style
	// Desc renders the description of a binding.
	Desc lipgloss.Style
	// HelpOverlay frames the full help.
	HelpOverlay lipgloss.Style

	// StatePending, StateDialing, StateAuthenticating, StateConnected,
	// StateFailed and StateClosed render a host's connection state.
	StatePending        lipgloss.Style
	StateDialing        lipgloss.Style
	StateAuthenticating lipgloss.Style
	StateConnected      lipgloss.Style
	StateFailed         lipgloss.Style
	StateClosed         lipgloss.Style

	// Match highlights a scrollback line the active search matches.
	Match lipgloss.Style

	// Failure highlights a pane or row whose host failed, or whose last
	// command exited non-zero.
	Failure lipgloss.Style
	// ExitOK renders a zero exit status.
	ExitOK lipgloss.Style
}

// NewTheme builds the styles for a terminal.
func NewTheme(opts Options) Theme {
	palette := LightPalette()
	if opts.Dark {
		palette = DarkPalette()
	}

	t := Theme{Palette: palette, NoColor: opts.NoColor}

	fg := func(c color.Color) lipgloss.Style {
		s := lipgloss.NewStyle()
		if opts.NoColor {
			return s
		}
		return s.Foreground(c)
	}

	t.Base = fg(palette.Text)
	t.Muted = fg(palette.Muted)
	t.Title = fg(palette.Text).Bold(true).Padding(0, 1)
	t.TitleFocused = fg(palette.Focus).Bold(true).Padding(0, 1)

	border := func(b lipgloss.Border, c color.Color) lipgloss.Style {
		s := lipgloss.NewStyle().Border(b)
		if opts.NoColor {
			return s
		}
		return s.BorderForeground(c)
	}

	// The focused frame is a thicker border as well as a brighter one: focus
	// has to be unmistakable on a terminal that renders no colour at all.
	t.Panel = border(lipgloss.RoundedBorder(), palette.Border)
	t.PanelFocused = border(lipgloss.ThickBorder(), palette.Focus)
	t.Pane = border(lipgloss.NormalBorder(), palette.Border)
	t.PaneFocused = border(lipgloss.ThickBorder(), palette.Focus)
	t.PaneFailed = border(lipgloss.NormalBorder(), palette.Danger)
	t.PaneFocusedFailed = border(lipgloss.ThickBorder(), palette.Danger)
	t.PaneHeader = fg(palette.Muted).Padding(0, 1)

	t.StatusBar = fg(palette.Text).Padding(0, 1)
	t.StatusWarning = fg(palette.Warning).Bold(true).Padding(0, 1)
	t.StatusInsecure = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	if !opts.NoColor {
		t.StatusInsecure = t.StatusInsecure.Foreground(palette.Danger)
	}

	t.Selected = fg(palette.Accent).Bold(true)
	t.Cursor = lipgloss.NewStyle().Bold(true)
	if !opts.NoColor {
		t.Cursor = t.Cursor.Background(palette.Highlight).Foreground(palette.Text)
	} else {
		t.Cursor = t.Cursor.Reverse(true)
	}

	t.Key = fg(palette.Accent).Bold(true)
	t.Desc = fg(palette.Muted)
	t.HelpOverlay = border(lipgloss.RoundedBorder(), palette.Focus).Padding(1, 2)

	t.StatePending = fg(palette.Muted)
	t.StateDialing = fg(palette.Warning)
	t.StateAuthenticating = fg(palette.Warning)
	t.StateConnected = fg(palette.Success)
	t.StateFailed = fg(palette.Danger).Bold(true)
	t.StateClosed = fg(palette.Muted)

	// Bold and underlined as well as coloured, so a match survives a terminal
	// without colour.
	t.Match = fg(palette.Warning).Bold(true).Underline(true)

	t.Failure = fg(palette.Danger).Bold(true)
	t.ExitOK = fg(palette.Success)

	return t
}

// State returns the style for a host's connection state.
func (t Theme) State(state ssh.State) lipgloss.Style {
	switch state {
	case ssh.StatePending:
		return t.StatePending
	case ssh.StateDialing:
		return t.StateDialing
	case ssh.StateAuthenticating:
		return t.StateAuthenticating
	case ssh.StateConnected:
		return t.StateConnected
	case ssh.StateFailed:
		return t.StateFailed
	case ssh.StateClosed:
		return t.StateClosed
	default:
		return t.Muted
	}
}

// PanelFrame returns the frame for a panel, focused or not.
func (t Theme) PanelFrame(focused bool) lipgloss.Style {
	if focused {
		return t.PanelFocused
	}
	return t.Panel
}

// PaneFrame returns the frame for a host pane. A failing host's border turns
// the danger colour so it reads at a glance across a 20-pane grid; the header
// carries the exit code in text as well, because colour alone is not allowed
// to carry meaning.
func (t Theme) PaneFrame(focused, failed bool) lipgloss.Style {
	switch {
	case focused && failed:
		return t.PaneFocusedFailed
	case focused:
		return t.PaneFocused
	case failed:
		return t.PaneFailed
	default:
		return t.Pane
	}
}

// PanelTitle returns the heading style for a panel, focused or not.
func (t Theme) PanelTitle(focused bool) lipgloss.Style {
	if focused {
		return t.TitleFocused
	}
	return t.Title
}

// ExitStatus returns the style for a command's exit status: a non-zero status
// is a failure, and failures are never rendered quietly.
func (t Theme) ExitStatus(code int) lipgloss.Style {
	if code == 0 {
		return t.ExitOK
	}
	return t.Failure
}
