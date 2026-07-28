package ui

import (
	"strings"
	"testing"
)

// The acceptance criterion: tiling is deterministic and known for these counts.
func TestTileGridShapes(t *testing.T) {
	// Room for 5 columns and 6 rows of minimum-size panes, so the shape is
	// decided by the host count rather than by the terminal.
	area := Rect{Width: 5 * MinPaneWidth, Height: 6 * MinPaneHeight}

	tests := []struct {
		hosts       int
		wantColumns int
		wantRows    int
	}{
		{1, 1, 1},
		{2, 2, 1},
		{3, 2, 2},
		{4, 2, 2},
		{6, 3, 2},
		{9, 3, 3},
		{12, 4, 3},
		{20, 5, 4},
	}

	for _, tc := range tests {
		g := TileGrid(area, tc.hosts)
		if g.Columns != tc.wantColumns || g.Rows != tc.wantRows {
			t.Errorf("%d hosts tiled %dx%d, want %dx%d",
				tc.hosts, g.Columns, g.Rows, tc.wantColumns, tc.wantRows)
		}
		if g.Pages != 1 {
			t.Errorf("%d hosts needed %d pages in an area that fits 30", tc.hosts, g.Pages)
		}
		if len(g.Cells) != tc.hosts {
			t.Errorf("%d hosts produced %d cells", tc.hosts, len(g.Cells))
		}
	}
}

func TestTileGridIsDeterministic(t *testing.T) {
	area := Rect{Width: 137, Height: 61}
	for hosts := 1; hosts <= 40; hosts++ {
		first := TileGrid(area, hosts)
		second := TileGrid(area, hosts)
		if first.Columns != second.Columns || first.Rows != second.Rows ||
			len(first.Cells) != len(second.Cells) {
			t.Fatalf("%d hosts tiled differently twice", hosts)
		}
	}
}

// The acceptance criterion: below the minimum pane size the grid pages instead
// of shrinking further.
func TestTileGridPagesRatherThanShrinking(t *testing.T) {
	// Room for exactly 2x2 minimum-size panes.
	area := Rect{Width: 2 * MinPaneWidth, Height: 2 * MinPaneHeight}

	g := TileGrid(area, 20)
	if g.PerPage != 4 {
		t.Fatalf("PerPage = %d, want 4", g.PerPage)
	}
	if g.Pages != 5 {
		t.Fatalf("Pages = %d, want 5", g.Pages)
	}
	for i, cell := range g.Cells {
		if cell.Width < MinPaneWidth || cell.Height < MinPaneHeight {
			t.Fatalf("cell %d is %dx%d, below the %dx%d floor",
				i, cell.Width, cell.Height, MinPaneWidth, MinPaneHeight)
		}
	}
}

func TestTileGridNeverGoesBelowTheFloorUntilItHasTo(t *testing.T) {
	for width := MinPaneWidth; width < 200; width += 7 {
		for height := MinPaneHeight; height < 80; height += 5 {
			area := Rect{Width: width, Height: height}
			for _, hosts := range []int{1, 2, 5, 13, 40} {
				g := TileGrid(area, hosts)
				for _, cell := range g.Cells {
					if cell.Width < MinPaneWidth || cell.Height < MinPaneHeight {
						t.Fatalf("%dx%d with %d hosts produced a %dx%d cell",
							width, height, hosts, cell.Width, cell.Height)
					}
				}
			}
		}
	}
}

// The cells must cover the area exactly: a column left undrawn is a stripe of
// the previous frame that never gets cleared.
func TestCellsCoverTheAreaExactly(t *testing.T) {
	area := Rect{X: 30, Y: 0, Width: 137, Height: 61}

	for _, hosts := range []int{1, 2, 3, 4, 6, 9, 12, 20} {
		g := TileGrid(area, hosts)

		rowWidth := 0
		for col := range g.Columns {
			rowWidth += g.Cells[min(col, len(g.Cells)-1)].Width
		}
		if hosts >= g.Columns && rowWidth != area.Width {
			t.Errorf("%d hosts: a row covers %d columns, the area has %d",
				hosts, rowWidth, area.Width)
		}
		if got := g.Cells[0].X; got != area.X {
			t.Errorf("%d hosts: the first cell starts at x=%d, the area at x=%d",
				hosts, got, area.X)
		}
		if got := g.Cells[0].Y; got != area.Y {
			t.Errorf("%d hosts: the first cell starts at y=%d, the area at y=%d",
				hosts, got, area.Y)
		}
	}
}

func TestTileGridDegenerateInput(t *testing.T) {
	tests := []struct {
		name  string
		area  Rect
		hosts int
	}{
		{"no hosts", Rect{Width: 100, Height: 40}, 0},
		{"negative hosts", Rect{Width: 100, Height: 40}, -3},
		{"empty area", Rect{}, 4},
		{"negative area", Rect{Width: -10, Height: -10}, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := TileGrid(tc.area, tc.hosts)
			if !g.Empty() {
				t.Fatalf("TileGrid(%+v, %d) = %+v", tc.area, tc.hosts, g)
			}
			if _, ok := g.Cell(0); ok {
				t.Fatal("an empty grid handed out a cell")
			}
			if g.Page(3) != 0 {
				t.Fatal("an empty grid reported a page")
			}
		})
	}
}

