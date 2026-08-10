package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

// The main area is lazygit's detail view: it shows the selection of whatever
// side panel has the keyboard. In lazycssh the fleet's detail view is the pane
// grid, so the grid keeps the area whenever the grid or the Status panel - the
// panel that describes the run as a whole - has focus. The list panels get a
// preview instead: moving the cursor in Groups, Sessions or the Command log
// tells the user what enter would act on, before they press it (issue #218).
//
// Every preview is read-only and built from model state alone - the group rows
// the store was last read into, the fleet snapshot, the in-memory command log.
// Nothing here dials, reads a file or touches live session state, so a preview
// cannot disagree with the frame it is drawn in.

// hasPreview reports whether a sidebar panel previews its cursor row in the
// main area.
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
