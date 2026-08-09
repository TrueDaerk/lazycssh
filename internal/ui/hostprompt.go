package ui

import "strings"

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

// connectModal is the new-host dialog: the pattern being typed, and the
// ssh-config aliases it still matches under it.
func (a App) connectModal() modal {
	m := a.prompt("Connect host", "host", a.hostInput, "enter connects · esc cancels")

	hints := a.aliasHints()
	shown := min(len(hints), maxAliasHints)
	for _, alias := range hints[:shown] {
		m.Lines = append(m.Lines, a.theme.Muted.Render("  "+alias))
	}
	if hidden := len(hints) - shown; hidden > 0 {
		m.Lines = append(m.Lines, a.theme.Muted.Render("  …"))
	}
	if shown > 0 {
		m.Hint = "tab completes · enter connects · esc cancels"
	}
	return m
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
