package ui

import (
	"fmt"
	"strings"
)

// The overflow footer: whenever the grid is showing a part - more pages, more
// split chunks, more open sessions - a line under the panes says so, in the
// grid itself. The muted page counter in the status bar is easy to read past,
// and reading the visible panes as "the whole session" is exactly the
// misreading this tool exists to prevent.

// overflowFooterVisible reports whether the footer line is drawn. The check
// tiles the *full* main area: whether the footer itself costs a row must not
// change the answer, or the layout would flap.
func (a App) overflowFooterVisible() bool {
	if a.fullScreen {
		// Full screen is an explicit zoom; alt+z is the way back and the
		// status bar still carries the counts.
		return false
	}
	g := TileGrid(a.layout.Main, a.gridSlots())
	return g.Pages > 1 || a.splitChunks() > 1 || len(a.open) > 1
}

// gridArea is the rectangle the pane grid tiles: the main area, minus the
// footer line when it is drawn.
func (a App) gridArea() Rect {
	r := a.layout.Main
	if a.overflowFooterVisible() && r.Height > 1 {
		r.Height--
	}
	return r
}

// overflowFooter renders the footer line: what is hidden, and the key that
// reaches it. Each part names its own navigation, because "more" means three
// different keys depending on what is hiding it - a page (ctrl+→), a split
// chunk (ctrl+→), another session (the Sessions panel).
func (a App) overflowFooter() string {
	var parts []string

	g := a.grid()
	if g.Pages > 1 {
		// Holes are not hosts: only real machines are worth announcing.
		hidden := len(nonHoles(a.hostIDs())) - len(nonHoles(a.WindowHosts()))
		parts = append(parts, a.theme.StatusWarning.Render(fmt.Sprintf(
			"+%d more host%s — ctrl+→ · page %d/%d",
			hidden, plural(hidden), a.clampedPage(g)+1, g.Pages)))
	}

	if chunks := a.splitChunks(); chunks > 1 {
		hidden := len(nonHoles(a.filteredHosts())) - len(nonHoles(a.visibleHosts()))
		parts = append(parts, a.theme.StatusWarning.Render(fmt.Sprintf(
			"+%d host%s in other chunks — ctrl+→", hidden, plural(hidden))))
	}

	if others := len(a.open) - 1; others > 0 {
		parts = append(parts, a.theme.Muted.Render(fmt.Sprintf(
			"%d more session%s — [3]", others, plural(others))))
	}

	line := strings.Join(parts, a.theme.Muted.Render(" · "))
	return a.theme.Base.
		Width(max(0, a.layout.Main.Width)).
		MaxWidth(max(0, a.layout.Main.Width)).
		MaxHeight(1).
		Render(line)
}
