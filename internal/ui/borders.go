package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// titledBox draws a bordered box with its title embedded in the top border
// line, the way lazygit frames its panels:
//
//	╭─ Hosts [2] ──────╮
//	│ web-01           │
//	╰──────────────────╯
//
// lipgloss has no border-title support, so the top line is assembled by hand
// from the border's character set and the body supplies the other three sides.
// A box shorter than three rows degrades honestly: two rows draw the title
// line and the bottom edge, one row draws the bare title, zero draws nothing.
func titledBox(t Theme, focused bool, width, height int, title, content string) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	if width < 4 || height == 1 {
		// No room for a border at all: the title is the panel.
		return t.PanelTitle(focused).MaxWidth(width).Render(clipTitle(title, width))
	}

	label := t.PanelTitle(focused).Render(clipTitle(title, width-4))
	chars := t.PanelBorderChars(focused)
	edge := t.PanelBorderText(focused)

	fill := max(0, width-2-lipgloss.Width(label))
	top := edge.Render(chars.TopLeft) + label + edge.Render(strings.Repeat(chars.Top, fill)+chars.TopRight)

	if height == 2 {
		bottom := edge.Render(chars.BottomLeft + strings.Repeat(chars.Bottom, max(0, width-2)) + chars.BottomRight)
		return top + "\n" + bottom
	}

	// lipgloss v2 counts the border into Width and Height, so the body block
	// is the full box width and the full height minus the hand-drawn top line.
	body := t.PanelBodyFrame(focused).
		Width(width).
		Height(max(0, height-1)).
		MaxWidth(width).
		MaxHeight(max(0, height-1)).
		Render(content)

	return top + "\n" + body
}

// clipTitle cuts a title to a rune budget so it cannot push the border's
// corner off the edge of the box.
func clipTitle(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit])
}
