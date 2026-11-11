package document

import (
	"github.com/tbxark/gvim/buffer"
	"strings"
)

// indentTriggers are the last characters of a line that make the next line one
// level deeper.
const indentTriggers = "{[(:"

// isBlank reports whether r is a space, a tab or a carriage return.
func isBlank(r rune) bool { return r == ' ' || r == '\t' || r == '\r' }

// lastNonBlank returns the last rune of line that is not blank.
func lastNonBlank(line []rune) (rune, bool) {
	for i := len(line) - 1; i >= 0; i-- {
		if !isBlank(line[i]) {
			return line[i], true
		}
	}
	return 0, false
}

// leadColumns returns the display width of the blanks at the start of line,
// looking at no more than limit runes.
func leadColumns(line []rune, limit, tabSize int) int {
	w := 0
	for i := 0; i < len(line) && i < limit; i++ {
		switch line[i] {
		case ' ':
			w++
		case '\t':
			// A tab advances to the next tab stop, as the display draws it.
			if tabSize > 0 {
				w = (w/tabSize + 1) * tabSize
			}
		default:
			return w
		}
	}
	return w
}

// indentString returns the blanks of display width w: tabs and then spaces for the
// remainder when useTabs is set, else only spaces.
func indentString(w int, useTabs bool, tabSize int) string {
	if useTabs && tabSize > 0 {
		return strings.Repeat("\t", w/tabSize) + strings.Repeat(" ", w%tabSize)
	}
	return strings.Repeat(" ", w)
}

// blankPrefix reports whether line[:col] holds only spaces and tabs.
func blankPrefix(line []rune, col int) bool {
	for _, r := range line[:col] {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}

// newLineIndent returns the width of the indent for the line that Enter starts at
// p. In plain text a blank line keeps the caret column, because a blank line ends a
// block of prose.
func newLineIndent(b *buffer.Buffer, p buffer.Pos, tabSize int, plainText bool) int {
	line := []rune(b.Line(p.Line))
	if blankPrefix(line, min(p.Col, len(line))) {
		// The caret sits in the leading blanks: the text after it keeps its column.
		rest := line[min(p.Col, len(line)):]
		_, content := lastNonBlank(rest)
		if content || plainText {
			return leadColumns(line, p.Col, tabSize)
		}
	}
	return patternIndent(b, p, tabSize)
}

// patternIndent is Fresh's calculate_indent_pattern. A line with text before the
// caret keeps its own indent. A blank line takes the indent of the nearest line
// above that has text, lowered to the next line's indent when that is smaller and
// does not start with a closer. Either way a trigger at the end of the text before
// the caret opens one more level.
func patternIndent(b *buffer.Buffer, p buffer.Pos, tabSize int) int {
	line := []rune(b.Line(p.Line))
	col := min(p.Col, len(line))
	last, hasText := lastNonBlank(line[:col])
	ref := 0
	if hasText {
		ref = leadColumns(line, col, tabSize)
	} else {
		budget := newScanBudget(b)
		for q := p.Line - 1; q >= 0; q-- {
			if !budget.Take(b, q) {
				break
			}
			l := []rune(b.Line(q))
			c, ok := lastNonBlank(l)
			if !ok {
				continue
			}
			ref = leadColumns(l, len(l), tabSize)
			if strings.ContainsRune(indentTriggers, c) {
				return ref + tabSize
			}
			break
		}
		if next, ok := nextIndentUnlessCloser(b, p, tabSize); ok && next < ref {
			ref = next
		}
	}
	if hasText && strings.ContainsRune(indentTriggers, last) {
		return ref + tabSize
	}
	return ref
}

// nextIndentUnlessCloser returns the indent of the first line at or after p (the
// rest of p's line first) that has text, unless that text starts with a closer.
func nextIndentUnlessCloser(b *buffer.Buffer, p buffer.Pos, tabSize int) (int, bool) {
	budget := newScanBudget(b)
	for q := p.Line; q < b.Lines(); q++ {
		if !budget.Take(b, q) {
			break
		}
		l := []rune(b.Line(q))
		if q == p.Line {
			l = l[min(p.Col, len(l)):]
		}
		indent := 0
		for _, r := range l {
			switch r {
			case ' ':
				indent++
			case '\t':
				indent += tabSize
			case '\r':
			default:
				if r == '}' || r == ']' || r == ')' {
					return 0, false
				}
				return indent, true
			}
		}
	}
	return 0, false
}

// closerIndent returns the indent a closer typed at p should have: the indent of
// the line that holds the opener it matches. Lines are scanned upward and judged by
// their last character, as Fresh's calculate_dedent_pattern does: a line that ends
// in a closer skips the next opener. It is 0 when no opener is found.
func closerIndent(b *buffer.Buffer, p buffer.Pos, tabSize int) int {
	depth := 0
	budget := newScanBudget(b)
	for q := p.Line; q >= 0; q-- {
		if !budget.Take(b, q) {
			break
		}
		l := []rune(b.Line(q))
		if q == p.Line {
			l = l[:min(p.Col, len(l))]
		}
		c, ok := lastNonBlank(l)
		if !ok {
			continue
		}
		switch c {
		case '}', ']', ')':
			depth++
		case '{', '[', '(':
			if depth == 0 {
				return leadColumns(l, len(l), tabSize)
			}
			depth--
		}
	}
	return 0
}
