package editor

import (
	"errors"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// bracketPairs lists the opener and closer of each bracket pair.
var bracketPairs = [][2]rune{{'(', ')'}, {'[', ']'}, {'{', '}'}}

// angleBrackets is the pair that counts only where < and > are delimiters
// rather than comparison operators: HTML and Templ, as in Fresh.
var angleBrackets = [2]rune{'<', '>'}

// registerBracketHandlers registers goto_matching_bracket.
func registerBracketHandlers(h map[Action]handler) {
	h["goto_matching_bracket"] = gotoMatchingBracket
}

// gotoMatchingBracket moves the caret to the bracket that matches the bracket
// at the caret. Anywhere else it goes to the closing bracket of the pair that
// encloses the caret. When neither exists, it uses the bracket just before the
// caret. With a mark active, the selection grows to the match.
func gotoMatchingBracket(e *Editor, _ map[string]any) error {
	d := e.Active()
	pairs := e.bracketPairsOf(d)
	at, ok, limited := bracketTarget(d, pairs)
	if limited {
		return ErrScanLimit
	}
	if !ok {
		return errors.New("no bracket at cursor")
	}
	to, ok, limited := matchingBracket(d.Buffer, at, pairs)
	if limited {
		return ErrScanLimit
	}
	if !ok {
		return errors.New("no matching bracket found")
	}
	d.want = -1
	d.move(to, e.marking(d))
	return nil
}

// bracketPairsOf returns the bracket pairs of d's language.
func (e *Editor) bracketPairsOf(d *Document) [][2]rune {
	lang := e.SettingsFor(d).Language
	if lang == "" {
		lang = strings.ToLower(d.Language())
	}
	if lang == "html" || lang == "templ" {
		return append(bracketPairs[:len(bracketPairs):len(bracketPairs)], angleBrackets)
	}
	return bracketPairs
}

// bracketTarget returns the bracket to match: the one at the caret, else the
// opener of the pair around the caret, else the one just before the caret. limited
// is set when the scan for the enclosing pair stopped at the budget of a large
// document and the bracket just before the caret is not one either.
func bracketTarget(d *Document, pairs [][2]rune) (pos buffer.Pos, ok, limited bool) {
	at := d.Cursor
	if r, ok := d.Buffer.RuneAt(at); ok && isBracket(r, pairs) {
		return at, true, false
	}
	open, ok, limited := enclosingBracket(d.Buffer, at, pairs)
	if ok {
		return open, true, false
	}
	if at.Col > 0 {
		before := buffer.Pos{Line: at.Line, Col: at.Col - 1}
		if r, ok := d.Buffer.RuneAt(before); ok && isBracket(r, pairs) {
			return before, true, false
		}
	}
	return buffer.Pos{}, false, limited
}

// enclosingBracket scans back from p for the opening bracket whose closer does
// not come before p. Each kind of pair nests on its own. In a large document the scan
// reads at most the lines and bytes of a scan budget; limited reports that it stopped
// there without an answer.
func enclosingBracket(b *buffer.Buffer, p buffer.Pos, pairs [][2]rune) (pos buffer.Pos, ok, limited bool) {
	depths := make([]int, len(pairs))
	budget := newScanBudget(b)
	for line := p.Line; line >= 0; line-- {
		if !budget.take(b, line) {
			return buffer.Pos{}, false, true
		}
		rs := []rune(b.Line(line))
		col := len(rs) - 1
		if line == p.Line {
			col = p.Col - 1
		}
		for ; col >= 0; col-- {
			for i, pair := range pairs {
				switch rs[col] {
				case pair[1]:
					depths[i]++
				case pair[0]:
					if depths[i] == 0 {
						return buffer.Pos{Line: line, Col: col}, true, false
					}
					depths[i]--
				}
			}
		}
	}
	return buffer.Pos{}, false, false
}

// partnerOf returns the bracket that pairs with r and whether r opens its pair.
func partnerOf(r rune, pairs [][2]rune) (partner rune, opens, ok bool) {
	for _, pair := range pairs {
		switch r {
		case pair[0]:
			return pair[1], true, true
		case pair[1]:
			return pair[0], false, true
		}
	}
	return 0, false, false
}

// isBracket reports whether r is a bracket of pairs.
func isBracket(r rune, pairs [][2]rune) bool {
	_, _, ok := partnerOf(r, pairs)
	return ok
}

// matchingBracket finds the bracket that pairs with the one at at. It counts
// nested pairs of the same kind only, so the other kinds are ignored. It scans
// forward from an opening bracket and backward from a closing one. In a large
// document the scan reads at most the lines and bytes of a scan budget, and a forward
// scan also ends at the last line the index knows; limited reports that it stopped
// at either without an answer.
func matchingBracket(b *buffer.Buffer, at buffer.Pos, pairs [][2]rune) (pos buffer.Pos, ok, limited bool) {
	ch, _ := b.RuneAt(at)
	partner, opens, _ := partnerOf(ch, pairs)
	depth := 0
	budget := newScanBudget(b)
	if opens {
		for line := at.Line; line < b.Lines(); line++ {
			if !budget.take(b, line) {
				return buffer.Pos{}, false, true
			}
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
						return buffer.Pos{Line: line, Col: col}, true, false
					}
					depth--
				}
			}
		}
		return buffer.Pos{}, false, indexUnfinished(b)
	}
	for line := at.Line; line >= 0; line-- {
		if !budget.take(b, line) {
			return buffer.Pos{}, false, true
		}
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
					return buffer.Pos{Line: line, Col: col}, true, false
				}
				depth--
			}
		}
	}
	return buffer.Pos{}, false, false
}
