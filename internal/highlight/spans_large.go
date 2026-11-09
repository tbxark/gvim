package highlight

import (
	"slices"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/syntax"
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
	// hotBack and hotAhead are the same for the first window after the text changed. A
	// key typed in a big document lexes a window again, and most of the lines of a wide
	// one are not on screen yet: a window as long as the screen is cheaper by a factor of
	// three, and the lines past it are lexed when they are scrolled to.
	hotBack  = 24
	hotAhead = 80
	// maxContextLines bounds the context in lines, for a document of short lines.
	maxContextLines = 2000
	// maxSpanWindows is how many windows are kept: one for each pane that shows the
	// document.
	maxSpanWindows = 4
	// windowedBytes is the size of the text from which a document is highlighted
	// through windows, as a large one is. Lexing a whole text again after each edit
	// takes about a millisecond per KiB, which a keystroke cannot afford past this.
	windowedBytes = 64 << 10
	// maxLexedLineBytes is the longest line that is lexed. A longer line has no spans: the
	// lexer allocates a few hundred bytes a character, so a paragraph of a few thousand
	// characters costs milliseconds and megabytes on every key press while it is typed,
	// and a line of a megabyte costs seconds. Vim's synmaxcol and VS Code's
	// maxTokenizationLineLength are the same bound.
	maxLexedLineBytes = 4096
	// plainTextLexer is the name of the lexer chroma matches to .txt files.
	plainTextLexer = "plaintext"
)

// lineSize is the length of line i that the highlighting guards compare with its limits. A large
// buffer has the bytes of the line from its index. A memory buffer has the runes, which it
// counts in no time, where its bytes would be counted by reading the line: a line of megabytes
// would cost that on every key. The runes are the fewer, so a line of multibyte text is lexed
// a little past the limit in bytes.
func lineSize(b *buffer.Buffer, i int) int {
	if b.Large() {
		return b.LineBytes(i)
	}
	return b.LineLen(i)
}

// largeSpans holds the lexed windows of one large document at one version of its
// text.
type largeSpans struct {
	buf     *buffer.Buffer
	version uint64
	// gen is the asynchronous window-arrival generation at which wins were lexed, or
	// zero for a local file. A remote window that arrives changes no text version, yet
	// its lines were blank when the window was lexed; a new generation therefore drops
	// the windows and lets the next paint lex them with the loaded text.
	gen  uint64
	wins []spanWindow // newest first
	// hot is set when the text changed since the last window was lexed: the next window is
	// a short one.
	hot bool
}

// spanWindow is a lexed run of lines. It answers for the lines from served to last.
type spanWindow struct {
	served, last int
	first        int
	spans        [][]syntax.Span // by line - first
}

// Fresh returns an empty cache with the settings of c. A nil c gives a plain empty
// cache.
func (c *Cache) Fresh() *Cache {
	n := &Cache{}
	if c != nil {
		n.contextBytes, n.contextSet = c.contextBytes, c.contextSet
	}
	return n
}

// SetContext sets how many bytes of lines above the lines shown the lexer is given
// (highlight_context_bytes). A nil cache ignores it.
func (c *Cache) SetContext(n int) {
	if c != nil {
		c.contextBytes, c.contextSet = max(n, 0), true
	}
}

// context is the bytes of context lines above a window.
func (c *Cache) context() int {
	if c.contextSet {
		return c.contextBytes
	}
	return defaultContextBytes
}

