package picker

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// drawPlain paints p into a new w by h grid and returns the grid and its rows.
func drawPlain(p *Picker, w, h int) (*grid.Grid, []string) {
	g := grid.New(w, h)
	p.Draw(g)
	return g, g.Plain()
}

// TestPickerCardFrameAndQueryRow pins the picker layout: a PaintBox card with a
// bare title and the footer on its bottom border, the query line on the last row of
// the frame, and the box kept space.Margin from the left and right edge.
func TestPickerCardFrameAndQueryRow(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{
		Title: "Pick", Footer: "enter to open", Placeholder: "type here",
		Fuzzy: true, Items: items("alpha", "beta"),
	})
	_, rows := drawPlain(p, 80, 20)
	geo := p.box
	if m := space.Margin(80); geo.left != m || geo.bw != 80-2*m {
		t.Fatalf("box at %d, %d wide, want margin %d on both sides", geo.left, geo.bw, m)
	}
	// The query line is the last row, and the box's bottom border sits one above it.
	if geo.queryY != 19 {
		t.Fatalf("query row = %d, want the last row", geo.queryY)
	}
	if got := geo.top + geo.boxH - 1; got != 18 {
		t.Fatalf("box bottom border = %d, want row 18", got)
	}
	if geo.top < 1 {
		t.Fatalf("box top = %d, want at least one row from the top", geo.top)
	}
	if top := rows[geo.top]; !strings.Contains(top, "╭─ Pick ─") {
		t.Fatalf("top border = %q, want the bare title on it", top)
	}
	bottom := rows[18]
	if !strings.Contains(bottom, "enter to open") || !strings.Contains(bottom, "2/2") {
		t.Fatalf("bottom border = %q, want the footer and the count on it", bottom)
	}
	if got := strings.TrimSpace(bottom); !strings.HasPrefix(got, "╰") || !strings.HasSuffix(got, "╯") {
		t.Fatalf("bottom border = %q, want a rounded bottom border", got)
	}
	// The rows start directly under the top border.
	if row := rows[geo.top+1]; !strings.Contains(row, "alpha") {
		t.Fatalf("row %d = %q, want the first item under the top border", geo.top+1, row)
	}
	if !strings.Contains(rows[19], "type here") {
		t.Fatalf("query row = %q, want the placeholder", rows[19])
	}
}

// TestPickerStatusIsOnTheBottomBorder: a status the owner sets replaces the row
// count, on the same bottom border row as the hints.
func TestPickerStatusIsOnTheBottomBorder(t *testing.T) {
	t.Parallel()
	p := New(style.New(theme.Theme{}), Options{Title: "Pick", Footer: "enter", Fuzzy: true, Items: items("a")})
	p.SetStatus("searching")
	_, rows := drawPlain(p, 80, 20)
	bottom := rows[p.box.top+p.box.boxH-1]
	if !strings.Contains(bottom, "searching") || !strings.Contains(bottom, "enter") {
		t.Fatalf("bottom border = %q, want the footer and the status", bottom)
	}
	if strings.Contains(bottom, "1/1") {
		t.Fatalf("bottom border = %q, want the status instead of the count", bottom)
	}
}