// A terminal too small for even one pane still tiles, because the alternative
// is a grid that renders nothing and says nothing.
func TestTileGridBelowOnePane(t *testing.T) {
	g := TileGrid(Rect{Width: 10, Height: 3}, 4)
	if g.Columns != 1 || g.Rows != 1 {
		t.Fatalf("tiled %dx%d, want 1x1", g.Columns, g.Rows)
	}
	if g.Pages != 4 {
		t.Fatalf("Pages = %d, want one per host", g.Pages)
	}
}

func TestGridPageAndCell(t *testing.T) {
	g := TileGrid(Rect{Width: 2 * MinPaneWidth, Height: 2 * MinPaneHeight}, 10)
	if g.PerPage != 4 {
		t.Fatalf("PerPage = %d", g.PerPage)
	}

	tests := []struct {
		host int
		page int
		slot int
	}{
		{0, 0, 0}, {3, 0, 3}, {4, 1, 0}, {9, 2, 1},
	}
	for _, tc := range tests {
		if got := g.Page(tc.host); got != tc.page {
			t.Fatalf("Page(%d) = %d, want %d", tc.host, got, tc.page)
		}
		cell, ok := g.Cell(tc.host)
		if !ok {
			t.Fatalf("Cell(%d) reported no cell", tc.host)
		}
		if cell != g.Cells[tc.slot] {
			t.Fatalf("Cell(%d) = %+v, want slot %d", tc.host, cell, tc.slot)
		}
	}

	if _, ok := g.Cell(-1); ok {
		t.Fatal("Cell accepted a negative index")
	}
}

func TestSplit(t *testing.T) {
	tests := []struct {
		total, n int
		want     []int
	}{
		{10, 2, []int{5, 5}},
		{11, 2, []int{6, 5}},
		{10, 3, []int{4, 3, 3}},
		{3, 5, []int{1, 1, 1, 0, 0}},
	}
	for _, tc := range tests {
		got := split(tc.total, tc.n)
		if len(got) != len(tc.want) {
			t.Fatalf("split(%d, %d) = %v", tc.total, tc.n, got)
		}
		sum := 0
		for i, part := range got {
			if part != tc.want[i] {
				t.Fatalf("split(%d, %d) = %v, want %v", tc.total, tc.n, got, tc.want)
			}
			sum += part
		}
		if sum != tc.total {
			t.Fatalf("split(%d, %d) sums to %d", tc.total, tc.n, sum)
		}
	}
	if got := split(5, 0); got != nil {
		t.Fatalf("split(5, 0) = %v", got)
	}
}

func TestCeilDiv(t *testing.T) {
	tests := []struct{ a, b, want int }{
		{10, 5, 2}, {11, 5, 3}, {0, 5, 0}, {5, 0, 0}, {5, -1, 0},
	}
	for _, tc := range tests {
		if got := ceilDiv(tc.a, tc.b); got != tc.want {
			t.Fatalf("ceilDiv(%d, %d) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// Full screen is one pane in the whole area. It is bound to f rather than to a
// number: the number keys select sidebar panels, which is the epic's rule.
func TestFullScreenTogglesAndShowsOnePane(t *testing.T) {
	a := fleetApp(t, 6)
	tiled := plain(a.View().Content)
	if !strings.Contains(tiled, "web-06") {
		t.Fatalf("the grid does not show every host:\n%s", tiled)
	}

	a = pressKey(t, a, "f")
	if !a.FullScreen() {
		t.Fatal("f did not switch to full screen")
	}
	full := plain(a.View().Content)
	if !strings.Contains(full, "web-01") {
		t.Fatalf("the focused host is not shown full screen:\n%s", full)
	}
	if strings.Contains(full, "web-06") {
		t.Fatalf("full screen still shows the other panes:\n%s", full)
	}

	a = pressKey(t, a, "f")
	if a.FullScreen() {
		t.Fatal("f did not switch back")
	}
	if !strings.Contains(plain(a.View().Content), "web-06") {
		t.Fatal("the grid did not come back")
	}
}

func TestGridRendersOnlyTheCurrentPage(t *testing.T) {
	// A short terminal fits few panes, so twelve hosts have to page.
	a := resize(t, fleetApp(t, 12), 60, 14)

	g := a.Grid()
	if g.Pages < 2 {
		t.Fatalf("setup: 12 hosts fit in %d page(s) at 60x14", g.Pages)
	}

	view := plain(a.View().Content)
	if !strings.Contains(view, "web-01") {
		t.Fatalf("the first page does not show the first host:\n%s", view)
	}

	last := a.cfg.Hosts[len(a.cfg.Hosts)-1]
	if strings.Contains(view, last) {
		t.Fatalf("the first page shows a host from another page:\n%s", view)
	}
}
