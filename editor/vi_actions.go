package editor

import (
	"github.com/tbxark/gvim/buffer"
)

var (
	motionLeftInLine = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		p := d.Cursor
		if p.Col == 0 {
			return p
		}
		return buffer.Pos{Line: p.Line, Col: prevBoundary(d.Buffer, p.Line, p.Col)}
	}
	motionRightInLine = func(_ *Editor, d *Document, _ bool) buffer.Pos {
		p := d.Cursor
		last := lastCharCol(d.Buffer, p.Line)
		if p.Col >= d.Buffer.LineLen(p.Line) {
			return buffer.Pos{Line: p.Line, Col: last}
		}
		return buffer.Pos{Line: p.Line, Col: min(nextBoundary(d.Buffer, p.Line, p.Col), last)}
	}
	// motionViUp and motionViDown are motionUp and motionDown with two differences. The
	// caret ends on a character of the line it reaches, and the first and last line are
	// walls: k on the first line and j on the last stay where they are, where the plain
	// motions jump to the start and the end of the document.
	motionViUp = func(e *Editor, d *Document, _ bool) buffer.Pos {
		return e.viVertical(d, -1)
	}
	motionViDown = func(e *Editor, d *Document, _ bool) buffer.Pos {
		return e.viVertical(d, 1)
	}
)

// viVertical moves the caret one display row up (dir -1) or down (+1), aiming at the
// wanted column like the plain vertical motions do, and keeps it on a character.
func (e *Editor) viVertical(d *Document, dir int) buffer.Pos {
	from := d.Cursor
	if to, ok := e.visualVertical(d, from, dir); ok && to.Line <= viLastLine(d.Buffer) {
		return onLastChar(d.Buffer, to)
	}
	line := from.Line + dir
	if line < 0 || line > viLastLine(d.Buffer) {
		e.wantFrom(d, from)
		return from
	}
	return onLastChar(d.Buffer, e.verticalTarget(d, from, line))
}

// registerViHandlers adds the vi_mode actions to h.
func registerViHandlers(h map[Action]handler) {
	h["move_left_in_line"] = moveHandler(motionLeftInLine, false, false)
	h["move_right_in_line"] = moveHandler(motionRightInLine, false, false)
	h["vi_move_up"] = moveHandler(motionViUp, false, true)
	h["vi_move_down"] = moveHandler(motionViDown, false, true)
	h["toggle_case"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.ToggleCase()
		return nil
	})
}
