package editor

import (
	"unicode"

	"github.com/tbxark/gvim/internal/buffer"
)

// The actions of this file are written for the vi_mode plugin (AMENDMENTS C15). They are
// plugin-facing: they have no palette row and no menu entry, and no default key. A plugin
// runs them with gvim.command.run, and they show in gvim.command.actions().
//
//	move_left_in_line   one character left; never crosses to the previous line
//	move_right_in_line  one character right; never past the last character of the line
//	vi_move_up          like move_up, but the caret stays on a character (vi's k)
//	vi_move_down        like move_down, but the caret stays on a character (vi's j)
//	toggle_case         the case of the character at the caret (or of the selection)
//
// toggle_case changes text, so it is in textChanging (readonly.go).

// lastCharCol returns the column of the last character of line, or 0 for an empty line.
func lastCharCol(b *buffer.Buffer, line int) int {
	n := b.LineLen(line)
	if n == 0 {
		return 0
	}
	return prevBoundary(b, line, n)
}

// onLastChar moves p back to the last character of its line when it is past it, which
// is where a vertical move leaves the caret on a shorter line.
func onLastChar(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	if last := lastCharCol(b, p.Line); p.Col > last {
		p.Col = last
	}
	return p
}

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
	if to, ok := e.visualVertical(d, from, dir); ok {
		return onLastChar(d.Buffer, to)
	}
	line := from.Line + dir
	if line < 0 || line >= d.Buffer.Lines() {
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
		d.toggleCase()
		return nil
	})
}

// toggleRunes swaps the case of every letter of s, one rune for one rune, so the text
// keeps its length and its line breaks.
func toggleRunes(s string) string {
	out := []rune(s)
	for i, r := range out {
		if unicode.IsUpper(r) {
			out[i] = unicode.ToLower(r)
		} else {
			out[i] = unicode.ToUpper(r)
		}
	}
	return string(out)
}

// toggleCase swaps the case of the selection of each caret. A caret without a selection
// swaps the character at it and moves past that character, as vi's ~ does; at the end of
// a line there is nothing to swap and the caret stays. A selection is replaced by the
// swapped text and the caret ends after it.
func (d *Document) toggleCase() {
	cs := d.allCarets()
	plans := make([]caretPlan, len(cs))
	for i, c := range cs {
		if lo, hi := c.bounds(); lo != hi {
			old := d.planRange(lo, hi)
			if swapped := toggleRunes(old); swapped != old {
				plans[i].edit = &edit{from: lo, to: hi, text: swapped}
			}
			continue
		}
		p := c.head
		if p.Col >= d.Buffer.LineLen(p.Line) {
			continue
		}
		next := buffer.Pos{Line: p.Line, Col: nextBoundary(d.Buffer, p.Line, p.Col)}
		old := d.planRange(p, next)
		if swapped := toggleRunes(old); swapped != old {
			plans[i].edit = &edit{from: p, to: next, text: swapped}
			continue
		}
		// Nothing to change (a digit, a symbol), but the caret still moves on. An empty
		// edit at the caret carries it through the other carets' edits.
		width := next.Col - p.Col
		plans[i].edit = &edit{from: p, to: p}
		plans[i].place = func(end buffer.Pos) buffer.Pos { return buffer.Pos{Line: end.Line, Col: end.Col + width} }
	}
	d.applyPlans(cs, plans)
}
