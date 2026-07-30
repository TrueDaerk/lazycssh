package ui

import (
	"fmt"
	"strings"
)

// panelBody renders the content of the selected panel.
//
// Only the selected panel has a body: a sidebar that showed five open panels at
// once on an 80-column terminal would show none of them usefully.
func (a App) panelBody(panel Panel, width, height int) string {
	switch panel {
	case PanelStatus:
		return a.statusPanel(width)
	case PanelGroups:
		return a.groupsPanel(width, height)
	case PanelSessions:
		return a.sessionsPanel(width, height)
	case PanelCommandLog:
		return a.logPanel(width, height)
	default:
		return a.theme.Muted.Render("not implemented yet")
	}
}

// statusPanel answers the question the whole panel exists for: what happens if
// I type right now.
//
// Every number here is read from the model's fleet snapshot, which the fleet
// event that changed it refreshed inside Update - so the count is as fresh as
// the redraw showing it. A count that could go stale silently is a lie waiting
// to be told, and the one moment it matters is the moment it changed.
func (a App) statusPanel(width int) string {
	var lines []string

	// The host key question outranks everything: it owns the keyboard while
	// it is open, and a security decision must be visible while it is made.
	lines = append(lines, a.hostKeyQuestionLines()...)

	// The open new-host prompt comes first: while it has the keyboard it is
	// the thing being interacted with, and its hints must not be pushed off a
	// short panel.
	lines = append(lines, a.hostPromptLines()...)
	if a.connectErr != "" {
		// A connect that failed to resolve reports where it was asked for.
		lines = append(lines, a.theme.Failure.Render(a.connectErr))
	}

	// The next line answers the panel's question directly: where do my
	// keys go right now.
	switch a.focus {
	case AreaGrid:
		lines = append(lines, a.field("keys go to", a.FocusedHost()))
	case AreaBroadcast:
		lines = append(lines, a.field("keys go to", "the broadcast targets"))
	default:
		lines = append(lines, a.field("keys go to", "lazycssh (commands)"))
	}

	if a.cfg.SessionName != "" {
		lines = append(lines, a.field("session", a.cfg.SessionName))
	} else {
		lines = append(lines, a.field("session", "(ad hoc)"))
	}

	// Broadcast scope, with the target count. Without a router there is no
	// honest number to show, so it says so instead of showing zero.
	if a.cfg.Targets != nil {
		scope := a.cfg.Targets.Describe()
		style := a.theme.Base
		if a.cfg.Targets.Warning() {
			style = a.theme.StatusWarning
		}
		lines = append(lines, style.Render(scope))
	} else {
		lines = append(lines, a.theme.Muted.Render("BROADCAST (not started)"))
	}

	if a.cfg.WorkingSet != nil {
		lines = append(lines, a.field("set", a.cfg.WorkingSet.Describe()))
	}

	counts := a.counts()
	lines = append(lines, a.field("hosts", fmt.Sprintf("%d/%d up", counts.Connected, counts.Total)))
	if counts.Pending > 0 {
		lines = append(lines, a.field("connecting", fmt.Sprintf("%d", counts.Pending)))
	}
	if counts.Failed > 0 {
		lines = append(lines, a.theme.Failure.Render(fmt.Sprintf("%d failed", counts.Failed)))
	}
	if counts.Closed > 0 {
		lines = append(lines, a.field("closed", fmt.Sprintf("%d", counts.Closed)))
	}

	// Flags come last and are never abbreviated away: anything that weakens a
	// default is worth the line it costs.
	for _, flag := range a.activeFlags() {
		lines = append(lines, flag)
	}

	// Wrapped rather than truncated: a status line that silently loses its tail
	// is worse than one that takes a second row, because the tail is where the
	// warnings are.
	return a.theme.Base.Width(max(0, width)).Render(strings.Join(lines, "\n"))
}

// activeFlags renders every state that weakens a default, in the warning style.
func (a App) activeFlags() []string {
	var flags []string
	if a.cfg.Insecure {
		flags = append(flags, a.theme.StatusInsecure.Render("HOST KEYS UNVERIFIED"))
	}
	if a.cfg.Logging {
		flags = append(flags, a.theme.StatusWarning.Render("SESSION LOGGING ON"))
	}
	if a.cfg.Targets != nil && a.cfg.Targets.Warning() {
		flags = append(flags, a.theme.StatusWarning.Render("BROADCASTING TO EVERY HOST"))
	}
	if a.connectedOnly {
		// Hidden panes are hidden targets: the filter must be on screen for
		// as long as it narrows what a keystroke reaches.
		flags = append(flags, a.theme.StatusWarning.Render("CONNECTED HOSTS ONLY"))
	}
	return flags
}

// field renders a labelled value.
func (a App) field(label, value string) string {
	return a.theme.Muted.Render(label+": ") + a.theme.Base.Render(value)
}
