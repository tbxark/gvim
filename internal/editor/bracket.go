package editor

import (
	"errors"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// bracketPartner maps each bracket to the bracket that closes or opens it.
var bracketPartner = map[rune]rune{
	'(': ')', ')': '(',
	'[': ']', ']': '[',
	'{': '}', '}': '{',
}

// bracketOpens lists the opening brackets.
const bracketOpens = "([{"

// registerBracketHandlers registers goto_matching_bracket.
func registerBracketHandlers(h map[Action]handler) {
	h["goto_matching_bracket"] = gotoMatchingBracket
}

// gotoMatchingBracket moves the caret to the bracket that matches the bracket
// at the caret, or the one just before it. Only (), [] and {} count. With a
// mark active, the selection grows to the match.
func gotoMatchingBracket(e *Editor, _ map[string]any) error {
	d := e.Active()
	at, ok := bracketNear(d)
	if !ok {
		return errors.New("no bracket at cursor")
	}
	to, ok := matchingBracket(d.Buffer, at)
	if !ok {
		return errors.New("no matching bracket found")
	}
	d.want = -1
	d.move(to, e.marking(d))
	return nil
}

// bracketNear returns the position of the bracket at the caret, or else of the
// bracket just before it.
func bracketNear(d *Document) (buffer.Pos, bool) {
	at := d.Cursor
	if r, ok := d.Buffer.RuneAt(at); ok && isBracket(r) {
		return at, true
	}
	if at.Col > 0 {
		before := buffer.Pos{Line: at.Line, Col: at.Col - 1}
		if r, ok := d.Buffer.RuneAt(before); ok && isBracket(r) {
			return before, true
		}
	}
	return buffer.Pos{}, false
}

// isBracket reports whether r is one of (), [] and {}.
func isBracket(r rune) bool {
	_, ok := bracketPartner[r]
	return ok
}

// matchingBracket finds the bracket that pairs with the one at at. It counts
// nested pairs of the same kind only, so the other kinds are ignored. It scans
// forward from an opening bracket and backward from a closing one.
func matchingBracket(b *buffer.Buffer, at buffer.Pos) (buffer.Pos, bool) {
	ch, _ := b.RuneAt(at)
	partner := bracketPartner[ch]
	depth := 0
	if strings.ContainsRune(bracketOpens, ch) {
		for line := at.Line; line < b.Lines(); line++ {
			rs := []rune(b.Line(line))
			col := 0
			if line == at.Line {
				col = at.Col + 1
			}
			for ; col < len(rs); col++ {
				switch rs[col] {
				case ch:
					depth++
				case partner:
					if depth == 0 {
						return buffer.Pos{Line: line, Col: col}, true
					}
					depth--
				}
			}
		}
		return buffer.Pos{}, false
	}
	for line := at.Line; line >= 0; line-- {
		rs := []rune(b.Line(line))
		col := len(rs) - 1
		if line == at.Line {
			col = at.Col - 1
		}
		for ; col >= 0; col-- {
			switch rs[col] {
			case ch:
				depth++
			case partner:
				if depth == 0 {
					return buffer.Pos{Line: line, Col: col}, true
				}
				depth--
			}
		}
	}
	return buffer.Pos{}, false
}
