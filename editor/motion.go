package editor

import (
	"github.com/tbxark/gvim/buffer"
)

// motion computes where the cursor goes next. A motion never changes the
// document.
type motion func(e *Editor, d *Document, extend bool) buffer.Pos

// motions for the cursor, keyed by the action name the keymap uses. extend is
// true when the move grows a selection (or mark mode is on); a plain move with a
// selection collapses it to the edge instead, as Fresh does.
var (
	motionLeft = func(_ *Editor, d *Document, extend bool) buffer.Pos {
		if !extend && d.HasSelection() {
			from, _ := d.Sel.Ordered()
			return from
		}
		if p, ok := prevPos(d.Buffer, d.Cursor); ok {
			return p
		}
		return d.Cursor
	}
	motionRight = func(_ *Editor, d *Document, extend bool) buffer.Pos {
		if !extend && d.HasSelection() {
			_, to := d.Sel.Ordered()
			return to
		}
		if p, ok := nextPos(d.Buffer, d.Cursor); ok {
			return p
		}
		return d.Cursor
	}
	// Up and Down start from the near edge of a collapsing selection, and on the
	// first or last line go to the start or end of the document.
	motionUp = func(e *Editor, d *Document, extend bool) buffer.Pos {
		from := d.Cursor
		if !extend && d.HasSelection() {
			from, _ = d.Sel.Ordered()
		}
		if to, ok := e.visualVertical(d, from, -1); ok {
			return to
		}
		if from.Line == 0 {
			e.wantFrom(d, from)
			return buffer.Pos{}
		}
		return e.verticalTarget(d, from, from.Line-1)
	}
	motionDown = func(e *Editor, d *Document, extend bool) buffer.Pos {
		from := d.Cursor
		if !extend && d.HasSelection() {
			_, from = d.Sel.Ordered()
		}
		if to, ok := e.visualVertical(d, from, 1); ok {
			return to
		}
		if from.Line == d.Buffer.Lines()-1 {
			e.wantFrom(d, from)
			return d.Buffer.End()
		}
		return e.verticalTarget(d, from, from.Line+1)
	}
	motionWordLeft = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		return wordLeft(d.Buffer, d.Cursor)
	}
	motionWordRight = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		return wordRight(d.Buffer, d.Cursor)
	}
	motionWordEnd = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		return wordEnd(d.Buffer, d.Cursor)
	}
	motionLineStart = func(e *Editor, d *Document, _ bool) buffer.Pos {
		if p, ok := e.visualLineStart(d.Cursor); ok {
			return p
		}
		return buffer.Pos{Line: d.Cursor.Line}
	}
	motionLineEnd = func(e *Editor, d *Document, _ bool) buffer.Pos {
		if p, ok := e.visualLineEnd(d.Buffer, d.Cursor); ok {
			return p
		}
		return buffer.Pos{Line: d.Cursor.Line, Col: d.Buffer.LineLen(d.Cursor.Line)}
	}
	// On a wrapped line, Home first goes to the start of the caret's row. On the
	// first row it toggles between the first non-blank and the line start.
	motionSmartHome = func(e *Editor, d *Document, _ bool) buffer.Pos {
		if starts, _ := e.rowStartsAt(d.Cursor.Line, d.Cursor.Col); starts != nil && rowOf(starts, d.Cursor.Col) > 0 {
			p, _ := e.visualLineStart(d.Cursor)
			return p
		}
		first := firstNonBlank(d.Buffer, d.Cursor.Line)
		if d.Cursor.Col == first {
			return buffer.Pos{Line: d.Cursor.Line}
		}
		return buffer.Pos{Line: d.Cursor.Line, Col: first}
	}
	motionDocStart = func(*Editor, *Document, bool) buffer.Pos { return buffer.Pos{} }
	motionDocEnd   = func(_ *Editor, d *Document, _ bool) buffer.Pos { return d.Buffer.End() }
)

// wantFrom sets the wanted column from p when it is unset. The next vertical
// move then aims at the column p is in.
func (e *Editor) wantFrom(d *Document, p buffer.Pos) {
	if d.Want < 0 {
		d.Want = cellColumn(d.Buffer, p, e.tabSizeOf(d))
	}
}

// verticalTarget moves from p to line, aiming at the wanted column. The column
// counts display cells, so a tab or a wide character keeps the caret aligned on
// the screen. The column is taken from p when it is unset.
func (e *Editor) verticalTarget(d *Document, from buffer.Pos, line int) buffer.Pos {
	e.wantFrom(d, from)
	line = min(max(line, 0), d.Buffer.Lines()-1)
	return buffer.Pos{Line: line, Col: colAtCell(d.Buffer, line, d.Want, e.tabSizeOf(d))}
}
