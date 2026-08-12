package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// The main area is lazygit's detail view: it shows the selection of whatever
// side panel has the keyboard. In lazycssh the fleet's detail view is the pane
// grid, and the grid outranks every preview: a host's output is live, so it
// leaves the screen when its session ends, never because the user walked the
// cursor through another panel (issue #290). The list panels' preview - what
// enter in Groups, Sessions or the Command log would act on (issue #218) - is
// therefore the empty grid's tenant: it takes the area only while there is no
// pane to draw there, which is exactly the argumentless start the preview was
// most useful in. With hosts on screen the sidebar keeps saying what the cursor
// row is: the selected panel expands to its full body, and the others preview
// inline (issue #186).
//
// Every preview is read-only and built from model state alone - the group rows
// the store was last read into, the fleet snapshot, the in-memory command log.
// Nothing here dials, reads a file or touches live session state, so a preview
// cannot disagree with the frame it is drawn in.

const (
	// previewOverlayMargin is how much of the grid stays uncovered on each
	// side of the popup, so it reads as floating over the panes rather than
	// replacing them.
	previewOverlayMargin = 3

	// minPreviewOverlay is the smallest popup drawn: below this the margin is
	// given up before the content is.
	minPreviewOverlay = 10
)

// hasPreview reports whether a sidebar panel previews its cursor row.
func hasPreview(p Panel) bool {
	switch p {
	case PanelGroups, PanelSessions, PanelCommandLog, PanelDiff:
		return true
	default:
		return false
	}
}

// mainPreview renders the focused panel's preview into the main area, and
// reports whether the main area belongs to a preview at all. A false answer
// leaves the grid where it is.
func (a App) mainPreview() (string, bool) {
	if a.focus != AreaSidebar || !hasPreview(a.panel) {
		return "", false
	}
	if len(nonHoles(a.hostIDs())) > 0 {
		// There are panes to draw: they keep the area, focus or no focus. Holes
		// do not count - a slot whose host left the fleet has no output to hide.
		return "", false
	}
	r := a.layout.Main
	if r.Empty() {
		// Nothing to draw into: the too-small guard has already spoken.
		return "", true
	}

	// The box eats two columns for its border and two more for the body's
	// padding; the hand-drawn title line takes the first row.
	title, body := a.panelPreview(a.panel, max(1, r.Width-4), max(1, r.Height-2))
	return titledBox(a.theme, false, r.Width, r.Height, title, body), true
}

// previewOverlay renders the focused panel's preview as a popup over the
// frame - what `p` asks for - and where to composite it. It floats over the
// main area, centred there rather than in the terminal, so the sidebar the
// cursor is in stays readable beside it; the grid keeps running underneath and
// any key closes it, the way the help overlay behaves.
func (a App) previewOverlay() (string, int, int, bool) {
	if !a.showPreview || a.focus != AreaSidebar || !hasPreview(a.panel) {
		return "", 0, 0, false
	}
	r := a.layout.Main
	if r.Empty() {
		return "", 0, 0, false
	}

	// A popup, not a takeover: it leaves a margin of the grid showing on every
	// side of itself, and shrinks with the area rather than spilling out of it.
	w := max(minPreviewOverlay, r.Width-2*previewOverlayMargin)
	h := max(minPreviewOverlay, r.Height-2*previewOverlayMargin)
	w, h = min(w, r.Width), min(h, r.Height)

	title, body := a.panelPreview(a.panel, max(1, w-4), max(1, h-3))
	body = lipgloss.JoinVertical(lipgloss.Left, body, a.theme.Muted.Render("any key closes this"))
	box := titledBox(a.theme, true, w, h, title, body)

	return box, r.X + max(0, (r.Width-w)/2), r.Y + max(0, (r.Height-h)/2), true
}

// panelPreview dispatches per panel, the way panelBody does for the sidebar,
// and returns the box title and its content.
func (a App) panelPreview(panel Panel, width, height int) (string, string) {
	if p := a.panels.byID(panel); p != nil {
		if title, body, ok := p.Preview(width, height); ok {
			return title, body
		}
	}
	return "Preview", ""
}

// fitLines renders lines into the height it was dealt, wrapping each at the
// width and replacing the tail that does not fit with a counter. A preview
// that silently drops rows tells the user a short list is the whole list.
func fitLines(theme *Theme, width, height int, lines []string) string {
	avail := max(1, height)

	blocks := make([]string, 0, len(lines))
	heights := make([]int, 0, len(lines))
	used := 0
	shown := 0
	for _, line := range lines {
		block := theme.Base.Width(max(0, width)).Render(line)
		h := lipgloss.Height(block)
		if used+h > avail {
			break
		}
		blocks = append(blocks, block)
		heights = append(heights, h)
		used += h
		shown++
	}

	if shown < len(lines) {
		// The counter takes its line back from the bottom of what fits, so it
		// is never itself the row that gets clipped away.
		notice := theme.Muted.Render(fmt.Sprintf("+%d more", len(lines)-shown))
		for used+1 > avail && len(blocks) > 0 {
			used -= heights[len(heights)-1]
			blocks = blocks[:len(blocks)-1]
			heights = heights[:len(heights)-1]
			shown--
			notice = theme.Muted.Render(fmt.Sprintf("+%d more", len(lines)-shown))
		}
		if used+1 <= avail {
			blocks = append(blocks, notice)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}
