package ui

import (
	"fmt"
	"strings"

	"github.com/TrueDaerk/lazycssh/internal/hosts"
)

// hostPreviewLimit bounds how many resolved names hostPatternPreview lists
// before the total count takes over from naming them.
const hostPreviewLimit = 3

// hostPatternPreview is what pattern would expand to if it were connected
// right now: up to the first three resolved names and the total count, e.g.
// "web01, web02, web03 … (8 hosts)". isErr reports that text is the parse
// error's message instead, which the caller renders in the error style
// rather than the muted preview style.
//
// Only hosts.Expand is called - pure string expansion, no ssh-config lookup,
// no DNS, no dial - so this is cheap enough to run on every keystroke of a
// pattern still being typed (issue #249). An empty pattern previews as no
// text at all: there is nothing to say about nothing typed yet.
func hostPatternPreview(pattern string) (text string, isErr bool) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return "", false
	}

	names, err := hosts.Expand(pattern)
	if err != nil {
		return err.Error(), true
	}

	shown := names
	truncated := len(shown) > hostPreviewLimit
	if truncated {
		shown = shown[:hostPreviewLimit]
	}
	joined := strings.Join(shown, ", ")
	if truncated {
		joined += " …"
	}
	return fmt.Sprintf("%s (%d host%s)", joined, len(names), plural(len(names))), false
}

// hostPatternPreviewLine renders hostPatternPreview's result in the theme's
// preview or error style, or the empty string when there is nothing to show.
func (a App) hostPatternPreviewLine(pattern string) string {
	text, isErr := hostPatternPreview(pattern)
	if text == "" {
		return ""
	}
	style := a.theme.Muted
	if isErr {
		style = a.theme.Failure
	}
	return style.Render("  " + text)
}
