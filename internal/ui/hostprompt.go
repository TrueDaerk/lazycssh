package ui

import "strings"

// newHostPrompt is what the new-host input shows while it is open.
const newHostPrompt = "+"

// maxAliasHints bounds how many ssh-config aliases the open prompt offers. The
// hints are a completion aid, not a browser; a long list would push the status
// lines the panel exists for off the screen.
const maxAliasHints = 5

// ConnectError is the last connect request's resolve error, empty when there
// is none. It clears when the fleet changes.
func (a App) ConnectError() string { return a.connectErr }

// ConnectPromptOpen reports whether the new-host prompt has the keyboard.
func (a App) ConnectPromptOpen() bool { return a.hostInput.Focused() }

// ConnectPromptValue is what has been typed into the new-host prompt.
func (a App) ConnectPromptValue() string { return a.hostInput.Value() }

// aliasHints returns the ssh-config aliases matching what has been typed into
// the new-host prompt, hosts already in the run excluded: offering to connect
// a host again is the duplicate this list exists to avoid. An empty prompt
// matches every alias, which is how the offer stays discoverable.
func (a App) aliasHints() []string {
	typed := strings.ToLower(strings.TrimSpace(a.hostInput.Value()))

	running := make(map[string]bool, len(a.fleetIDs()))
	for _, id := range a.fleetIDs() {
		running[id] = true
	}

	var hints []string
	for _, alias := range a.cfg.ConfigAliases {
		if running[alias] {
			continue
		}
		if typed == "" || strings.Contains(strings.ToLower(alias), typed) {
			hints = append(hints, alias)
		}
	}
	return hints
}

// hostPromptLines renders the open new-host prompt and its completion hints,
// nil while the prompt is closed.
func (a App) hostPromptLines() []string {
	if !a.hostInput.Focused() {
		return nil
	}

	lines := []string{a.theme.Base.Render(newHostPrompt + a.hostInput.Value())}
	hints := a.aliasHints()
	shown := min(len(hints), maxAliasHints)
	for _, alias := range hints[:shown] {
		lines = append(lines, a.theme.Muted.Render("  "+alias))
	}
	if hidden := len(hints) - shown; hidden > 0 {
		lines = append(lines, a.theme.Muted.Render("  …"))
	}
	if shown > 0 {
		lines = append(lines, a.theme.Muted.Render("tab completes, enter connects"))
	}
	return lines
}

// visibleRange returns the half-open range of rows to draw so that the cursor is
// on screen, scrolling by whole rows.
func visibleRange(cursor, total, height int) (first, last int) {
	if height < 1 {
		height = 1
	}
	if total <= height {
		return 0, total
	}

	first = cursor - height/2
	first = clamp(first, 0, total-height)
	return first, first + height
}
