package app

import (
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/syntax"
)

// stringReach is how many lines above and below a queried position the lexer reads.
// Typing a quote asks for the context on the UI goroutine, so the lexing must not
// grow with the document: a window of this reach costs a few tens of milliseconds
// however large the file is.
const stringReach = 300

// stringCache answers the editor's question whether a position lies inside a string
// literal, which decides if a typed quote is paired. It lexes with the lexer that
// colors the document, so a string that spans lines counts. Only a window of lines
// around the position is lexed, and it starts at a blank line when one is near, where
// a string or comment seldom begins. A document of at most 2*stringReach lines is
// lexed whole, so its answer is exact. The last window is kept while the text is
// unchanged, so the carets of one keystroke share one lexing. A position at the end
// of a line, in a window with no lexer, or in a window too large to lex, is not in a
// string. A document in large file mode is never in a string: its lines can be huge.
// The editor calls it on the UI goroutine only.
type stringCache struct {
	doc     *editor.Document
	version uint64
	lang    string
	path    string
	lexer   chroma.Lexer

	valid      bool
	start, end int // the lines [start, end) that spans cover
	lines      []string
	spans      [][]syntax.Span
	lexed      int // how many windows were lexed, for tests
}

// newStringContext returns the editor.StringContext that app installs.
func newStringContext() editor.StringContext {
	c := &stringCache{}
	return c.inString
}

// newNonCodeContext returns the editor.NonCodeContext that app installs. It shares no
// state with newStringContext, so each keeps its own window.
func newNonCodeContext() editor.NonCodeContext {
	c := &stringCache{}
	return c.nonCode
}

// stringWindow returns the lines [start, end) to lex for a query on line of a document
// of n lines. blank tells whether a line holds only blanks.
func stringWindow(n, line int, blank func(int) bool) (start, end int) {
	end = min(n, line+stringReach+1)
	start = max(0, line-stringReach)
	if start > 0 {
		for i := start; i > start-stringReach && i >= 0; i-- {
			if blank(i) {
				start = i
				break
			}
		}
	}
	return start, end
}

// refresh makes the window cover line of doc, and lexes it again when the text, the
// document or its language changed.
func (c *stringCache) refresh(doc *editor.Document, line int) {
	b := doc.Buffer
	if c.doc != doc || c.lang != doc.Language() || c.path != doc.Path {
		c.doc, c.lang, c.path = doc, doc.Language(), doc.Path
		c.lexer = documentLexer(doc)
		c.valid = false
	}
	if c.valid && c.version == b.Version() && line >= c.start && line < c.end {
		return
	}
	start, end := stringWindow(b.Lines(), line, func(i int) bool {
		return strings.TrimSpace(b.Line(i)) == ""
	})
	c.lines = make([]string, end-start)
	for i := range c.lines {
		c.lines[i] = b.Line(start + i)
	}
	c.spans = syntax.NewHighlighter(c.lexer).Lines(0, c.lines)
	c.start, c.end, c.version, c.valid = start, end, b.Version(), true
	c.lexed++
}

func (c *stringCache) inString(doc *editor.Document, p buffer.Pos) bool {
	// A document in large file mode is not lexed for a typed quote: its lines can be
	// huge, and the window would read them whole.
	if doc.Large() || p.Line < 0 || p.Line >= doc.Buffer.Lines() {
		return false
	}
	c.refresh(doc, p.Line)
	i := p.Line - c.start
	// Spans hold byte offsets, and p holds a rune column.
	line := []rune(c.lines[i])
	if p.Col < 0 || p.Col >= len(line) {
		return false
	}
	offset := len(string(line[:p.Col]))
	for _, s := range c.spans[i] {
		if s.Start <= offset && offset < s.End {
			return s.Kind == syntax.KindString
		}
	}
	return false
}

// nonCode is the editor.NonCodeContext: for each rune column of the line, whether the
// character is in a comment or a string. It is nil when the line has none.
func (c *stringCache) nonCode(doc *editor.Document, line int) []bool {
	if doc.Large() || line < 0 || line >= doc.Buffer.Lines() {
		return nil
	}
	c.refresh(doc, line)
	i := line - c.start
	var mask []bool
	offset := 0 // the byte offset of the rune at col
	k := 0      // the first span that may cover offset
	for col, r := range []rune(c.lines[i]) {
		for k < len(c.spans[i]) && c.spans[i][k].End <= offset {
			k++
		}
		if k < len(c.spans[i]) && c.spans[i][k].Start <= offset {
			if kind := c.spans[i][k].Kind; kind == syntax.KindString || kind == syntax.KindComment {
				if mask == nil {
					mask = make([]bool, len([]rune(c.lines[i])))
				}
				mask[col] = true
			}
		}
		offset += utf8.RuneLen(r)
	}
	return mask
}
