package editorpane

import (
	"regexp"
	"strings"

	"github.com/tbxark/gvim/syntax"
)

// The page view composes a Markdown line: it hides the inline delimiters
// (emphasis, strong, inline code, links, strike-through), turns a thematic break
// into a rule, turns the quote marks into a colored bar and works out the hanging
// indent of a list item or a quote. The caret's line (and the line above a caret
// at the start of a line) is left raw, Fresh's unless-cursor-in scope. See
// docs/markdown.md.

// composedLine is one composed Markdown line.
type composedLine struct {
	runes []rune        // the visible runes
	kinds []syntax.Kind // the syntax kind of each visible rune
	// src is the source rune column of each visible rune. srcToDisp maps a source
	// rune boundary (caret column) to a composed rune boundary, and dispToSrc the
	// reverse; hidden delimiters map to the nearest visible boundary.
	src       []int
	srcToDisp []int
	dispToSrc []int
	// hang is the hanging indent of the rows after the first, in cells.
	hang int
}

// composedLine returns the composed line i, cached in the document's layout memo
// until its text changes.
func (f frame) composedLine(i int) composedLine {
	if lm, ok := f.doc.(LayoutMemo); ok && !isLarge(f.doc) {
		key := composeKey{line: i, measure: f.pageMeasure}
		return lm.MemoValue(key, func() any { return f.composeLine(i) }).(composedLine)
	}
	return f.composeLine(i)
}

type composeKey struct {
	line    int
	measure int
}

// rawComposed returns line i without any composing: the visible reveal variant.
func (f frame) rawComposed(i int) composedLine {
	text := f.doc.Line(i)
	raw := []rune(text)
	kinds := kindsPerRune(text, f.doc.LineSpans(i))
	cp := composedLine{runes: raw, kinds: kinds, src: make([]int, len(raw))}
	for j := range raw {
		cp.src[j] = j
	}
	finishMaps(&cp, len(raw))
	return cp
}

// preRune is one rune of a composed prefix, with its source rune column.
type preRune struct {
	r   rune
	src int
}

// composeLine composes one Markdown line.
func (f frame) composeLine(i int) composedLine {
	text := f.doc.Line(i)
	raw := []rune(text)
	kinds := kindsPerRune(text, f.doc.LineSpans(i))
	if fl, ok := f.markdownFences()[i]; ok {
		return f.composeFenceLine(i, fl, raw, kinds)
	}
	if tl, ok := f.tables().rows[i]; ok {
		return f.composeTableLine(i, tl, false)
	}
	out := composedLine{}

	// A thematic break becomes one rule across the measure, two cells short of it
	// so it never wraps onto a second row.
	if isRuleRunes(raw) {
		n := max(f.pageMeasure-2, 1)
		out.runes = make([]rune, n)
		out.kinds = make([]syntax.Kind, n)
		out.src = make([]int, n)
		for j := range out.runes {
			out.runes[j] = '─'
			out.kinds[j] = syntax.KindQuote
			out.src[j] = 0
		}
		finishMaps(&out, len(raw))
		return out
	}

	prefix, contentStart, hang := composePrefix(raw)
	out.hang = hang
	for _, p := range prefix {
		out.runes = append(out.runes, p.r)
		out.kinds = append(out.kinds, kindAt(kinds, p.src))
		out.src = append(out.src, p.src)
	}
	// Inline delimiters of the content part, in source rune columns.
	marks := syntax.MarkdownInlineMarkers(string(raw[min(contentStart, len(raw)):]))
	hidden := make([]bool, len(raw))
	for _, m := range marks {
		for j := max(contentStart+m.Start, 0); j < contentStart+m.End && j < len(hidden); j++ {
			hidden[j] = true
		}
	}
	for j := max(contentStart, 0); j < len(raw); j++ {
		if hidden[j] {
			continue
		}
		out.runes = append(out.runes, raw[j])
		out.kinds = append(out.kinds, kindAt(kinds, j))
		out.src = append(out.src, j)
	}
	finishMaps(&out, len(raw))
	// A hanging indent must leave room for the continuation rows.
	if out.hang+minContinuation > f.pageMeasure {
		out.hang = 0
	}
	return out
}

