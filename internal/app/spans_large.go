package app

import (
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/editorpane"
)

// Syntax spans of a large document. Its text is not held, so the lexer cannot run over
// all of it. The spans of the lines on screen come from a window: the lines from a
// stretch of context above the lines shown to a few screens below them. A token that
// began above the context is read from the start of the window, which is wrong
// only for a string or a comment that spans more than the context.

const (
	// defaultContextBytes is highlight_context_bytes when the config does not set it.
	defaultContextBytes = 10000
	// windowBack and windowAhead are how many lines above and below the line asked
	// for the window answers without being lexed again. Scrolling inside them moves
	// no window.
	windowBack  = 100
	windowAhead = 200
	// maxContextLines bounds the context in lines, for a document of short lines.
	maxContextLines = 2000
	// maxSpanWindows is how many windows are kept: one for each pane that shows the
	// document.
	maxSpanWindows = 4
	// plainTextLexer is the name of the lexer chroma matches to .txt files.
	plainTextLexer = "plaintext"
)

// largeSpans holds the lexed windows of one large document at one version of its
// text.
type largeSpans struct {
	buf     *buffer.Buffer
	version uint64
	wins    []spanWindow // newest first
}

// spanWindow is a lexed run of lines. It answers for the lines from served to last.
type spanWindow struct {
	served, last int
	first        int
	spans        [][]syntax.Span // by line - first
}

// fresh returns an empty cache with the settings of c. A nil c gives a plain empty
// cache.
func (c *spanCache) fresh() *spanCache {
	n := &spanCache{}
	if c != nil {
		n.contextBytes, n.contextSet = c.contextBytes, c.contextSet
	}
	return n
}

// setContext sets how many bytes of lines above the lines shown the lexer is given
// (highlight_context_bytes). A nil cache ignores it.
func (c *spanCache) setContext(n int) {
	if c != nil {
		c.contextBytes, c.contextSet = max(n, 0), true
	}
}

// context is the bytes of context lines above a window.
func (c *spanCache) context() int {
	if c.contextSet {
		return c.contextBytes
	}
	return defaultContextBytes
}

// window returns the syntax spans of line i of the large document doc. The lexer runs
// over a window of lines around i, and the result is kept while the text has the
// same version, so scrolling inside the window and moving the caret lex nothing.
// A line too long to be painted whole has none. It returns nil when no lexer matched
// the document, or when the cache is nil.
func (c *spanCache) window(doc *editor.Document, i int) []syntax.Span {
	if c == nil {
		return nil
	}
	c.useLexer(doc)
	b := doc.Buffer
	if c.plain || i < 0 || i >= b.Lines() || c.hl.Language() == plainTextLexer {
		// The plain text lexer gives plain spans, which are no spans, and reading the
		// window to find that out is the cost of every edit of a large log.
		return nil
	}
	ver := b.Version()
	if c.large.buf != b || c.large.version != ver {
		c.large = largeSpans{buf: b, version: ver}
	}
	for _, w := range c.large.wins {
		if i >= w.served && i <= w.last {
			return w.spans[i-w.first]
		}
	}
	w := c.lexWindow(b, ver, i)
	c.large.wins = slices.Insert(c.large.wins, 0, w)
	if len(c.large.wins) > maxSpanWindows {
		c.large.wins = c.large.wins[:maxSpanWindows]
	}
	return w.spans[i-w.first]
}

// lexWindow lexes the lines around line i.
func (c *spanCache) lexWindow(b *buffer.Buffer, ver uint64, i int) spanWindow {
	served := max(i-windowBack, 0)
	first := max(served-contextLines(b, served, c.context()), 0)
	last := min(i+windowAhead, b.Lines()-1)
	if first == 0 {
		served = 0
	}
	text := make([]string, last-first+1)
	var long []int
	for k := range text {
		if b.LineBytes(first+k) > editorpane.LongLineRunes {
			// The pane paints such a line as a window of its own and wants no spans of
			// it. Lexing it would cost more than the rest of the window together.
			long = append(long, k)
			continue
		}
		text[k] = b.Line(first + k)
	}
	spans := slices.Clone(c.hl.LinesWindow(int(ver), first, text).Spans)
	for _, k := range long {
		spans[k] = nil
	}
	return spanWindow{served: served, last: last, first: first, spans: spans}
}

// contextLines is how many lines above line from hold about bytes bytes, or fewer
// when the document or maxContextLines ends first.
func contextLines(b *buffer.Buffer, from, bytes int) int {
	n, sum := 0, 0
	for j := from - 1; j >= 0 && n < maxContextLines && sum < bytes; j-- {
		sum += b.LineBytes(j) + 1
		n++
	}
	return n
}
