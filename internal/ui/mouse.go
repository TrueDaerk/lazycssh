package ui

import (
	tea "charm.land/bubbletea/v2"
)

// wheelStep is how many wrapped lines one wheel notch scrolls a pane.
const wheelStep = 3

// handleClick resolves a left click: panes take the keyboard (clicking a
// terminal is focusing it), the [x] button closes or removes, sidebar rows
// move their panel's cursor, and the broadcast bar takes the keyboard like a
// pane does.
func (a App) handleClick(x, y int) (tea.Model, tea.Cmd) {
	switch a.layout.regionAt(x, y) {
	case RegionMain:
		index, ok := a.paneUnder(x, y)
		if !ok {
			return a, nil
		}
		if cell, found := a.paneCell(index); found && paneCloseHit(cell, x, y) {
			if id := a.hostIDAt(index); id != "" {
				return a, a.closeOrRemove(id)
			}
			return a, nil
		}
		a.paneIndex = index
		a = a.enterPane().followFocus()
		return a, nil

	case RegionSidebar:
		heights := SidebarHeights(a.layout.Sidebar.Height, len(Panels()), int(a.panel))
		panel, bodyRow, ok := sidebarPanelAt(heights, y-a.layout.Sidebar.Y)
		if !ok {
			return a, nil
		}
		a.focus = AreaSidebar
		a.panel = Panels()[panel]
		if bodyRow >= 0 {
			a = a.moveCursorToVisibleRow(a.panel, bodyRow)
		}
		return a, nil

	case RegionBroadcast:
		a.focus = AreaBroadcast
		return a, nil
	}
	return a, nil
}

// handleWheel scrolls what is under the pointer, not what has focus: the pane
// under it by a few lines, a sidebar panel's cursor by one row.
func (a App) handleWheel(msg tea.MouseWheelMsg) App {
	delta := 0
	switch msg.Button {
	case tea.MouseWheelUp:
		delta = +1
	case tea.MouseWheelDown:
		delta = -1
	default:
		return a
	}

	switch a.layout.regionAt(msg.X, msg.Y) {
	case RegionMain:
		if index, ok := a.paneUnder(msg.X, msg.Y); ok {
			return a.scrollHostBy(index, delta*wheelStep)
		}
	case RegionSidebar:
		heights := SidebarHeights(a.layout.Sidebar.Height, len(Panels()), int(a.panel))
		if panel, _, ok := sidebarPanelAt(heights, msg.Y-a.layout.Sidebar.Y); ok {
			return a.movePanelCursor(Panels()[panel], -delta)
		}
	}
	return a
}

// paneUnder resolves a point in the main area to a host index.
func (a App) paneUnder(x, y int) (int, bool) {
	g := a.grid()
	return paneAt(g, a.layout.Main, a.clampedPage(g), len(a.hostIDs()), a.paneIndex, a.fullScreen, x, y)
}

// paneCell is the rect the pane at index is drawn in right now.
func (a App) paneCell(index int) (Rect, bool) {
	if a.fullScreen {
		return a.layout.Main, index == a.paneIndex
	}
	return a.grid().Cell(index)
}

// hostIDAt is the host identifier at a pane index, or empty.
func (a App) hostIDAt(index int) string {
	ids := a.hostIDs()
	if index < 0 || index >= len(ids) {
		return ""
	}
	return ids[index]
}

// moveCursorToVisibleRow puts a panel's cursor on the body row that was
// clicked, mapped through the same window arithmetic the panel renders with.
func (a App) moveCursorToVisibleRow(panel Panel, bodyRow int) App {
	switch panel {
	case PanelGroups:
		a.groupCursor = clamp(bodyRow, 0, max(0, len(a.groupList)-1))
	case PanelSessions:
		a.sessionCursor = clamp(bodyRow, 0, max(0, len(a.open)-1))
	case PanelCommandLog:
		a.logCursor = clamp(bodyRow, 0, max(0, len(a.logEntries())-1))
	}
	return a
}

// movePanelCursor nudges one panel's cursor without changing the selection or
// the focus - the wheel browses, it does not commit.
func (a App) movePanelCursor(panel Panel, delta int) App {
	switch panel {
	case PanelGroups:
		a.groupCursor = clamp(a.groupCursor+delta, 0, max(0, len(a.groupList)-1))
	case PanelSessions:
		a.sessionCursor = clamp(a.sessionCursor+delta, 0, max(0, len(a.open)-1))
	case PanelCommandLog:
		a.logCursor = clamp(a.logCursor+delta, 0, max(0, len(a.logEntries())-1))
	}
	return a
}

// sidebarBodyHeight is the body height of the selected panel's box, matching
// what renderSidebar hands to panelBody.
func (a App) sidebarBodyHeight() int {
	heights := SidebarHeights(a.layout.Sidebar.Height, len(Panels()), int(a.panel))
	return max(1, heights[int(a.panel)]-2)
}