// composePrefix parses the leading blockquote and list markers of raw: the runes
// to draw for them, where the content starts, and the hanging indent their width
// gives the continuation rows. A quote marker becomes a colored bar; a list
// marker is kept as text.
func composePrefix(raw []rune) (prefix []preRune, contentStart, hang int) {
	pos := 0
	for pos < len(raw) && raw[pos] == ' ' {
		prefix = append(prefix, preRune{raw[pos], pos})
		pos++
	}
	// Nested quotes: each ">" becomes a bar and a space, and the blanks after it
	// are its separator.
	for pos < len(raw) && raw[pos] == '>' {
		prefix = append(prefix, preRune{'│', pos}, preRune{' ', pos})
		pos++
		for pos < len(raw) && (raw[pos] == ' ' || raw[pos] == '\t') {
			pos++
		}
	}
	rest := string(raw[pos:])
	if m := composeList.FindString(rest); m != "" {
		start := pos
		for _, r := range m {
			if r == '\t' {
				for q := 0; q < 4; q++ {
					prefix = append(prefix, preRune{' ', start})
				}
				continue
			}
			prefix = append(prefix, preRune{r, start})
			start++
		}
		pos += len([]rune(m))
	}
	contentStart = pos
	hang = len(prefix)
	return prefix, contentStart, hang
}

// composeList is the list marker the page view keeps, in Fresh's own matching
// order (a bullet or an ordered number, then the blanks after it).
var composeList = regexp.MustCompile("^(?:[-+*]|[0-9]{1,9}[.)])(?:[ \t]+|$)")

// kindAt returns the syntax kind of source rune col, or plain.
func kindAt(kinds []syntax.Kind, col int) syntax.Kind {
	if col >= 0 && col < len(kinds) {
		return kinds[col]
	}
	return syntax.KindPlain
}

// finishMaps builds the source/display boundary maps of a composed line. The
// source columns of the visible runes are monotonic, so a boundary maps to the
// first visible rune at or after it.
func finishMaps(cp *composedLine, srcLen int) {
	cp.srcToDisp = make([]int, srcLen+1)
	cp.dispToSrc = make([]int, len(cp.runes)+1)
	next := 0
	for j, src := range cp.src {
		cp.dispToSrc[j] = src
		for k := next; k <= src && k < len(cp.srcToDisp); k++ {
			cp.srcToDisp[k] = j
		}
		if src+1 > next {
			next = min(src+1, len(cp.srcToDisp))
		}
	}
	for k := next; k < len(cp.srcToDisp); k++ {
		cp.srcToDisp[k] = len(cp.runes)
	}
	cp.dispToSrc[len(cp.runes)] = srcLen
}

// isRuleRunes reports whether raw is a thematic break, "---", "***" or "___".
func isRuleRunes(raw []rune) bool {
	s := string(raw)
	body := strings.TrimLeft(s, " ")
	if len(s)-len(body) > 3 || body == "" || !strings.ContainsRune("*-_", rune(body[0])) {
		return false
	}
	first := body[0]
	n := 0
	for i := 0; i < len(body); i++ {
		switch body[i] {
		case first:
			n++
		case ' ', '\t':
		default:
			return false
		}
	}
	return n >= 3
}

// markdownFences returns the fenced code classification of the document's
// lines, so compose can frame a block. The table is kept in the document's
// layout memo until its text changes.
func (f frame) markdownFences() map[int]syntax.FenceLine {
	compute := func() any {
		lines := make([]string, f.doc.LineCount())
		for i := range lines {
			lines[i] = f.doc.Line(i)
		}
		out := make(map[int]syntax.FenceLine)
		for i, fl := range syntax.MarkdownFences(lines) {
			if fl.Kind != syntax.FenceNone {
				out[i] = fl
			}
		}
		return out
	}
	if lm, ok := f.doc.(LayoutMemo); ok && !isLarge(f.doc) {
		return lm.MemoValue(composeFenceKey{}, compute).(map[int]syntax.FenceLine)
	}
	return compute().(map[int]syntax.FenceLine)
}

type composeFenceKey struct{}

// fenceBody reports whether line i is a line of a fenced code block's body: the
// page view frames it, so the caret does not reveal it (the caret sits inside
// the frame, and the source/display map keeps its column exact).
func (f frame) fenceBody(i int) bool {
	fl, ok := f.markdownFences()[i]
	return ok && fl.Kind == syntax.FenceBody
}

// revealed reports whether the caret reveals line i: the caret is on it, or at
// the start of the next line (Fresh's line scope includes the line end).
func (f frame) revealed(i int) bool {
	return i == f.caret.Line || (i+1 == f.caret.Line && f.caret.Col == 0)
}
