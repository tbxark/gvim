package highlight

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

// stringMaxBytes bounds the text of a windowed remote large file that a StringContext
// query lexes. The window is read from already-loaded remote windows and typed quotes
// are handled on the UI goroutine, so the window must stay small even though one line
// may be up to maxLexedLineBytes.
const stringMaxBytes = 64 << 10

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
	// gen is the remote window-arrival generation the window was lexed at, or zero
	// for a local document. A window arrival changes no text version, so it must be
	// part of the cache key or the spans of unloaded blank lines would be served.
	gen   uint64
	lang  string
	path  string
	lexer chroma.Lexer

	valid      bool
	start, end int // the lines [start, end) that spans cover
	lines      []string
	spans      [][]syntax.Span
	lexed      int // how many windows were lexed, for tests
}

// NewStringContext returns the editor.StringContext that the editor installs.
func NewStringContext() editor.StringContext {
	c := &stringCache{}
	return c.inString
}

// NewNonCodeContext returns the editor.NonCodeContext that the editor installs. It shares no
// state with NewStringContext, so each keeps its own window.
func NewNonCodeContext() editor.NonCodeContext {
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
// document, its language or a remote window arrival changed. It reports false when
// the line cannot be read safely (an unloaded remote window, a line too big to lex,
// or a local large document, which keeps the no-context behavior).
func (c *stringCache) refresh(doc *editor.Document, line int) bool {
	b := doc.Buffer
	gen := b.WindowGeneration()
	if c.doc != doc || c.lang != doc.Language() || c.path != doc.Path {
		c.doc, c.lang, c.path = doc, doc.Language(), doc.Path
		c.lexer, _ = documentLexer(doc)
		c.valid = false
	}
	if c.valid && c.version == b.Version() && c.gen == gen && line >= c.start && line < c.end {
		return true
	}
	var start int
	var lines []string
	if doc.AsyncLarge() {
		var ok bool
		start, lines, ok = c.asyncStringWindow(doc, b, line)
		if !ok {
			c.valid = false
			return false
		}
	} else {
		if doc.Large() {
			// A local large document keeps the old behavior: reading its lines on the
			// UI goroutine for a typed quote is not bounded by a loaded-window cache.
			c.valid = false
			return false
		}
		end := 0
		start, end = stringWindow(b.Lines(), line, func(i int) bool {
			return strings.TrimSpace(b.Line(i)) == ""
		})
		lines = make([]string, end-start)
		for i := range lines {
			lines[i] = b.Line(start + i)
		}
	}
	c.lines = lines
	c.spans = syntax.NewHighlighter(c.lexer).Lines(0, c.lines)
	c.start, c.end, c.version, c.gen, c.valid = start, start+len(lines), b.Version(), gen, true
	c.lexed++
	return true
}

// asyncStringWindow returns the loaded, contiguous, byte-bounded lines around line,
// starting at start. It never reads a line whose remote window is not loaded: the
// backward walk stops at the first unloaded or too-large line, the forward walk does
// the same, and a line that is too big for the lexer bounds the window. The query line
// itself must be loaded and lexable. The budget is stringMaxBytes, so a burst of long
// lines cannot make typing a quote lex megabytes on the UI goroutine.
func (c *stringCache) asyncStringWindow(doc *editor.Document, b *buffer.Buffer, line int) (start int, lines []string, ok bool) {
	if line < 0 || line >= b.Lines() || !doc.LineLoaded(line) || lineTooBig(b, line) {
		return 0, nil, false
	}
	text := b.Line(line)
	if !doc.LineLoaded(line) {
		return 0, nil, false
	}
	used := len(text)
	back := make([]string, 0, stringReach)
	start = line
	for i := line - 1; i >= 0 && line-i <= stringReach; i-- {
		if !doc.LineLoaded(i) || lineTooBig(b, i) {
			break
		}
		s := b.Line(i)
		if !doc.LineLoaded(i) {
			return 0, nil, false
		}
		if used+len(s) > stringMaxBytes {
			break
		}
		back = append(back, s)
		used += len(s)
		start = i
		if strings.TrimSpace(s) == "" {
			break
		}
	}
	forward := make([]string, 0, stringReach)
	for i := line + 1; i < b.Lines() && i-line <= stringReach; i++ {
		if !doc.LineLoaded(i) || lineTooBig(b, i) {
			break
		}
		s := b.Line(i)
		if !doc.LineLoaded(i) {
			return 0, nil, false
		}
		if used+len(s) > stringMaxBytes {
			break
		}
		forward = append(forward, s)
		used += len(s)
	}
	lines = make([]string, 0, len(back)+1+len(forward))
	for i := len(back) - 1; i >= 0; i-- {
		lines = append(lines, back[i])
	}
	lines = append(lines, text)
	lines = append(lines, forward...)
	return start, lines, true
}

// lineTooBig reports whether line is longer than the largest line the syntax lexer
// handles (see maxLexedLineBytes). The length comes from the index; the line must
// already be loaded.
func lineTooBig(b *buffer.Buffer, line int) bool {
	return b.LineBytes(line) > maxLexedLineBytes
}
func (c *stringCache) inString(doc *editor.Document, p buffer.Pos) bool {
	if p.Line < 0 || p.Line >= doc.Buffer.Lines() {
		return false
	}
	// A local large document keeps the old no-context behavior. A windowed remote
	// large document is read only from lines whose windows have arrived; a missing
	// window answers false without reading.
	if doc.Large() && !doc.AsyncLarge() {
		return false
	}
	if doc.AsyncLarge() && (!doc.LineLoaded(p.Line) || lineTooBig(doc.Buffer, p.Line)) {
		return false
	}
	if !c.refresh(doc, p.Line) {
		return false
	}
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
	if line < 0 || line >= doc.Buffer.Lines() {
		return nil
	}
	if doc.Large() && !doc.AsyncLarge() {
		return nil
	}
	if doc.AsyncLarge() && (!doc.LineLoaded(line) || lineTooBig(doc.Buffer, line)) {
		return nil
	}
	if !c.refresh(doc, line) {
		return nil
	}
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
