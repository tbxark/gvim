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
	wins    []windowEntry // newest first, see LinesWindow
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
	h.wins = nil
}

// windowCacheSize is how many windows a Highlighter keeps. A window is the lexed
// run of lines around one viewport, so this is how many panes of one document can
// scroll without lexing each other's lines again.
const windowCacheSize = 4

// Window is the spans of a run of consecutive lines of a buffer, which starts at
// line First. A document too large to lex whole is highlighted through windows
// around the lines on screen.
type Window struct {
	First int
	Spans [][]Span
}

// Line returns the spans of line i, or nil when i is outside the window.
func (w Window) Line(i int) []Span {
	k := i - w.First
	if k < 0 || k >= len(w.Spans) {
		return nil
	}
	return w.Spans[k]
}

// windowKey identifies a lexed window: the version of the buffer, the line the
// window starts at and the number of lines it holds.
type windowKey struct{ version, first, lines int }

type windowEntry struct {
	key windowKey
	win Window
}

// LinesWindow returns the spans of text, which are the lines of a buffer from line
// first on, at the given buffer version. Only the window is lexed, so the text
// before it is not known: a token that began above the window is read from the
// window's own start. The caller passes enough lines above the lines it shows for
// that to be rare.
//
// The newest windows are kept, by (version, first, len(text)), so two panes that
// scroll the same buffer do not lex each other's lines again. Like Lines, it
// returns plain spans when lexing fails or text is larger than MaxHighlightBytes.
// Spans are shared with the cache, so callers must not modify them.
func (h *Highlighter) LinesWindow(version, first int, text []string) Window {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := windowKey{version: version, first: first, lines: len(text)}
	for i, e := range h.wins {
		if e.key == key {
			copy(h.wins[1:i+1], h.wins[:i])
			h.wins[0] = e
			return e.win
		}
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
	win := Window{First: first, Spans: spans}
	h.wins = append(h.wins, windowEntry{})
	copy(h.wins[1:], h.wins)
	h.wins[0] = windowEntry{key: key, win: win}
	if len(h.wins) > windowCacheSize {
		h.wins = h.wins[:windowCacheSize]
	}
	return win
}
