package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/TrueDaerk/lazycssh/internal/broadcast"
	"github.com/TrueDaerk/lazycssh/internal/outdiff"
)

// The Output diff panel answers "which machines disagree" (issue #46): after a
// command goes out over the command line, the panel groups the hosts by the
// output they produced since the send, and lists the distinct answers instead
// of making the user read forty panes.
//
// The comparison window starts at the send: [App.markDiff] records each
// target's scrollback length when the command leaves, and the tail past that
// mark is what gets compared. Comparison is over normalized output - see
// [outdiff.Normalize] - so a prompt carrying the hostname does not put every
// host in its own group.
//
// Grouping re-reads the marked hosts' scrollback, so it runs only while the
// panel is selected ([App.syncPanels]); an unselected panel shows the groups
// as of the last time it was open, which is the same "rows I was last handed"
// contract the Groups panel has with its store.

// DiffSelectMsg asks the program to make one variant's hosts the selection, so
// "these three disagree" turns into a target set the user can act on with B.
type DiffSelectMsg struct {
	// Hosts are the variant's hosts, in host order.
	Hosts []string
}

// diffPanel is the Output diff child model: the cursor over the variants.
type diffPanel struct {
	ctx  panelContext
	keys *KeyMap

	// command is the command under comparison, empty before the first send.
	command string
	// marked is how many hosts the comparison window covers.
	marked int
	// variants are the distinct outputs, largest group first, as of the last
	// recompute.
	variants []outdiff.Variant

	// cursor is the selected variant.
	cursor int
}

// Title is the heading shown in the sidebar.
func (p *diffPanel) Title() string { return PanelDiff.Title() }

// Number is the key that jumps to this panel.
func (p *diffPanel) Number() int { return PanelDiff.Number() }

// Cursor is the position of the cursor.
func (p *diffPanel) Cursor() int { return p.cursor }

// clampCursor keeps the cursor on a variant when a recompute shrinks the list.
func (p *diffPanel) clampCursor() {
	p.cursor = clamp(p.cursor, 0, max(0, len(p.variants)-1))
}

// MoveCursor nudges the cursor without committing - the wheel browses.
func (p *diffPanel) MoveCursor(delta int) {
	p.cursor = clamp(p.cursor+delta, 0, max(0, len(p.variants)-1))
}

// SetCursorRow puts the cursor on a clicked visible body row.
func (p *diffPanel) SetCursorRow(row int) {
	p.cursor = clamp(row, 0, max(0, len(p.variants)-1))
}

// selected is the variant under the cursor, nil without one.
func (p *diffPanel) selected() *outdiff.Variant {
	if len(p.variants) == 0 {
		return nil
	}
	return &p.variants[clamp(p.cursor, 0, len(p.variants)-1)]
}

// Update drives the panel: the arrows move through the variants and enter
// makes the variant's hosts the selection.
func (p *diffPanel) Update(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, p.keys.Up):
		p.MoveCursor(-1)
	case key.Matches(msg, p.keys.Down):
		p.MoveCursor(+1)
	case key.Matches(msg, p.keys.Choose):
		v := p.selected()
		if v == nil {
			return nil
		}
		hosts := append([]string(nil), v.Hosts...)
		return func() tea.Msg { return DiffSelectMsg{Hosts: hosts} }
	}
	return nil
}

// View lists the variants, largest group first: how many hosts gave the
// answer, and the answer's first line. One line per variant keeps the click
// row and the variant index the same number.
func (p *diffPanel) View(focused bool, width, height int) string {
	theme := p.ctx.theme
	if p.command == "" {
		return theme.Muted.Render("nothing to compare yet - send a command with :")
	}
	if len(p.variants) == 0 {
		return theme.Muted.Render("no output to compare for " + p.command)
	}

	cursor := clamp(p.cursor, 0, len(p.variants)-1)
	first := clamp(cursor-max(0, height-1), 0, max(0, len(p.variants)-height))
	var b strings.Builder
	for i := first; i < len(p.variants) && i-first < max(1, height); i++ {
		if i > first {
			b.WriteString("\n")
		}
		label := p.line(p.variants[i])
		switch {
		case i == cursor:
			b.WriteString(theme.ListCursor(focused).Render(label))
		case i == 0:
			b.WriteString(theme.Base.Render(label))
		default:
			// Everything past the largest group is a disagreement, which is
			// what the panel exists to surface.
			b.WriteString(theme.StatusWarning.Render(label))
		}
	}
	return b.String()
}

