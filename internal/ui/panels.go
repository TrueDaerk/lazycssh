package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// panelBody renders the content of a panel into the height it was dealt, by
// dispatching to the panel child model.
//
// It serves the selected panel and the unselected previews alike: the height
// budget, not the selection, decides how much of the content survives.
// focused reports whether this panel is both the selected one and holds the
// sidebar's focus - the only case a list panel draws its cursor row with the
// strong highlight rather than the muted marker (issue #222).
func (a App) panelBody(panel Panel, width, height int, focused bool) string {
	if p := a.panels.byID(panel); p != nil {
		return p.View(focused, width, height)
	}
	switch panel {
	case PanelCommandLog:
		return a.logPanel(width, height, focused)
	default:
		return a.theme.Muted.Render("not implemented yet")
	}
}

// statusPanel answers the question the whole panel exists for: what happens if
// I type right now.
//
// Every number here is read from the [panelContext] snapshot, which the
// message that changed it refreshed inside Update - so the count is as fresh
// as the redraw showing it. A count that could go stale silently is a lie
// waiting to be told, and the one moment it matters is the moment it changed.
type statusPanel struct {
	ctx panelContext

	// targets and workingSet are long-lived collaborators, handed over once
	// at construction; nil means the run has no router or working set yet
	// and the panel says so rather than guessing.
	targets    Targeter
	workingSet WorkingSets
}

// Title is the heading shown in the sidebar.
func (p *statusPanel) Title() string { return PanelStatus.Title() }

// Number is the key that jumps to this panel.
func (p *statusPanel) Number() int { return PanelStatus.Number() }

// Update handles a key routed to the Status panel, which has no keys of its
// own: everything live here is an app-level command the root already handled.
func (p *statusPanel) Update(msg tea.KeyPressMsg) tea.Cmd { return nil }

// MoveCursor and SetCursorRow ignore the mouse: the panel has no list.
func (p *statusPanel) MoveCursor(delta int) {}
func (p *statusPanel) SetCursorRow(row int) {}

// Preview reports that the Status panel has none: the run as a whole is what
// the grid already shows, so the grid keeps the main area.
func (p *statusPanel) Preview(width, height int) (string, string, bool) {
	return "", "", false
}

// View renders the panel body.
func (p *statusPanel) View(focused bool, width, height int) string {
	theme := p.ctx.theme
	var lines []string

	// Open auth prompts are counted first: their questions live in the panes,
	// but panes can be off screen, and a blocked dial must not look idle.
	lines = append(lines, p.ctx.authLines...)

	// The new-host prompt itself is a modal (see modal.go); what stays here
	// is the answer to it, which outlives the box that asked.
	if p.ctx.connectErr != "" {
		// A connect that failed to resolve reports where it was asked for.
		lines = append(lines, theme.Failure.Render(p.ctx.connectErr))
	}

	// The next line answers the panel's question directly: where do my
	// keys go right now.
	switch p.ctx.focus {
	case AreaGrid:
		lines = append(lines, field(theme, "keys go to", p.ctx.focusedHost))
	case AreaBroadcast:
		lines = append(lines, field(theme, "keys go to", "the broadcast targets"))
	default:
		lines = append(lines, field(theme, "keys go to", "lazycssh (commands)"))
	}

	if p.ctx.sessionName != "" {
		lines = append(lines, field(theme, "session", p.ctx.sessionName))
	} else {
		lines = append(lines, field(theme, "session", "(ad hoc)"))
	}

	// Broadcast scope, with the target count. Without a router there is no
	// honest number to show, so it says so instead of showing zero.
	if p.targets != nil {
		scope := p.targets.Describe()
		style := theme.Base
		if p.targets.Warning() {
			style = theme.StatusWarning
		}
		lines = append(lines, style.Render(scope))
	} else {
		lines = append(lines, theme.Muted.Render("BROADCAST (not started)"))
	}

	if p.workingSet != nil {
		lines = append(lines, field(theme, "set", p.workingSet.Describe()))
	}

	counts := p.ctx.counts
	lines = append(lines, field(theme, "hosts", fmt.Sprintf("%d/%d up", counts.Connected, counts.Total)))
	if counts.Pending > 0 {
		lines = append(lines, field(theme, "connecting", fmt.Sprintf("%d", counts.Pending)))
	}
	if counts.Failed > 0 {
		lines = append(lines, theme.Failure.Render(fmt.Sprintf("%d failed", counts.Failed)))
	}
	if counts.Closed > 0 {
		lines = append(lines, field(theme, "closed", fmt.Sprintf("%d", counts.Closed)))
	}

	// Flags come last and are never abbreviated away: anything that weakens a
	// default is worth the line it costs.
	lines = append(lines, activeFlags(theme, p.ctx.insecure, p.ctx.logging, p.targets)...)

	// Wrapped rather than truncated: a status line that silently loses its tail
	// is worse than one that takes a second row, because the tail is where the
	// warnings are.
	return theme.Base.Width(max(0, width)).Render(strings.Join(lines, "\n"))
}

// activeFlags renders the run's default-weakening flags for the status bar.
func (a App) activeFlags() []string {
	return activeFlags(a.theme, a.cfg.Insecure, a.cfg.Logging, a.cfg.Targets)
}

// field renders a labelled value in the root's theme.
func (a App) field(label, value string) string {
	return field(a.theme, label, value)
}
