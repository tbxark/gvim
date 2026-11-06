package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/ui/palette"
)

// Limits of a live grep. A query reads at most grepMaxFiles files of the index,
// and at most grepMaxFileBytes of each one. A file that looks binary is skipped.
const (
	grepMaxFiles     = 2000
	grepMaxFileBytes = 1 << 20
	grepBinaryProbe  = 8000
)

// liveGrep is the palette's Grepper over internal/search. It reads the files of the
// index one at a time, within the limits above, and searches each one for the query
// as literal text, without regard to case. It is called from a tea.Cmd.
type liveGrep struct{}

// Search returns at most limit hits of query, one per matching line, in file order.
// The files are relative to root. A file that cannot be read is skipped.
func (liveGrep) Search(root string, files []string, query string, limit int) ([]palette.GrepHit, error) {
	if query == "" || limit <= 0 {
		return nil, nil
	}
	var hits []palette.GrepHit
	for i, rel := range files {
		if i >= grepMaxFiles {
			break
		}
		text, ok := readSearchable(filepath.Join(root, filepath.FromSlash(rel)))
		if !ok {
			continue
		}
		b := buffer.New(text)
		matches, err := search.Find(b, search.Query{Text: query})
		if err != nil {
			return hits, err
		}
		last := -1
		for _, m := range matches {
			if m.Start.Line == last {
				continue
			}
			last = m.Start.Line
			hits = append(hits, palette.GrepHit{
				Rel:  rel,
				Line: m.Start.Line + 1,
				Col:  m.Start.Col + 1,
				Text: strings.TrimRight(b.Line(m.Start.Line), "\r"),
			})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// readSearchable returns the first grepMaxFileBytes of the file at path. It reports
// false when the file cannot be read or looks binary: a NUL byte in its first
// grepBinaryProbe bytes.
func readSearchable(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, grepMaxFileBytes))
	if err != nil {
		return "", false
	}
	if bytes.IndexByte(data[:min(len(data), grepBinaryProbe)], 0) >= 0 {
		return "", false
	}
	return string(data), true
}
