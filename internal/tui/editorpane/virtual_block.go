package editorpane

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// BlockDocument is implemented by a Document with a block (rectangular)
// selection. The pane paints the rectangle, including the part that extends past
// the end of short lines in a virtual rectangle; a document without it paints the
// per-caret selections only.
type BlockDocument interface {
	// BlockSelection returns the rectangle of the block selection in progress,
	// and false when no block is active.
	BlockSelection() (editor.BlockRect, bool)
}

// blockRect is the part of a block selection the pane paints: its line and rune
// column bounds, where its head (caret) end is, and whether the rectangle extends
// past line ends.
type blockRect struct {
	Top, Bottom int
	Left, Right int
	HeadLine    int
	HeadCol     int
	Virtual     bool
}

// setBlock fills the frame's block rectangle from the document. It also computes
// the head's virtual offset, which the caret and the horizontal scroll use.
func (f *frame) setBlock(doc Document) {
	bd, ok := doc.(BlockDocument)
	if !ok {
		return
	}
	r, ok := bd.BlockSelection()
	if !ok {
		return
	}
	f.block = blockRect{
		Top: r.Top, Bottom: r.Bottom,
		Left: r.Left, Right: r.Right,
		HeadLine: r.Head.Line, HeadCol: r.Head.Col,
		Virtual: r.Virtual,
	}
	f.hasBlock = true
	if r.Virtual && r.Head.Line == f.caret.Line {
		f.virtualHead = max(r.Head.Col-f.blockRuneLen(r.Head.Line), 0)
	}
}

// paintBlockVirtual fills the part of a virtual block rectangle that lies past
// the end of line i's text with the selection style. The real cells of the
// rectangle are already painted from the per-caret selections; this is the
// spaces-only strip right of the line's content. end is the grid column after
// the row's painted cells, widthAll the row's width before horizontal scrolling
// dropped columns, and runeLen the line's rune length.
func (f frame) paintBlockVirtual(g *grid.Grid, i, y, end, widthAll int) {
	if !f.hasBlock || !f.block.Virtual || i < f.block.Top || i > f.block.Bottom || f.wrap || f.win != nil {
		return
	}
	runeLen := f.blockRuneLen(i)
	startCol := max(f.block.Left, runeLen)
	stopCol := f.block.Right
	if stopCol <= startCol {
		return
	}
	// The grid column of rune column c past the content is the row end plus the
	// cells c is past the content; the row end is the gutter plus the width left
	// after the window dropped its columns.
	start := end + (startCol - runeLen)
	stop := end + (stopCol - runeLen)
	start = max(start, f.gutter)
	stop = min(stop, f.w-1)
	if stop <= start {
		return
	}
	st := f.styles.Selection
	if st.BG == nil {
		st.Reverse = true
	}
	g.Fill(start, stop, y, st)
}

// blockRuneLen is the number of runes of line i. A windowed document reports its
// stored length; a document in memory builds the line, which a block selection can
// afford because virtual blocks are not enabled for large files.
func (f frame) blockRuneLen(i int) int {
	if f.win != nil {
		return f.win.LineBytes(i)
	}
	return len([]rune(f.doc.Line(i)))
}

// cellsWidth is the display width of a row's cells, before horizontal scrolling.
func cellsWidth(cells []cell) int {
	n := 0
	for _, c := range cells {
		n += ansi.StringWidth(string(c.r))
	}
	return n
}