// line renders one variant row: the host count and the first line of the
// answer, normalized - so it reads the same as the grouping compared.
func (p *diffPanel) line(v outdiff.Variant) string {
	snippet := "(no output)"
	for _, l := range v.Lines {
		if strings.TrimSpace(l) != "" {
			snippet = l
			break
		}
	}
	return fmt.Sprintf("%d host%s · %s", len(v.Hosts), plural(len(v.Hosts)), snippet)
}

// Preview shows the variant under the cursor whole: which hosts gave this
// answer, and the answer as the first of them printed it - real hostnames,
// not placeholders (issue #218 gave the list panels this pattern).
func (p *diffPanel) Preview(width, height int) (string, string, bool) {
	theme := p.ctx.theme
	title := "Output diff"
	if p.command == "" {
		return title, fitLines(theme, width, height, []string{
			theme.Muted.Render("nothing to compare yet - send a command with :"),
			"",
			theme.Muted.Render("after a send, hosts are grouped by the output"),
			theme.Muted.Render("they produced, so the machines that disagree"),
			theme.Muted.Render("stand out instead of hiding among the panes"),
		}), true
	}
	v := p.selected()
	if v == nil {
		return title, fitLines(theme, width, height,
			[]string{theme.Muted.Render("no output to compare for " + p.command)}), true
	}

	lines := []string{
		field(theme, "command", p.command),
		field(theme, "hosts", fmt.Sprintf("%d of %d compared", len(v.Hosts), p.marked)),
		theme.Base.Render(strings.Join(v.Hosts, " ")),
		"",
		field(theme, "as on", v.Hosts[0]),
	}
	if len(v.Raw) == 0 {
		lines = append(lines, theme.Muted.Render("(no output)"))
	}
	for _, l := range v.Raw {
		lines = append(lines, theme.Base.Render(l))
	}
	return title, fitLines(theme, width, height, lines), true
}

// The root's side: the comparison window and the recompute.

// markDiff starts a new comparison window at this send: the command, and each
// reached target's scrollback length, which is where its tail will start. A
// host the send could not reach keeps no mark - its silence is a fact about
// delivery, not an answer to compare.
func (a App) markDiff(command string, hosts []string, d broadcast.Delivery) App {
	failed := make(map[string]bool, len(d.Failed))
	for _, id := range d.Failed {
		failed[id] = true
	}
	marks := make(map[string]int, len(hosts))
	for _, id := range hosts {
		if failed[id] {
			continue
		}
		marks[id] = a.scrollbackLen(id)
	}
	a.diffMarks = marks
	a.diffCommand = command
	return a
}

// scrollbackLen is a host's current scrollback length in lines, the watermark
// a comparison tail starts after. Zero for a host without output.
func (a App) scrollbackLen(id string) int {
	t := a.paneTerminal(id)
	if t == nil || !t.HasOutput() {
		return 0
	}
	return t.TextLineCount()
}

// diffTail is a host's output past its watermark. A watermark past the end
// means nothing new was said - or the retention cap has rewritten history
// under the mark, in which case the tail honestly cannot be reconstructed and
// "nothing yet" is the answer that does not invent output.
func (a App) diffTail(id string, mark int) string {
	t := a.paneTerminal(id)
	if t == nil || !t.HasOutput() {
		return ""
	}
	// The tail is fetched by length rather than slicing the full text: this
	// runs per output event while a filter watches a send (issue #274).
	total := t.TextLineCount()
	if mark >= total {
		return ""
	}
	return t.TailText(total - mark)
}

// computeDiffVariants groups the marked hosts by their tails. It re-reads the
// scrollbacks, so [App.syncPanels] calls it only while the panel is selected.
func (a App) computeDiffVariants() []outdiff.Variant {
	if len(a.diffMarks) == 0 {
		return nil
	}
	samples := make([]outdiff.Sample, 0, len(a.diffMarks))
	for _, id := range a.fleetIDs() {
		mark, ok := a.diffMarks[id]
		if !ok {
			continue
		}
		samples = append(samples, outdiff.Sample{Host: id, Text: a.diffTail(id, mark)})
	}
	return outdiff.Group(samples)
}

// The root's accessors: what the rest of the model and the tests ask the
// Output diff panel about.

// DiffCursor is the position of the cursor in the Output diff panel.
func (a App) DiffCursor() int { return a.panels.diff.Cursor() }

// DiffVariants is the variant list as of the last recompute.
func (a App) DiffVariants() []outdiff.Variant { return a.panels.diff.variants }
