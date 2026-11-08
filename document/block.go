package document

import (
	"github.com/tbxark/gvim/buffer"
)

// blockState is a block selection in progress. anchor is the corner the block
// began at, and at is the caret after the last block step. A block step extends
// the rectangle from anchor only while the primary caret is still at at. Once
// another action moves the caret, the next step starts a new block at the caret.
//
// Virtual says the rectangle may extend past the end of lines: its columns are
// rune columns that need not exist on every line (editor.virtual_space "block"
// or "on"). The carets of a virtual block are still clamped onto the text; the
// rectangle itself is what copy, paste, insert and the painter read.
type blockState struct {
	Anchor, At buffer.Pos
	Virtual    bool
}

// BlockRect is the rectangle of an active block selection. Columns are rune
// columns, half-open on the right as every block consumer reads them; a column
// past a line's length exists only in a virtual rectangle, where the gap is
// spaces once an edit materializes it.
type BlockRect struct {
	Top, Bottom int // the lines of the rectangle, both included
	Left, Right int // its rune columns, Right exclusive
	// Head is the caret end of the rectangle, blockState.At. Its column may be
	// past the end of its line when Virtual.
	Head    buffer.Pos
	Virtual bool
}

// Lines is how many lines the rectangle covers.
func (r BlockRect) Lines() int { return r.Bottom - r.Top + 1 }

// Width is how many rune columns the rectangle covers.
func (r BlockRect) Width() int { return r.Right - r.Left }

// blockInProgress reports whether the caret is still where the last block step
// left it, so the block selection is in progress. A virtual block stores its
// unclamped column, while the caret's byte position is clipped onto the line, so
// the two are compared after clipping.
func (d *Document) blockInProgress() bool {
	if d == nil || d.Block == nil {
		return false
	}
	at := d.Block.At
	if at.Line != d.Cursor.Line {
		return false
	}
	return min(at.Col, d.Buffer.LineLen(at.Line)) == d.Cursor.Col
}

// BlockSelection returns the rectangle of the block selection in progress, and
// false when the block is over: another action moved the caret.
func (d *Document) BlockSelection() (rect BlockRect, ok bool) {
	if !d.blockInProgress() {
		return BlockRect{}, false
	}
	a, at := d.Block.Anchor, d.Block.At
	return BlockRect{
		Top:     min(a.Line, at.Line),
		Bottom:  max(a.Line, at.Line),
		Left:    min(a.Col, at.Col),
		Right:   max(a.Col, at.Col),
		Head:    at,
		Virtual: d.Block.Virtual,
	}, true
}

// HasVirtualBlock reports whether a block selection is in progress whose
// rectangle may extend past the end of lines.
func (d *Document) HasVirtualBlock() bool {
	rect, ok := d.BlockSelection()
	return ok && rect.Virtual
}

// BlockStep moves the caret by (dLine, dCol) and selects the rectangle from the
// block anchor to the new caret. The rectangle has one caret per line, and each
// caret selects the rectangle's columns on its line. A line shorter than the
// rectangle's left edge gets a caret at its end.
//
// virtual, editor.virtual_space "block" or "on", lets the rectangle extend past
// the end of lines: a sideways step then moves one rune column with no clamp, a
// short line keeps the column (the gap is spaces once an edit materializes it),
// and a block started from a virtual caret grows from its on-screen column. Off,
// the caret is clamped to its line as before, so a short line narrows the
// rectangle. A sideways step crosses one grapheme cluster, and the rectangle's
// columns are rune columns, as Fresh's are byte offsets, so a vertical step
// keeps the rune column.
func (d *Document) BlockStep(dLine, dCol int, virtual bool) {
	at := d.Cursor
	anchor := at
	switch {
	case d.blockInProgress():
		// Continue the rectangle from its own corner; the caret's byte column
		// is clipped, the stored block column is not.
		anchor = d.Block.Anchor
		at.Col = d.Block.At.Col
		if d.Block.Virtual {
			virtual = true
		}
	case virtual && d.VirtualColumns() > 0:
		// Entering a block from a virtual caret keeps the on-screen column for
		// both corners of the rectangle.
		at.Col += d.VirtualColumns()
		anchor = at
	}
	col := at.Col
	switch {
	case dCol > 0:
		if virtual {
			col++
		} else if col < d.Buffer.LineLen(at.Line) {
			col = nextBoundary(d.Buffer, at.Line, col)
		}
	case dCol < 0 && col > 0:
		if virtual {
			col--
		} else {
			col = prevBoundary(d.Buffer, at.Line, col)
		}
	}
	line := at.Line + dLine
	if line < 0 {
		line = 0
	}
	if line >= d.Buffer.Lines() {
		line = d.Buffer.Lines() - 1
	}
	newAt := buffer.Pos{Line: line, Col: col}
	if virtual {
		newAt.Col = min(newAt.Col, MaxViewColumn)
	} else {
		newAt = d.Buffer.Clamp(newAt)
	}
	d.setBlock(anchor, newAt, virtual)
}

// setBlock makes the rectangle from anchor to at the selection, one caret per
// line from the top line to the bottom line. The carets are clamped onto the
// text even in a virtual rectangle; virtual is stored for the consumers that
// need the rectangle's true columns.
func (d *Document) setBlock(anchor, at buffer.Pos, virtual bool) {
	first, last := min(anchor.Line, at.Line), max(anchor.Line, at.Line)
	if d.large != nil && last-first+1 > maxLargeCarets {
		d.failPlan(errTooManyCarets())
		return
	}
	cs := make([]caret, 0, last-first+1)
	for line := first; line <= last; line++ {
		n := d.Buffer.LineLen(line)
		cs = append(cs, caret{
			Anchor:  buffer.Pos{Line: line, Col: min(anchor.Col, n)},
			Head:    buffer.Pos{Line: line, Col: min(at.Col, n)},
			Primary: line == at.Line,
		})
	}
	d.SetCarets(cs)
	d.Block = &blockState{Anchor: anchor, At: at, Virtual: virtual}
}

// blockPad is how many spaces a line of a virtual block needs before an edit at
// its caret: the rectangle's left edge when that edge is past the line's end,
// otherwise zero. It is set on the carets AllCarets returns.
func (d *Document) blockPad(rect BlockRect, line int) int {
	if !rect.Virtual {
		return 0
	}
	return max(rect.Left-d.Buffer.LineLen(line), 0)
}
