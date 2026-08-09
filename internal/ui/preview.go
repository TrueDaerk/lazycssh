package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/ssh"
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
	case PanelGroups, PanelSessions, PanelCommandLog:
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
	switch panel {
	case PanelGroups:
		return a.groupPreview(width, height)
	case PanelSessions:
		return a.sessionPreview(width, height)
	case PanelCommandLog:
		return a.commandPreview(width, height)
	default:
		return "Preview", ""
	}
}

// groupPreview describes the saved group under the Groups cursor: what it is
// for, and which hosts opening it would dial.
func (a App) groupPreview(width, height int) (string, string) {
	if len(a.groupList) == 0 {
		return "Group", a.fitLines(width, height, []string{a.theme.Muted.Render("no group selected")})
	}
	row := a.groupList[clamp(a.groupCursor, 0, len(a.groupList)-1)]

	title := "Group — " + row.Name
	if row.Err != nil {
		// An unreadable file says why here rather than only "(unreadable)" in
		// the list: this is the space the reason fits in.
		return title, a.fitLines(width, height, []string{a.theme.Failure.Render(row.Err.Error())})
	}

	lines := []string{a.field("hosts", fmt.Sprintf("%d", row.Hosts))}
	if row.Description != "" {
		lines = append(lines, a.field("description", row.Description))
	}
	if open := a.openSessionNamed(row.Name); open {
		lines = append(lines, a.theme.Selected.Render("open as a session"))
	}
	lines = append(lines, "")
	if len(row.Patterns) == 0 {
		lines = append(lines, a.theme.Muted.Render("no host patterns"))
	}
	for _, p := range row.Patterns {
		lines = append(lines, a.theme.Base.Render("  "+p))
	}
	return title, a.fitLines(width, height, lines)
}

// openSessionNamed reports whether a group is currently open as a session.
func (a App) openSessionNamed(name string) bool {
	for _, s := range a.open {
		if s.Name == name {
			return true
		}
	}
	return false
}

// sessionPreview describes the open session under the Sessions cursor: whether
// it is the one on screen, and how each of its hosts is doing.
func (a App) sessionPreview(width, height int) (string, string) {
	if len(a.open) == 0 {
		return "Session", a.fitLines(width, height, []string{a.theme.Muted.Render("no open sessions")})
	}
	index := clamp(a.sessionCursor, 0, len(a.open)-1)
	s := a.open[index]

	where := "background"
	if index == a.active {
		where = "foreground"
	}
	if s.Ending {
		where += ", ending"
	}

	up := 0
	hosts := make([]string, 0, len(s.Hosts))
	for _, id := range s.Hosts {
		if id == "" {
			// A hole is a grid position a closed host left behind, not a host.
			continue
		}
		state := a.state(id)
		if state == ssh.StateConnected {
			up++
		}
		line := a.theme.Base.Render("  "+id+"  ") + a.theme.State(state).Render(state.String())
		if err := a.stateErr(id); err != "" {
			line += a.theme.Failure.Render("  " + err)
		}
		hosts = append(hosts, line)
	}

	lines := []string{
		a.field("state", where),
		a.field("hosts", fmt.Sprintf("%d/%d up", up, len(hosts))),
		"",
	}
	if len(hosts) == 0 {
		lines = append(lines, a.theme.Muted.Render("no hosts"))
	}
	lines = append(lines, hosts...)
	return "Session — " + s.Name, a.fitLines(width, height, lines)
}

// commandPreview shows the whole of the command under the Command log cursor:
// the list truncates a long command to its row, and "what exactly did I send"
// is the question the log exists to answer.
func (a App) commandPreview(width, height int) (string, string) {
	entries := a.logEntries()
	if len(entries) == 0 {
		if a.cfg.CommandLog == nil {
			return "Command", a.fitLines(width, height, []string{a.theme.Muted.Render("no command log")})
		}
		return "Command", a.fitLines(width, height, []string{a.theme.Muted.Render("nothing sent yet")})
	}
	entry := entries[clamp(a.logCursor, 0, len(entries)-1)]

	scope := fmt.Sprintf("%s → %d host%s", entry.Mode, entry.Targets, plural(entry.Targets))
	scopeLine := a.field("scope", scope)
	if entry.Mode == broadcast.ModeFleet {
		// A command that went to every host reads here the way it read in the
		// list: the audit trail should feel like the decision did.
		scopeLine = a.theme.StatusWarning.Render(scope)
	}

	lines := []string{
		a.field("sent", entry.At.Format("2006-01-02 15:04:05")),
		scopeLine,
		"",
		a.theme.Base.Render(entry.Command),
	}
	return "Command", a.fitLines(width, height, lines)
}

// fitLines renders lines into the height it was dealt, wrapping each at the
// width and replacing the tail that does not fit with a counter. A preview
// that silently drops rows tells the user a short list is the whole list.
func (a App) fitLines(width, height int, lines []string) string {
	avail := max(1, height)

	blocks := make([]string, 0, len(lines))
	heights := make([]int, 0, len(lines))
	used := 0
	shown := 0
	for _, line := range lines {
		block := a.theme.Base.Width(max(0, width)).Render(line)
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
		notice := a.theme.Muted.Render(fmt.Sprintf("+%d more", len(lines)-shown))
		for used+1 > avail && len(blocks) > 0 {
			used -= heights[len(heights)-1]
			blocks = blocks[:len(blocks)-1]
			heights = heights[:len(heights)-1]
			shown--
			notice = a.theme.Muted.Render(fmt.Sprintf("+%d more", len(lines)-shown))
		}
		if used+1 <= avail {
			blocks = append(blocks, notice)
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}
