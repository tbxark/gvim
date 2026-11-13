package document

import (
	"github.com/tbxark/gvim/buffer"
)

// lineEdit is one edit inside a line: the runes inserted at col, or the runes
// removed from col on.
type lineEdit struct {
	Line, Col         int
	Inserted, Removed int
}

// Move returns where p ends up after the edit. A position on another line does
// not move. After a removal, a position past the removed run moves back by its
// length, and a position inside it moves to where the run began. After an
// insertion, a position past col moves forward by the inserted length. A
// position at col moves only when strict is false: a caret moves with the text
// it sits in front of, while a selection end stays where it was.
func (e lineEdit) Move(p buffer.Pos, strict bool) buffer.Pos {
	if p.Line != e.Line {
		return p
	}
	switch {
	case e.Removed > 0:
		switch {
		case p.Col >= e.Col+e.Removed:
			p.Col -= e.Removed
		case p.Col > e.Col:
			p.Col = e.Col
		}
	case e.Inserted > 0:
		if p.Col > e.Col || (!strict && p.Col == e.Col) {
			p.Col += e.Inserted
		}
	}
	return p
}
