package editorpane

import (
	"github.com/tbxark/gvim/buffer"
)

// VirtualLinesDocument is implemented by a Document whose caret may park on a
// virtual line below the last line (vertical virtual space). The pane draws the
// caret on that empty row at its display column; a document without it has no
// virtual lines.
type VirtualLinesDocument interface {
	// VirtualLines is how many empty rows below the last line the caret sits on;
	// 0 when it is on text.
	VirtualLines() int
	// VirtualLineColumn is the display column of the caret on that row.
	VirtualLineColumn() int
}

// setVirtualLines fills the frame's virtual-line position from the document.
func (f *frame) setVirtualLines(doc Document) {
	vd, ok := doc.(VirtualLinesDocument)
	if !ok {
		return
	}
	if n := vd.VirtualLines(); n > 0 {
		f.vlines, f.vlineCol = n, vd.VirtualLineColumn()
	}
}

// caretCell returns the grid cell of the caret, including a caret parked on a
// virtual line below the last buffer line. The virtual row is clamped to the
// body's last row: the view does not scroll past the text (Fresh's known limit),
// so deeper virtual lines would not be visible anyway.
func (f frame) caretCell() (x, y int, ok bool) {
	if f.vlines > 0 {
		end := buffer.Pos{Line: f.lines - 1, Col: f.blockRuneLen(f.lines - 1)}
		if _, ey, ok := f.cellOf(end); ok {
			return f.gutter + f.vlineCol - f.left, min(ey+f.vlines, f.h-1), true
		}
		return 0, 0, false
	}
	x, y, ok = f.cellOf(f.caret)
	return x + f.virtualHead, y, ok
}
