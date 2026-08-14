---
type: concept
title: Theme and styles
description: The one place styles live, the palette they are built from, and why colour is never the only carrier of meaning.
resource: internal/ui/theme.go
tags: [ui, lipgloss, styles, accessibility]
timestamp: 2026-08-14T00:00:00Z
---

# Theme and styles

Every style the interface draws with lives in `internal/ui/theme.go`. A view that builds its own
has forked the theme, and the next change to the palette will miss it — so a test walks the
package and fails on a `lipgloss.NewStyle(` or a `lipgloss.Color(` anywhere else.

## Palette

Colours are named once, in `Palette`, and referred to by role rather than by value:

| Role | Used for |
|------|----------|
| `Text`, `Muted` | body text and secondary text |
| `Border`, `Focus` | panel and pane frames, focused frames |
| `Accent` | selection markers, key bindings in the help |
| `Success`, `Warning`, `Danger` | connected / connecting / failed, and warnings |
| `Highlight` | the row under the list cursor |

`DarkPalette` is the default — a tool for servers is nearly always on a dark background — and
`LightPalette` is the same vocabulary tuned for a light one. Bubbletea reports the terminal's
background colour as a message; the root model passes what it learned into `NewTheme`.

## Degrading

Colours are written as truecolour hex. lipgloss downsamples to what the terminal actually
supports (256 colours, 16 colours), so a hex value is a statement of intent rather than a
requirement.

For a terminal with no colour at all, `Options.NoColor` drops colour entirely. That is why
**no view may encode meaning in colour alone**:

- the focused frame, normally a colour change at the same border weight (lazygit style, in the
  green `Focus` colour), becomes a **thicker** border when colour is off,
- the insecure-host-key marker is **reverse video**, not just red,
- the list cursor is reverse video when colour is off; the muted cursor row (below) is
  **underlined** instead, so the two never collapse into the same look,
- a failed host is **bold** as well as red,
- the simulated remote cursor (`RemoteCursor`, see [TUI shell](./tui.md#the-caret)) is **reverse
  video**, tinted with `Accent` when there is colour: it has to be findable over whatever colours
  the remote painted that cell in, on the light and the dark palette alike.

Tests assert each of these, because the failure mode — a monochrome terminal where the user
cannot tell which panel has focus or that host key checking is off — is silent otherwise.

## Helpers

`Theme` exposes the small mappings the views would otherwise each reinvent:

```go
th.State(ssh.StateFailed)     // style for a connection state
th.PanelFrame(focused)        // panel border, focused or not
th.PaneFrame(focused)         // host pane border
th.PanelTitle(focused)        // panel heading
th.PanelBodyFrame(focused)    // titled box: the frame minus its top edge
th.PanelBorderChars(focused)  // titled box: the border character set for the hand-drawn top line
th.PanelBorderText(focused)   // titled box: the style those characters are drawn in
th.ExitStatus(code)           // zero is quiet, non-zero never is
th.ListCursor(focused)        // list cursor row: strong highlight or the muted marker
```

`ListCursor` is what `groupLine`, `openSessionLine` and `logLine` call for the row under the
cursor. The strong `Cursor` style — background highlight — is reserved for a list panel that is
both selected *and* holds the sidebar's focus, lazygit style; every other case (a collapsed
preview box, or the selected panel while the grid or broadcast bar has the keyboard) gets
`CursorMuted` instead, which keeps the row findable without claiming a keystroke would land there.
`panelBody` and the panel renderers thread that focus flag down from `renderSidebar`, which already
computes it for the panel's own border (issue #222).

The three `PanelBody*`/`PanelBorder*` helpers exist for `titledBox`, which draws the lazygit-style
top border line with the title inside it by hand — lipgloss has no border-title support. The
border characters stay a `lipgloss.Border` from the theme, so no view invents its own frame.

`State` falls back to the muted style for a state it does not know, rather than panicking: a new
state added to the transport must not be able to take the interface down.
