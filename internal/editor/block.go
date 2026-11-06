package editor

import "github.com/tbxark/gvim/internal/buffer"

// blockState is a block selection in progress. anchor is the corner the block
// began at, and at is the caret after the last block step. A block step extends
// the rectangle from anchor only while the primary caret is still at at. Once
// another action moves the caret, the next step starts a new block at the caret.
type blockState struct {
	anchor, at buffer.Pos
}

// blockHandler returns the handler of a block selection step in direction
// (dLine, dCol).
func blockHandler(dLine, dCol int) handler {
	return docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.blockStep(dLine, dCol)
		return nil
	})
}

// blockStep moves the caret by (dLine, dCol) and selects the rectangle from the
// block anchor to the new caret. The rectangle has one caret per line, and each
// caret selects the rectangle's columns on its line. A line shorter than the
// rectangle's left edge gets a caret at its end. The caret is clamped to its
// line, as a plain move is, so a short line narrows the rectangle.
func (d *Document) blockStep(dLine, dCol int) {
	anchor := d.Cursor
	if d.block != nil && d.block.at == d.Cursor {
		anchor = d.block.anchor
	}
	at := d.Buffer.Clamp(buffer.Pos{Line: d.Cursor.Line + dLine, Col: d.Cursor.Col + dCol})
	d.setBlock(anchor, at)
}

// setBlock makes the rectangle from anchor to at the selection, one caret per
// line from the top line to the bottom line.
func (d *Document) setBlock(anchor, at buffer.Pos) {
	first, last := min(anchor.Line, at.Line), max(anchor.Line, at.Line)
	cs := make([]caret, 0, last-first+1)
	for line := first; line <= last; line++ {
		n := d.Buffer.LineLen(line)
		cs = append(cs, caret{
			anchor:  buffer.Pos{Line: line, Col: min(anchor.Col, n)},
			head:    buffer.Pos{Line: line, Col: min(at.Col, n)},
			primary: line == at.Line,
		})
	}
	d.setCarets(cs)
	d.block = &blockState{anchor: anchor, at: at}
}
