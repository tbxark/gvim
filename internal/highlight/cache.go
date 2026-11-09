package highlight

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/syntax"
)

// Cache holds the syntax spans of the active document. The lexer runs over
// the whole text, because chroma is a stream lexer, and the result is kept until
// the next change of the text, which its version tells: an action that only moves
// the caret or the viewport lexes nothing, although it calls Edited. A document
// that is too big to lex per keystroke is lexed in windows (windowed). A document switch, or a change of its language, starts a new lexer. A
// large document is lexed in windows of lines instead, kept by the version of its
// text (spans_large.go). The cache is used on the UI goroutine only.
type Cache struct {
	gen   uint64           // bumped by every action that may edit
	doc   *editor.Document // the document the lexer belongs to
	lang  string           // the language the lexer was chosen for, as set with Set Language
	plain bool             // no lexer matched the document: no spans
	// headDetected says the lexer was chosen with the head of the document. A
	// windowed remote large document whose first window has not arrived is detected
	// from its path alone, and detected again after the paint loads the head, so the
	// detection never turns a context read into an extra remote window load.
	headDetected bool
	hl           *syntax.Highlighter
	spans        [][]syntax.Span
	valid        bool           // spans were lexed
	lexedAt      uint64         // gen the spans were lexed at
	lexedV       editor.Version // the text version the spans were lexed at
	lexes        int            // how many times the whole text was lexed, the version Highlighter sees

	// sized is the text version size was measured at, and big whether the text was
	// then over windowedBytes.
	sized   editor.Version
	sizedOK bool
	big     bool

	// The spans of a large document are lexed in windows, see spans_large.go.
	large        largeSpans
	contextBytes int  // highlight_context_bytes; read only when contextSet
	contextSet   bool // contextBytes was set from the config
	// segs are the spans of the pieces of very long lines, see spans_long.go.
	segs []segSpans
}

// New returns an empty cache with the default context.
func New() *Cache {
	return &Cache{}
}

// Edited marks the spans stale. It is called before every action. A nil cache
// is a zero adapter with no spans, and it is ignored.
func (c *Cache) Edited() {
	if c != nil {
		c.gen++
	}
}

// Lines returns the spans of every line of doc, lexing again only after an
// edit. It returns nil when no lexer matched the document, or when the cache is
// nil.
func (c *Cache) Lines(doc *editor.Document) [][]syntax.Span {
	if c == nil {
		return nil
	}
	c.useLexer(doc)
	if c.plain || c.Windowed(doc) {
		// A large document is never lexed whole: see window.
		return nil
	}
	// Only a change of the text lexes again. An action that moves the caret or the
	// viewport marks the cache edited, but the version of the text is the same.
	if ver := doc.Version(); !c.valid || c.lexedV != ver {
		text := make([]string, doc.Buffer.Lines())
		for i := range text {
			if lineSize(doc.Buffer, i) <= maxLexedLineBytes {
				text[i] = doc.Buffer.Line(i)
			}
		}
		c.lexes++
		c.spans = c.hl.Lines(c.lexes, text)
		c.lexedAt = c.gen
		c.lexedV = ver
		c.valid = true
	}
	return c.spans
}

// LineSpans returns the syntax spans of line i of doc, or nil when no lexer
// matched the document or i is out of range. It is the view the text pane
// paints: a line of a large or windowed document comes from its window.
func (c *Cache) LineSpans(doc *editor.Document, i int) []syntax.Span {
	if c.Windowed(doc) {
		// The lexer runs over a window of the lines on screen, not over the text.
		return c.window(doc, i)
	}
	spans := c.Lines(doc)
	if i < 0 || i >= len(spans) {
		return nil
	}
	return spans[i]
}

// useLexer starts a new lexer when the cache is used for another document, or when
// the language of the document changed.
func (c *Cache) useLexer(doc *editor.Document) {
	headLoaded := doc.LineLoaded(0)
	if c.doc == doc && c.lang == doc.Language() && (c.headDetected || !headLoaded) {
		return
	}
	c.doc = doc
	c.lang = doc.Language()
	c.valid = false
	c.large = largeSpans{}
	c.segs = nil
	lexer, headDetected := documentLexer(doc)
	c.headDetected = headDetected
	c.plain = lexer == lexers.Fallback
	c.hl = syntax.NewHighlighter(lexer)
}

// documentLexer returns the lexer of doc and whether the head was available to choose
// it. A language set with Set Language names the lexer, when the name matches one.
// Otherwise the file name and the text choose it, as syntax.Detect does. A windowed
// remote large document whose first window has not arrived is detected from its path
// alone: reading the head would turn filling a syntax cache into an extra remote
// window load. Once the paint has loaded the head, useLexer detects again.
func documentLexer(doc *editor.Document) (chroma.Lexer, bool) {
	if name := doc.Language(); name != "" {
		if lexer := lexers.Get(name); lexer != nil {
			return lexer, true
		}
	}
	if !doc.LineLoaded(0) {
		return syntax.Detect(doc.Path, nil), false
	}
	// Detect looks at the head of the text only, so the head is all it is given: a
	// large document never builds its whole text for this.
	return syntax.Detect(doc.Path, []byte(doc.Buffer.TextPrefix(lexerHeadBytes))), true
}

// lexerHeadBytes is how much of the start of a document syntax.Detect is given. It is
// at least what Detect reads (its analyse limit is 16 KiB).
const lexerHeadBytes = 16 << 10
