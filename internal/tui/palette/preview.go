package palette

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
)

// The file preview is the file mode's right-hand column: the start of the selected
// file, read in a tea.Cmd and cached for the palette's lifetime. Update never reads a
// file; it starts the read and applies the message it sends back. A large file is read
// only at its head, a binary file is not painted, and a frame too narrow for the
// column paints no preview at all.

const (
	// previewMaxBytes is the head of a file the preview reads. It bounds the read of a
	// large file, so the preview never loads a whole file, however big it is.
	previewMaxBytes = 16 << 10
	// previewMaxLines is the most lines a preview keeps. The column paints at most
	// maxRows of them; the rest are kept for a taller frame in the same palette.
	previewMaxLines = 12
	// previewCacheSize is the number of previews a palette caches. A palette lives for
	// one opening, and the cache is dropped with it.
	previewCacheSize = 64
	// previewMinText is the inner card width, in cells, from which the preview column
	// appears. A narrower frame keeps the old full-width list.
	previewMinText = 64
	// previewMinWidth and previewMaxWidth bound the column the preview takes.
	previewMinWidth = 24
	previewMaxWidth = 60
	// previewShare is the share of the inner width the preview takes: previewShare/5.
	previewShare = 2
	// previewMinList is the cells the list keeps beside the preview, so a path is still
	// readable there.
	previewMinList = 30
	// previewTabSize is the number of cells a tab takes in a preview line.
	previewTabSize = 4
	// previewNoLines is what a file with no lines shows.
	previewNoLines = "(empty file)"
	// previewBinary is what a binary file shows instead of its bytes.
	previewBinary = "Binary file · not previewed"
	// previewUnavailable is what a file that cannot be read shows.
	previewUnavailable = "Preview unavailable"
	// previewLoading is what the column shows until the read arrives.
	previewLoading = "…"
	// previewMore is the mark on the row after the last preview line, when the file
	// continues.
	previewMore = "…"
)

// parseFileTarget splits a file query into the path to rank and an optional line and
// column target, "path:line" or "path:line:col". Fresh's file finder documents this
// form; the preview highlights the target line and Enter jumps to it. A text whose
// suffix is not a positive number is the path itself.
func parseFileTarget(text string) (path string, line, col int) {
	i := strings.LastIndexByte(text, ':')
	if i <= 0 || i == len(text)-1 {
		return text, 0, 0
	}
	n, err := strconv.Atoi(text[i+1:])
	if err != nil || n < 1 {
		return text, 0, 0
	}
	path = text[:i]
	// A second numeric field before the last is the line, and the last is the column.
	if j := strings.LastIndexByte(path, ':'); j > 0 {
		if c, err := strconv.Atoi(path[j+1:]); err == nil && c >= 1 {
			return path[:j], c, n
		}
	}
	return path, n, 0
}

// Previewer reads the start of a file for the file mode's preview. The app implements
// it over the filesystem. ReadHead is called from a tea.Cmd, never from Update or Draw.
type Previewer interface {
	// ReadHead returns at most maxBytes of the file at path. truncated reports that the
	// file is longer than that. err is not nil when the file cannot be read.
	ReadHead(path string, maxBytes int) (data []byte, truncated bool, err error)
}

// Preview is one file's preview: the lines the column paints, or the reason there are
// no lines. It is what the palette caches per file.
type Preview struct {
	// Lines are the first lines of the file, at most previewMaxLines of them.
	Lines []string
	// Binary reports a file whose head is not text, so Lines is nil.
	Binary bool
	// Truncated reports that the file continues past the head or past the lines kept.
	Truncated bool
	// Err is the read error, if any, so Lines is nil.
	Err error
}

// previewMsg carries the read of one preview. The palette applies it only when Seq is
// the read it started last and Rel is still the selected row.
type previewMsg struct {
	Seq     int
	Rel     string
	Preview Preview
}

// newPreview turns the head of a file into what the preview column paints. A head with
// a NUL byte or bytes that are not UTF-8 is binary, so it has no lines; text is split
// into lines, with a partial last line dropped when the file continues past the read.
func newPreview(data []byte, truncated bool) Preview {
	if isBinary(data) {
		return Preview{Binary: true, Truncated: truncated}
	}
	lines := strings.Split(string(data), "\n")
	if truncated && len(lines) > 0 && lines[len(lines)-1] != "" {
		// The head ends in the middle of a line: it is not shown.
		lines = lines[:len(lines)-1]
	}
	if len(lines) > previewMaxLines {
		lines, truncated = lines[:previewMaxLines], true
	}
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1] // a final line break does not start a line
	}
	for i, line := range lines {
		lines[i] = cleanPreviewLine(line)
	}
	return Preview{Lines: lines, Truncated: truncated}
}

// isBinary reports whether data is not text: a NUL byte or a byte sequence that is not
// UTF-8. A multi-byte rune cut off at the end of the read is tolerated, because the
// read ends wherever the byte bound falls.
func isBinary(data []byte) bool {
	if bytes.IndexByte(data, 0) >= 0 {
		return true
	}
	end := len(data)
	for drop := 0; !utf8.Valid(data[:end]) && end > 0; drop++ {
		if drop >= utf8.UTFMax {
			return true
		}
		end--
	}
	return false
}

// cleanPreviewLine makes one file line paintable: the CR of a CRLF ending and control
// characters become spaces, and a tab becomes previewTabSize spaces.
func cleanPreviewLine(line string) string {
	line = strings.TrimSuffix(line, "\r")
	line = strings.ReplaceAll(line, "\t", strings.Repeat(" ", previewTabSize))
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, line)
}

