package editorpane

import (
	"image/color"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/fold"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// The optional interfaces must match what *editor.Document offers. The app
// adapter forwards them, so a signature change breaks this file first.
var (
	_ CaretLister = (*editor.Document)(nil)
	_ Folder      = (*editor.Document)(nil)
	_ LargeDoc    = (*editor.Document)(nil)
)

// caretDoc is a fakeDoc with extra carets, the way *editor.Document reports them.
type caretDoc struct {
	*fakeDoc
	extra []editor.Caret
}

func (d caretDoc) Cursors() []editor.Caret { return d.extra }

// foldDoc is a fakeDoc with indentation folds, the way *editor.Document reports them.
type foldDoc struct {
	*fakeDoc
	ranges []fold.Range
	folded map[int]bool
}

func (d foldDoc) Folds(int) []fold.Range { return d.ranges }
func (d foldDoc) Folded(line int) bool   { return d.folded[line] }

// cellAt returns the cell at (x, y), failing the test when it is outside g.
func cellAt(t *testing.T, g *grid.Grid, x, y int) grid.Cell {
	t.Helper()
	if x < 0 || y < 0 || x >= g.Width() || y >= g.Height() {
		t.Fatalf("cell (%d,%d) is outside the %dx%d grid", x, y, g.Width(), g.Height())
	}
	return g.At(x, y)
}

// bgOf returns the background of a cell as a color, or nil when it has none.
func bgOf(c grid.Cell) color.Color { return c.Style.BG }

var (
	cRed   = color.RGBA{R: 255, A: 255}
	cGreen = color.RGBA{G: 255, A: 255}
	cBlue  = color.RGBA{B: 255, A: 255}
	cGray  = color.RGBA{R: 80, G: 80, B: 80, A: 255}
)
