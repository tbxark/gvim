package browser

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// drawBrowser draws b into a new w by h grid.
func drawBrowser(b *Browser, w, h int) *grid.Grid {
	g := grid.New(w, h)
	b.Draw(g)
	return g
}

// TestBrowserTitleIsBareOnTheTopBorder: the title goes to PaintBox bare, so the
// frame has single spaces around it.
func TestBrowserTitleIsBareOnTheTopBorder(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	tests := []struct {
		name   string
		folder bool
		title  string
		want   string
	}{
		{"file browser", false, "", "Open File"},
		{"folder picker", true, "", "Switch Project"},
		{"custom title", false, "Pick Folder", "Pick Folder"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New(Options{Dir: dir, Home: home, FolderOnly: tt.folder, Title: tt.title})
			g := drawBrowser(b, 60, 20)
			top := strings.TrimSpace(g.Plain()[b.box.y])
			if !strings.Contains(top, "╭─ "+tt.want+" ─") {
				t.Fatalf("top border = %q, want the bare title %q on it", top, tt.want)
			}
			if strings.Contains(top, "╭─  ") {
				t.Fatalf("top border = %q, the title starts after a double space", top)
			}
		})
	}
}

// TestBrowserHintsAreOnTheBottomBorder: the key hints, and a notice over them, are
// painted on the box's bottom border row.
func TestBrowserHintsAreOnTheBottomBorder(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	b := New(Options{Dir: dir, Home: home})
	g := drawBrowser(b, 60, 20)
	bottom := b.box.y + 3 + b.box.h
	row := strings.TrimSpace(g.Plain()[bottom])
	if !strings.HasPrefix(row, "╰") || !strings.Contains(row, "Enter open") {
		t.Fatalf("bottom border = %q, want the hints on it", row)
	}
	if strings.Contains(g.Plain()[bottom-1], "Enter open") {
		t.Fatalf("row %d = %q, want no separate hint row", bottom-1, g.Plain()[bottom-1])
	}
	send(t, b, altKey('.'))
	g = drawBrowser(b, 60, 20)
	if row := strings.TrimSpace(g.Plain()[bottom]); !strings.Contains(row, "Show Hidden enabled") {
		t.Fatalf("bottom border = %q, want the notice on it", row)
	}
}

// TestBrowserKeepsTheMarginFromTheFrameEdge: the card keeps space.Margin from the
// left and right edge, and its bottom border stays above the status row.
func TestBrowserKeepsTheMarginFromTheFrameEdge(t *testing.T) {
	t.Parallel()
	dir, home := fixture(t)
	for _, w := range []int{59, 60, 140} {
		b := New(Options{Dir: dir, Home: home})
		drawBrowser(b, w, 20)
		want := space.Margin(w)
		if b.box.x < want || w-(b.box.x+b.box.w) < want {
			t.Errorf("width %d: box at %d..%d, want a margin of at least %d", w, b.box.x, b.box.x+b.box.w, want)
		}
		if bottom := b.box.y + 3 + b.box.h; bottom != 18 {
			t.Errorf("width %d: bottom border at row %d, want row 18 above the status row", w, bottom)
		}
	}
}