// previewState is the file mode's preview: the reader, the cache, and the preview of
// the selected row.
type previewState struct {
	cache map[string]Preview
	order []string // the cache keys, least recently used first

	rel     string // the file the preview is for
	seq     int    // the read started last; older reads are dropped
	data    Preview
	ready   bool // data is the preview of rel
	loading bool // the read of rel is running
}

// reset drops the shown preview, as when the selected row is not a file.
func (s *previewState) reset() {
	s.rel, s.data, s.ready, s.loading = "", Preview{}, false, false
}

// lookup returns the cached preview of rel and moves rel to the young end of the LRU
// order.
func (s *previewState) lookup(rel string) (Preview, bool) {
	if s.cache == nil {
		return Preview{}, false
	}
	p, ok := s.cache[rel]
	if !ok {
		return Preview{}, false
	}
	for i, key := range s.order {
		if key == rel {
			s.order = append(append(s.order[:i], s.order[i+1:]...), rel)
			break
		}
	}
	return p, true
}

// put caches the preview of rel, evicting the least recently used one.
func (s *previewState) put(rel string, p Preview) {
	if s.cache == nil {
		s.cache = make(map[string]Preview, previewCacheSize)
	}
	if _, ok := s.cache[rel]; ok {
		s.lookup(rel)
		s.cache[rel] = p
		return
	}
	s.cache[rel] = p
	s.order = append(s.order, rel)
	for len(s.order) > previewCacheSize {
		delete(s.cache, s.order[0])
		s.order = s.order[1:]
	}
}

// previewCmd returns the command that reads the selected file's preview, or nil when
// there is nothing to read: the palette is not in file mode, it has no reader, the
// selected row is not a file, or that file's preview is loaded or loading. It is
// called after every message, so moving the highlight starts the read of the new row.
func (p *Palette) previewCmd() tea.Cmd {
	s := &p.preview
	if p.live || !p.inFile || p.opts.Preview == nil {
		s.reset()
		return nil
	}
	i := p.nav.Selected()
	if i < 0 || i >= len(p.rows) || p.rows[i].kind != rowFile {
		s.reset()
		return nil
	}
	rel := p.rows[i].rel
	if rel == s.rel && (s.ready || s.loading) {
		return nil
	}
	if cached, ok := s.lookup(rel); ok {
		s.rel, s.data, s.ready, s.loading = rel, cached, true, false
		return nil
	}
	s.rel, s.data, s.ready, s.loading = rel, Preview{}, false, true
	s.seq++
	seq, read, root := s.seq, p.opts.Preview, p.opts.Root
	path := filepath.Join(root, rel)
	return func() tea.Msg {
		data, truncated, err := read.ReadHead(path, previewMaxBytes)
		if err != nil {
			return previewMsg{Seq: seq, Rel: rel, Preview: Preview{Err: err}}
		}
		return previewMsg{Seq: seq, Rel: rel, Preview: newPreview(data, truncated)}
	}
}

// applyPreview shows a read that answers the current selection, and caches it.
func (p *Palette) applyPreview(msg previewMsg) {
	s := &p.preview
	if msg.Seq != s.seq || msg.Rel != s.rel {
		return
	}
	s.data, s.ready, s.loading = msg.Preview, true, false
	s.put(msg.Rel, msg.Preview)
}

// previewWanted reports whether the file mode has a preview column to paint: it needs a
// reader. The layout decides the width, so a narrow frame still paints no column.
func (p *Palette) previewWanted() bool {
	return p != nil && !p.live && p.inFile && p.opts.Preview != nil
}

// paintPreview draws the preview column inside the card, to the right of the rows: the
// divider, the selected file's head with a line-number gutter, and the highlighted
// target line of a "path:line" query. It paints nothing when the layout gave no column.
func (p *Palette) paintPreview(g *grid.Grid, geo geometry) {
	if !geo.preview {
		return
	}
	muted := p.chrome.muted
	for k := range geo.rows {
		g.Put(geo.divX, geo.rowsTop+k, geo.divX+1, "│", muted)
	}
	s := &p.preview
	if !s.ready {
		g.Put(geo.prevX, geo.rowsTop, geo.prevEnd, previewLoading, muted)
		return
	}
	switch {
	case s.data.Err != nil:
		g.Put(geo.prevX, geo.rowsTop, geo.prevEnd, previewUnavailable, muted)
		return
	case s.data.Binary:
		g.Put(geo.prevX, geo.rowsTop, geo.prevEnd, previewBinary, muted)
		return
	}
	lines := s.data.Lines
	if len(lines) == 0 {
		g.Put(geo.prevX, geo.rowsTop, geo.prevEnd, previewNoLines, muted)
		return
	}
	gutter := len(strconv.Itoa(max(len(lines), p.fileLine)))
	for k := 0; k < geo.rows && k < len(lines); k++ {
		y := geo.rowsTop + k
		st := p.chrome.panel
		if p.fileLine == k+1 {
			st = p.chrome.selected
			g.Fill(geo.prevX, geo.prevEnd, y, st)
		}
		num := fmt.Sprintf("%*d", gutter, k+1)
		label := num + " │ " + strings.TrimRight(lines[k], " ")
		g.Put(geo.prevX, y, geo.prevEnd, truncate(label, geo.prevEnd-geo.prevX), st)
	}
	if s.data.Truncated && len(lines) < geo.rows {
		g.Put(geo.prevX, geo.rowsTop+len(lines), geo.prevEnd, previewMore, muted)
	}
}
