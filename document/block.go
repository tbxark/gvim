package document

import (
	"github.com/tbxark/gvim/buffer"
)

// blockState is a block selection in progress. anchor is the corner the block
// began at, and at is the caret after the last block step. A block step extends
// the rectangle from anchor only while the primary caret is still at at. Once
// another action moves the caret, the next step starts a new block at the caret.
type blockState struct {
	Anchor, At buffer.Pos
}

// blockStep moves the caret by (dLine, dCol) and selects the rectangle from the
// block anchor to the new caret. The rectangle has one caret per line, and each
// caret selects the rectangle's columns on its line. A line shorter than the
// rectangle's left edge gets a caret at its end. The caret is clamped to its
// line, as a plain move is, so a short line narrows the rectangle. A sideways step
// crosses one grapheme cluster. The rectangle's columns are rune columns, as
// Fresh's are byte offsets, so a vertical step keeps the rune column.
func (d *Document) BlockStep(dLine, dCol int) {
	anchor := d.Cursor
	if d.Block != nil && d.Block.At == d.Cursor {
		anchor = d.Block.Anchor
	}
	col := d.Cursor.Col
	switch {
	case dCol > 0 && col < d.Buffer.LineLen(d.Cursor.Line):
		col = nextBoundary(d.Buffer, d.Cursor.Line, col)
	case dCol < 0 && col > 0:
		col = prevBoundary(d.Buffer, d.Cursor.Line, col)
	}
	at := d.Buffer.Clamp(buffer.Pos{Line: d.Cursor.Line + dLine, Col: col})
	d.setBlock(anchor, at)
}

// setBlock makes the rectangle from anchor to at the selection, one caret per
// line from the top line to the bottom line.
func (d *Document) setBlock(anchor, at buffer.Pos) {
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
	d.Block = &blockState{Anchor: anchor, At: at}
}