// Window returns the syntax spans of line i of the large document doc. The lexer runs
// over a window of lines around i, and the result is kept while the text has the
// same version, so scrolling inside the window and moving the caret lex nothing.
// A line too long to be painted whole has none. It returns nil when no lexer matched
// the document, or when the cache is nil.
func (c *Cache) window(doc *editor.Document, i int) []syntax.Span {
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
	gen := b.WindowGeneration()
	genChanged := c.large.buf == b && c.large.gen != gen
	if c.large.buf != b || c.large.version != ver || c.large.gen != gen {
		// A buffer seen for the first time is not an edit: it is scrolled to. A version
		// change is an edit and asks for a short window. A generation change is a remote
		// window arriving: the text version is the same, so the window is not hot, but
		// the spans must be lexed again because the loaded lines replaced blanks. The
		// old windows are dropped either way.
		hot := c.large.buf == b && c.large.version != ver
		c.large = largeSpans{buf: b, version: ver, gen: gen, hot: hot}
	}
	if genChanged {
		// The text version did not change when a remote window arrived, so the
		// highlighter's window cache (keyed by version, first line and length) would
		// otherwise return the spans lexed from the blank lines. Drop it; the next
		// LinesWindow lexes the loaded text.
		c.hl.Invalidate()
	}
	for _, w := range c.large.wins {
		if i >= w.served && i <= w.last {
			return w.spans[i-w.first]
		}
	}
	w := c.lexWindow(doc, b, ver, i, c.large.hot)
	c.large.hot = false
	c.large.wins = slices.Insert(c.large.wins, 0, w)
	if len(c.large.wins) > maxSpanWindows {
		c.large.wins = c.large.wins[:maxSpanWindows]
	}
	return w.spans[i-w.first]
}

// lexWindow lexes the lines around line i, a short window when hot. A line of an
// asynchronous window source that has not arrived is left blank (and gets no spans):
// reading it would turn highlighting a line on screen into an extra remote window
// load. The window-generation key above re-lexes after such a window arrives.
func (c *Cache) lexWindow(doc *editor.Document, b *buffer.Buffer, ver uint64, i int, hot bool) spanWindow {
	back, ahead := windowBack, windowAhead
	if hot {
		back, ahead = hotBack, hotAhead
	}
	served := max(i-back, 0)
	first := max(served-contextLines(doc, b, served, c.context()), 0)
	last := min(i+ahead, b.Lines()-1)
	if first == 0 {
		served = 0
	}
	text := make([]string, last-first+1)
	var noSpans []int
	for k := range text {
		if !doc.LineLoaded(first + k) {
			// Do not read a line whose remote window is not loaded: a read returns the
			// blank placeholder and records a window miss the paint did not need.
			noSpans = append(noSpans, k)
			continue
		}
		if lineSize(b, first+k) > maxLexedLineBytes {
			// Lexing such a line would cost more than the rest of the window together,
			// and the pane paints a line past editorpane.LongLineRunes as a window of its
			// own that wants no spans.
			noSpans = append(noSpans, k)
			continue
		}
		text[k] = b.Line(first + k)
	}
	spans := slices.Clone(c.hl.LinesWindow(int(ver), first, text).Spans)
	for _, k := range noSpans {
		spans[k] = nil
	}
	return spanWindow{served: served, last: last, first: first, spans: spans}
}

// contextLines is how many lines above line from hold about bytes bytes, or fewer
// when the document, maxContextLines, or an unloaded remote window ends first. An
// unloaded line is not measured: lineSize would read its window and make highlighting
// a context line perform remote I/O the paint did not ask for.
func contextLines(doc *editor.Document, b *buffer.Buffer, from, bytes int) int {
	n, sum := 0, 0
	for j := from - 1; j >= 0 && n < maxContextLines && sum < bytes; j-- {
		if !doc.LineLoaded(j) {
			break
		}
		sum += lineSize(b, j) + 1
		n++
	}
	return n
}

// Windowed reports whether doc is highlighted through windows: a large document, and
// a document of more than windowedBytes. The size is measured again only after the
// text changed. A nil cache answers as for a large document alone.
func (c *Cache) Windowed(doc *editor.Document) bool {
	if doc.Large() {
		return true
	}
	if c == nil {
		return false
	}
	if ver := doc.Version(); !c.sizedOK || c.sized != ver {
		n := 0
		for i, lines := 0, doc.Buffer.Lines(); i < lines && n <= windowedBytes; i++ {
			n += lineSize(doc.Buffer, i) + 1
		}
		c.sized, c.sizedOK, c.big = ver, true, n > windowedBytes
	}
	return c.big
}

// Generation reports how many actions that may have edited the text the cache has
// seen. The spans are current for the generation they were lexed at, so a caller can
// tell a refresh from the stale state the action left. It is for the tests of the
// adapters that mark the spans stale.
func (c *Cache) Generation() uint64 {
	if c == nil {
		return 0
	}
	return c.gen
}
