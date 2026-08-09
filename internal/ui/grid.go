package ui

import "math"

// Pane size floors. Below these a pane shows too little output to be worth
// looking at, so the grid pages instead of shrinking further: four readable
// panes and a page indicator beat twelve unreadable ones.
//
// The floor is set on the terminal the host sees, not on the cell: the remote
// PTY is sized to the pane's content (the cell minus its border and header, see
// resizePTYs), so these two constants are the smallest width and height a
// remote shell is ever told it has. 45x16 is a starting guideline (issue #139),
// tuned here in one place.
const (
	// MinPaneContentWidth is the narrowest terminal a host pane shows.
	MinPaneContentWidth = 45
	// MinPaneContentHeight is the shortest terminal a host pane shows.
	MinPaneContentHeight = 16
	// MinPaneWidth is the narrowest a pane cell may be: the content plus the
	// border column on each side.
	MinPaneWidth = MinPaneContentWidth + 2
	// MinPaneHeight is the shortest a pane cell may be: the content plus the
	// border rows and the header line.
	MinPaneHeight = MinPaneContentHeight + 3
)

// Grid is a tiling of the main area.
//
// The tiling is a pure function of the area and the host count, so it is
// deterministic and testable without a terminal - and so two renders of the same
// state can never disagree about where a pane is.
type Grid struct {
	// Columns and Rows are the shape of one page.
	Columns int
	Rows    int
	// PerPage is how many panes fit on a page: Columns * Rows, never more than
	// the host count.
	PerPage int
	// Pages is how many pages the host count needs.
	Pages int
	// Cells are the rectangles of the panes on one page, in reading order.
	// There are exactly PerPage of them.
	Cells []Rect
}

// Empty reports whether the grid holds no cells.
func (g Grid) Empty() bool { return len(g.Cells) == 0 }

// Page is the page a host at index i appears on.
func (g Grid) Page(i int) int {
	if g.PerPage <= 0 {
		return 0
	}
	return i / g.PerPage
}

// Cell returns the rectangle for a host index on its own page, and whether it
// has one.
func (g Grid) Cell(i int) (Rect, bool) {
	if g.PerPage <= 0 || i < 0 {
		return Rect{}, false
	}
	slot := i % g.PerPage
	if slot >= len(g.Cells) {
		return Rect{}, false
	}
	return g.Cells[slot], true
}

// TileGrid divides an area into panes for count hosts.
//
// The shape is the squarest arrangement that holds the hosts - 1 fills the
// area, 4 make a 2x2, 20 make a 5x4 - bounded by how many panes of the minimum
// size the area actually fits. When the hosts do not fit, the grid reports the
// pages it takes rather than shrinking the panes below the floor.
func TileGrid(area Rect, count int) Grid {
	return TileGridCapped(area, count, 0)
}

// TileGridCapped is [TileGrid] with a ceiling on how many panes one page may
// hold; a cap of zero or less means "as many as fit".
//
// The cap is how half mode enlarges panes: fewer panes on a page means bigger
// cells, and the hosts that no longer fit page like they always have - the
// pages count says so, and the overflow footer names the key that reaches
// them. Capping is never allowed to produce a page holding nothing.
func TileGridCapped(area Rect, count, maxPerPage int) Grid {
	if area.Empty() || count <= 0 {
		return Grid{}
	}

	maxColumns := max(1, area.Width/MinPaneWidth)
	maxRows := max(1, area.Height/MinPaneHeight)
	capacity := maxColumns * maxRows
	if maxPerPage > 0 {
		capacity = min(capacity, maxPerPage)
	}

	perPage := min(count, capacity)

	columns := min(int(math.Ceil(math.Sqrt(float64(perPage)))), maxColumns)
	columns = max(columns, 1)
	rows := max(1, ceilDiv(perPage, columns))
	if rows > maxRows {
		rows = maxRows
		columns = min(max(1, ceilDiv(perPage, rows)), maxColumns)
	}

	// The shape may hold more slots than there are hosts on the last page -
	// three hosts in a 2x2 - and that is deliberate: the panes stay the size
	// they were rather than jumping about as hosts come and go.
	perPage = min(perPage, columns*rows)

	g := Grid{
		Columns: columns,
		Rows:    rows,
		PerPage: perPage,
		Pages:   ceilDiv(count, perPage),
	}
	g.Cells = cells(area, columns, rows, perPage)
	return g
}

// cells lays out the rectangles, spreading the remainder columns and rows over
// the leftmost and topmost panes so every terminal column is used.
func cells(area Rect, columns, rows, want int) []Rect {
	widths := split(area.Width, columns)
	heights := split(area.Height, rows)

	out := make([]Rect, 0, want)
	y := area.Y
	for row := range rows {
		x := area.X
		for col := range columns {
			if len(out) == want {
				return out
			}
			out = append(out, Rect{X: x, Y: y, Width: widths[col], Height: heights[row]})
			x += widths[col]
		}
		y += heights[row]
	}
	return out
}

// split divides total into n parts, giving the remainder to the first parts, so
// the parts add up to exactly total.
func split(total, n int) []int {
	if n <= 0 {
		return nil
	}
	base := total / n
	extra := total % n

	parts := make([]int, n)
	for i := range parts {
		parts[i] = base
		if i < extra {
			parts[i]++
		}
	}
	return parts
}

// ceilDiv divides and rounds up. A non-positive divisor yields zero rather than
// panicking: the grid must never take the program down.
func ceilDiv(a, b int) int {
	if b <= 0 {
		return 0
	}
	return (a + b - 1) / b
}
