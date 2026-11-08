package editorpane

import (
	"strings"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
)

// brackets are the pairs that count, as the editor's goto_matching_bracket counts them.
const (
	openBrackets  = "([{"
	closeBrackets = ")]}"
	allBrackets   = openBrackets + closeBrackets

	// bracketReachLines bounds the lines read to find the partner of a bracket, so a
	// bracket in a huge file costs the same as one in a small file.
	bracketReachLines = 2000
)

// bracketScan is the bracket state of one frame: the pair at the caret, and the
// stack of open brackets for the depth colors. The depth is counted from a screenful
// above the window, line by line as the lines are painted.
type bracketScan struct {
	ready   bool
	budget  *budget // the bytes the scan of the lines above the window may read
	next    int     // the first line the stack has not seen
	stack   []rune
	pair    [2]buffer.Pos
	hasPair bool
}

// skipKind reports whether text of kind k is prose or data, whose brackets are not
// structure.
func skipKind(k syntax.Kind) bool { return k == syntax.KindComment || k == syntax.KindString }

// bracketsOf marks the brackets of buffer line i: the depth color of each one, and
// the pair at the caret. kinds are the syntax kinds of the runes of raw.
func (f frame) bracketsOf(i int, raw []rune, kinds []syntax.Kind, cells []cell) {
	if !f.opts.HighlightBrackets {
		return
	}
	bs := &f.fx.brackets
	rainbow := f.opts.RainbowBrackets
	if !bs.ready {
		bs.ready = true
		bs.next = max(f.top-f.h, 0)
		bs.budget = f.newBudget()
		bs.pair, bs.hasPair = f.findPair()
	}
	for rainbow && bs.next < i {
		text, ok := f.scanLine(bs.budget, bs.next)
		bs.next++
		if !ok || !strings.ContainsAny(text, allBrackets) {
			continue
		}
		r := []rune(text)
		f.walkBrackets(r, kindsPerRune(text, f.doc.LineSpans(bs.next-1)), func(int, rune, int) {})
	}
	bs.next = i + 1
	if rainbow {
		// One pass maps every column: expandedIndex per bracket is quadratic on a
		// line of 200 KB.
		var table []int
		f.walkBrackets(raw, kinds, func(col int, r rune, depth int) {
			if table == nil {
				table = expandedTable(raw, f.tab)
			}
			if x := table[col]; x < len(cells) {
				cells[x].mark, cells[x].lvl = markBracket, uint8(depth%style.RainbowLevels)
			}
		})
	}
	if bs.hasPair {
		for _, p := range bs.pair {
			if p.Line == i {
				if x := expandedIndex(raw, p.Col-f.off, f.tab); x < len(cells) {
					cells[x].pair = true
				}
			}
		}
	}
}

// walkBrackets feeds the structural brackets of one line to the stack and calls
// visit with each one's rune column, rune and depth. An opener has the depth it
// opens, and a closer the depth it closes. A closer that does not match the open
// bracket closes nothing.
func (f frame) walkBrackets(raw []rune, kinds []syntax.Kind, visit func(col int, r rune, depth int)) {
	bs := &f.fx.brackets
	for col, r := range raw {
		if !strings.ContainsRune(allBrackets, r) || (col < len(kinds) && skipKind(kinds[col])) {
			continue
		}
		if strings.ContainsRune(openBrackets, r) {
			visit(col, r, len(bs.stack))
			bs.stack = append(bs.stack, r)
			continue
		}
		depth := max(len(bs.stack)-1, 0)
		if n := len(bs.stack); n > 0 && strings.IndexRune(openBrackets, bs.stack[n-1]) == strings.IndexRune(closeBrackets, r) {
			bs.stack = bs.stack[:n-1]
		}
		visit(col, r, depth)
	}
}

// findPair finds the bracket at the caret, or else the one just before it, and its
// partner. A bracket in a comment or a string is not structure, so it has no pair.
func (f frame) findPair() ([2]buffer.Pos, bool) {
	at, ok := f.bracketAtCaret()
	if !ok {
		return [2]buffer.Pos{}, false
	}
	partner, ok := f.partnerOf(at)
	if !ok {
		return [2]buffer.Pos{at, at}, true
	}
	return [2]buffer.Pos{at, partner}, true
}

// bracketAtCaret returns the position of the bracket under the caret, or else just
// before it.
func (f frame) bracketAtCaret() (buffer.Pos, bool) {
	if f.caret.Line >= f.lines || f.isLong(f.caret.Line) || f.isTall(f.caret.Line) {
		// A window shows part of its line, as a segment does, so a pair at the caret there is
		// not looked for.
		return buffer.Pos{}, false
	}
	text := f.doc.Line(f.caret.Line)
	runes := []rune(text)
	kinds := kindsPerRune(text, f.doc.LineSpans(f.caret.Line))
	for _, col := range []int{f.caret.Col, f.caret.Col - 1} {
		if col < 0 || col >= len(runes) || !strings.ContainsRune(allBrackets, runes[col]) {
			continue
		}
		if col < len(kinds) && skipKind(kinds[col]) {
			return buffer.Pos{}, false
		}
		return buffer.Pos{Line: f.caret.Line, Col: col}, true
	}
	return buffer.Pos{}, false
}

// partnerOf scans from the bracket at at for the bracket that closes or opens it,
// counting only brackets of its own kind and leaving out comments and strings. It
// reads at most bracketReachLines lines each way.
func (f frame) partnerOf(at buffer.Pos) (buffer.Pos, bool) {
	atText := f.doc.Line(at.Line)
	ch := []rune(atText)[at.Col]
	var open, closer rune
	forward := strings.ContainsRune(openBrackets, ch)
	if forward {
		open, closer = ch, rune(closeBrackets[strings.IndexRune(openBrackets, ch)])
	} else {
		open, closer = rune(openBrackets[strings.IndexRune(closeBrackets, ch)]), ch
	}
	depth := 0
	step := 1
	if !forward {
		step = -1
	}
	bud := f.newBudget()
	for n, line := 0, at.Line; line >= 0 && line < f.lines && n <= bracketReachLines; n, line = n+1, line+step {
		text := atText
		if line != at.Line {
			var ok bool
			if text, ok = f.scanLine(bud, line); !ok {
				if bud != nil && bud.out {
					break
				}
				continue
			}
		}
		if !strings.ContainsRune(text, open) && !strings.ContainsRune(text, closer) {
			continue
		}
		runes := []rune(text)
		kinds := kindsPerRune(text, f.doc.LineSpans(line))
		col := 0
		if !forward {
			col = len(runes) - 1
		}
		if line == at.Line {
			col = at.Col + step
		}
		for ; col >= 0 && col < len(runes); col += step {
			r := runes[col]
			if (r != open && r != closer) || (col < len(kinds) && skipKind(kinds[col])) {
				continue
			}
			if r == ch {
				depth++
				continue
			}
			if depth == 0 {
				return buffer.Pos{Line: line, Col: col}, true
			}
			depth--
		}
	}
	return buffer.Pos{}, false
}
