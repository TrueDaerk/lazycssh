package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/ssh"
)

// The visibility filters: ctrl+a narrows the grid to the hosts that can take
// input right now, ctrl+s splits it into chunks of a chosen size, and the
// broadcast limit follows both, so a keystroke never reaches a pane the user
// cannot see. The filters are views, not removals - a host that reconnects
// reappears without a keypress: its state change is a fleet event, the event
// refreshes the model's snapshot inside Update, and the redraw recomputes the
// visible list from it.

// ConnectedOnly reports whether the connected-only filter is on.
func (a App) ConnectedOnly() bool { return a.connectedOnly }

// SplitSize is the chosen chunk size, 0 while the split is off.
func (a App) SplitSize() int { return a.splitSize }

// filteredHosts is the session's hosts after the connected-only filter,
// before the split: the list the split chunks.
func (a App) filteredHosts() []string {
	ids := a.sessionHosts()
	if !a.connectedOnly {
		return ids
	}
	var out []string
	for _, id := range ids {
		if a.state(id) == ssh.StateConnected {
			out = append(out, id)
		}
	}
	return out
}

// visibleHosts is the session's hosts after every visibility filter: what the
// grid draws and what all/selected broadcast may reach.
func (a App) visibleHosts() []string {
	ids := a.filteredHosts()
	if a.splitSize <= 0 || len(ids) == 0 {
		return ids
	}
	// The chunk index is clamped here rather than stored clamped: the host
	// list can shrink under a chosen chunk, and the last chunk is where a
	// user who was past the end lands.
	index := clamp(a.splitChunk, 0, a.splitChunks()-1)
	first := index * a.splitSize
	return ids[first:min(first+a.splitSize, len(ids))]
}

// splitChunks is how many chunks the split produces right now, 0 while the
// split is off.
func (a App) splitChunks() int {
	if a.splitSize <= 0 {
		return 0
	}
	n := len(a.filteredHosts())
	if n == 0 {
		return 1
	}
	return (n + a.splitSize - 1) / a.splitSize
}

// toggleConnectedOnly flips the filter. The pane focus is kept by host
// identity where possible - the pane may move, the host does not change its
// name - and the PTYs are asked to match the new grid shape.
func (a App) toggleConnectedOnly() (App, tea.Cmd) {
	focused := a.FocusedHost()
	a.connectedOnly = !a.connectedOnly
	a.page = 0
	a = a.resetGridSlots().refocus(focused).followFocus().syncBroadcastLimit()
	return a, gridChanged()
}

// beginSplit opens the chunk-size prompt. It opens empty - enter on an empty
// prompt clears the split, which is the promised way out - and esc keeps
// whatever was in force.
func (a App) beginSplit() App {
	a.splitInput.SetValue("")
	a.splitInput.Focus()
	return a
}

// handleSplitKey drives the chunk-size prompt, which owns the keyboard while
// it is open. enter applies the typed size - empty or 0 clears the split -
// and esc leaves the split as it was.
func (a App) handleSplitKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		raw := strings.TrimSpace(a.splitInput.Value())
		a.splitInput.SetValue("")
		a.splitInput.Blur()
		size := 0
		if raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 {
				// Not a number: the split stays what it was, and the prompt
				// closes rather than trapping the user over a typo.
				return a, nil
			}
			size = parsed
		}
		return a.applySplit(size)
	case "esc":
		a.splitInput.SetValue("")
		a.splitInput.Blur()
		return a, nil
	}

	var cmd tea.Cmd
	a.splitInput, cmd = a.splitInput.Update(msg)
	return a, cmd
}

// applySplit sets the chunk size - 0 clears the split - and shows the first
// chunk, with the pane focus on it.
func (a App) applySplit(size int) (App, tea.Cmd) {
	a.splitSize = size
	a.splitChunk = 0
	a.page = 0
	a.paneIndex = 0
	a = a.resetGridSlots().syncBroadcastLimit().syncFocusTarget()
	return a, gridChanged()
}

// stepView shows the next or previous screenful (issue #147): pages inside
// the current split chunk first, then the neighbouring chunk, wrapping at
// both ends. With the split off it degenerates to page navigation with wrap.
// Wrapping is safe here because paging never changes who receives a
// keystroke, and a chunk change runs the same resync a direct chunk step
// always did - the status bar's page and SPLIT indicators say where the wrap
// landed.
func (a App) stepView(delta int) (App, tea.Cmd) {
	g := a.grid()
	chunks := a.splitChunks() // 0 while the split is off
	if g.Pages <= 1 && chunks <= 1 {
		// One screenful: nothing to step, and no flicker.
		return a, nil
	}
	page := a.clampedPage(g)
	if delta > 0 && page < g.Pages-1 {
		return a.pageBy(+1).syncFocusTarget(), nil
	}
	if delta < 0 && page > 0 {
		return a.pageBy(-1).syncFocusTarget(), nil
	}
	return a.stepChunkWrapped(delta)
}

// stepChunkWrapped moves the split chunk by delta with wrap-around, landing
// on the first page of the new chunk going forward and on its last page going
// backward. Without a multi-chunk split it wraps the pages instead.
func (a App) stepChunkWrapped(delta int) (App, tea.Cmd) {
	chunks := a.splitChunks()
	if chunks <= 1 {
		g := a.grid()
		if delta > 0 {
			a.page = 0
		} else {
			a.page = g.Pages - 1
		}
		a.paneIndex = clamp(a.page*g.PerPage, 0, len(a.hostIDs())-1)
		return a.syncFocusTarget(), nil
	}

	a.splitChunk = (clamp(a.splitChunk, 0, chunks-1) + delta + chunks) % chunks
	a.page = 0
	a.paneIndex = 0
	a = a.resetGridSlots().syncBroadcastLimit().syncFocusTarget()
	if delta < 0 {
		if g := a.grid(); g.Pages > 1 {
			a.page = g.Pages - 1
			a.paneIndex = clamp(a.page*g.PerPage, 0, len(a.hostIDs())-1)
			a = a.syncFocusTarget()
		}
	}
	return a, gridChanged()
}

// splitLabel is the status bar's split indicator, empty while the split is
// off.
func (a App) splitLabel() string {
	if a.splitSize <= 0 {
		return ""
	}
	visible := len(a.visibleHosts())
	return fmt.Sprintf("SPLIT %d/%d (%d host%s)",
		clamp(a.splitChunk, 0, a.splitChunks()-1)+1, a.splitChunks(), visible, plural(visible))
}

// renderSplitLine draws the chunk-size prompt in place of the status bar
// while the size is being typed.
func (a App) renderSplitLine() string {
	line := a.theme.Base.Render("split: "+a.splitInput.Value()) + " " +
		a.theme.Muted.Render("→ panes per view, empty or 0 shows all — enter applies, esc keeps")
	return a.theme.StatusBar.
		Width(a.layout.Width).
		MaxHeight(StatusBarHeight).
		Render(line)
}
