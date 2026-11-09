package app

// Syntax-coloured preview windows shared by the local and remote project-search panels.
// The panel paints PreviewSpan runs with its theme's syntax styles; this helper lexes the
// window with the same syntax.Highlighter the editor uses, caches one highlighter per file
// (bounded count/size/lines), and returns no spans (plain text) above the caps or without a
// grammar. Everything here is CPU work inside the preview command: no file I/O, no local
// fallback.

import (
	"sync"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/syntax"
)

// Bounds of the syntax-coloured preview: a bigger file is plain text, so the cached
// highlighter stays small.
const (
	previewHighlightBytes       = 256 << 10
	previewHighlightLines       = 4000
	previewHighlighterCacheSize = 8
)

// previewHighlighter is one preview cache entry: a highlighter and the content hash it was
// built for.
type previewHighlighter struct {
	hash uint64
	h    *syntax.Highlighter
}

// previewHighlighterCache is the bounded per-file highlighter cache of one previewer. The
// mutex makes concurrent preview commands (a panel can have more than one in flight) safe.
type previewHighlighterCache struct {
	mu      sync.Mutex
	entries map[string]*previewHighlighter
	order   []string // least recently rebuilt first
}

// spans returns the syntax-coloured runs of the preview window. It reuses a cached
// highlighter per file (keyed by path and content hash), rebuilds it when the file changed,
// and returns nothing, plain text, for a file over the size or line caps or without a
// grammar.
func (c *previewHighlighterCache) spans(path, text string, lines []string, first, count int) []searchpanel.PreviewSpan {
	if count <= 0 || len(text) > previewHighlightBytes || len(lines) > previewHighlightLines {
		return nil
	}
	hash := hashText(text)
	c.mu.Lock()
	entry := c.entries[path]
	if entry == nil || entry.hash != hash {
		entry = &previewHighlighter{hash: hash, h: syntax.NewHighlighter(syntax.Detect(path, []byte(text)))}
		if c.entries == nil {
			c.entries = make(map[string]*previewHighlighter)
		}
		c.entries[path] = entry
		for i, old := range c.order {
			if old == path {
				c.order = append(c.order[:i], c.order[i+1:]...)
				break
			}
		}
		c.order = append(c.order, path)
		for len(c.order) > previewHighlighterCacheSize {
			oldest := c.order[0]
			c.order = c.order[1:]
			if oldest != path {
				delete(c.entries, oldest)
			}
		}
	}
	h := entry.h
	c.mu.Unlock()
	all := h.Lines(int(hash), lines)
	var out []searchpanel.PreviewSpan
	for i := 0; i < count && first+i < len(all); i++ {
		abs := first + i
		line := lines[abs]
		for _, span := range all[abs] {
			if span.Kind == syntax.KindPlain {
				continue
			}
			start := utf8.RuneCountInString(line[:min(span.Start, len(line))])
			end := utf8.RuneCountInString(line[:min(span.End, len(line))])
			if end > start {
				out = append(out, searchpanel.PreviewSpan{Line: abs, Start: start, End: end, Kind: span.Kind.String()})
			}
		}
	}
	return out
}
