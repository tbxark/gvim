package syntax

import (
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// MaxHighlightBytes is the largest buffer, counted as its lines plus one
// newline each, that Lines lexes. A larger buffer gets plain spans. The value
// is proposed at 2 MiB; see BenchmarkTokenizeGo for the measured lexing rate.
const MaxHighlightBytes = 2 << 20

// Highlighter caches the spans for one buffer. Chroma lexers are stream-based,
// so the whole buffer is lexed once per version, not per visible line.
//
// Contract: the caller passes a version number that changes on every text
// edit. Cursor moves and scrolls do not need a new version. Lines returns the
// cached spans while the version and line count are unchanged. A version that
// changes, or a line count that changes, causes a re-lex. Invalidate forces
// one.
//
// A Highlighter is safe for concurrent use. The slices Lines returns are shared
// with the cache, so callers must not modify them.
type Highlighter struct {
	mu      sync.Mutex
	lexer   chroma.Lexer
	valid   bool
	version int
	lines   int
	spans   [][]Span
	skipped bool
}

// NewHighlighter returns a Highlighter for lexer. A nil lexer means
// lexers.Fallback, so the result never highlights anything.
func NewHighlighter(lexer chroma.Lexer) *Highlighter {
	if lexer == nil {
		lexer = lexers.Fallback
	}
	return &Highlighter{lexer: lexer}
}

// Language returns the chroma lexer name in use, such as "Go" or "fallback".
func (h *Highlighter) Language() string {
	return h.lexer.Config().Name
}

// Lines returns the spans for each line of text at the given buffer version.
// The result has len(text) entries. Each entry is ordered by Start and covers
// the line, with KindPlain spans for unclaimed text.
//
// If lexing fails, or the buffer is larger than MaxHighlightBytes, Lines
// returns one KindPlain span per line instead of an error, so painting never
// breaks. The result is cached until the version changes. Skipped reports
// whether the size guard applied.
func (h *Highlighter) Lines(version int, text []string) [][]Span {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.valid && h.version == version && h.lines == len(text) {
		return h.spans
	}
	var spans [][]Span
	h.skipped = size(text) > MaxHighlightBytes
	if !h.skipped {
		if s, err := Tokenize(h.lexer, text); err == nil {
			spans = s
		}
	}
	if spans == nil {
		spans = plainSpans(text)
	}
	h.spans = spans
	h.version = version
	h.lines = len(text)
	h.valid = true
	return spans
}

// Skipped reports whether the last Lines call skipped lexing because the
// buffer was larger than MaxHighlightBytes.
func (h *Highlighter) Skipped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.skipped
}

// size returns the byte count of text as joined with newlines.
func size(text []string) int {
	n := len(text)
	for _, l := range text {
		n += len(l)
	}
	return n
}

// Invalidate drops the cached spans. The next Lines call re-lexes, whatever
// version it is given.
func (h *Highlighter) Invalidate() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.valid = false
	h.spans = nil
}
