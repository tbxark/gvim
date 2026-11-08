package highlight

import (
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/syntax"
)

// Syntax spans of a very long line. The pane lays such a line out in segments (see
// editorpane's longwrap.go) and asks for the spans of one segment at a time. The segment is
// lexed on its own, with a stretch of the text before it so that a string that began above it
// is not read from its middle in most cases, and what is lexed is kept for the text it was
// lexed from, so the frames and the caret queries of one key press lex it once. A line of a
// megabyte costs the lexing of the few segments on screen, and none of the rest.

const (
	// segmentContext is how many runes before a segment the lexer is given.
	segmentContext = 512
	// maxSegSpans is how many segments are kept: those on screen, with some to spare.
	maxSegSpans = 8
)

// segSpans is the lexed spans of one segment of a line, for one version of the text.
type segSpans struct {
	ver       uint64
	line, col int
	n         int // the length of the segment's text in bytes
	spans     []syntax.Span
}

// Segment returns the syntax spans of text, which is runes [col, ...) of line i of doc, as
// byte offsets into text. It returns nil when no lexer matched the document, or when the cache
// is nil.
func (c *Cache) Segment(doc *editor.Document, i, col int, text string) []syntax.Span {
	if c == nil || text == "" {
		return nil
	}
	c.useLexer(doc)
	if c.plain || c.hl.Language() == plainTextLexer {
		return nil
	}
	ver := doc.Buffer.Version()
	for _, s := range c.segs {
		if s.ver == ver && s.line == i && s.col == col && s.n == len(text) {
			return s.spans
		}
	}
	before := ""
	if col > 0 {
		before = doc.Buffer.LineSlice(i, max(col-segmentContext, 0), min(col, segmentContext))
	}
	spans := c.hl.Fragment(before + text)
	out := make([]syntax.Span, 0, len(spans))
	for _, s := range spans {
		// The spans of the stretch before the segment are the lexer's context, not the segment's.
		if s.End <= len(before) {
			continue
		}
		out = append(out, syntax.Span{Start: max(s.Start-len(before), 0), End: s.End - len(before), Kind: s.Kind})
	}
	c.segs = append(c.segs, segSpans{ver: ver, line: i, col: col, n: len(text), spans: out})
	if len(c.segs) > maxSegSpans {
		c.segs = c.segs[len(c.segs)-maxSegSpans:]
	}
	return out
}
