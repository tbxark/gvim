package app

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/search"
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
type liveGrep struct {
	// skipFrom is the size in bytes from which a file is skipped: the files that open
	// in large file mode. Only the first grepMaxFileBytes of such a file would be
	// searched, and a miss in the rest would look like no match. Zero skips none.
	skipFrom int64
}

// grepFor returns the live grep for an editor with the options o. It skips the
// files that the editor opens in large file mode.
func grepFor(o editor.Options) liveGrep {
	if o.LargeFileMode && o.LargeFileThresholdBytes > 0 {
		return liveGrep{skipFrom: int64(o.LargeFileThresholdBytes)}
	}
	return liveGrep{}
}

// Search returns at most limit hits of query, one per matching line, in file order.
// The files are relative to root. A file that cannot be read is skipped, and so is a
// file of skipFrom bytes or more.
func (g liveGrep) Search(root string, files []string, query string, limit int) ([]palette.GrepHit, error) {
	if query == "" || limit <= 0 {
		return nil, nil
	}
	var hits []palette.GrepHit
	for i, rel := range files {
		if i >= grepMaxFiles {
			break
		}
		text, ok := readSearchable(filepath.Join(root, filepath.FromSlash(rel)), g.skipFrom)
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
// false when the file cannot be read, is skipFrom bytes or more (when skipFrom is not
// zero), or looks binary: a NUL byte in its first grepBinaryProbe bytes.
func readSearchable(path string, skipFrom int64) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	if skipFrom > 0 {
		if info, err := f.Stat(); err != nil || info.Size() >= skipFrom {
			return "", false
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, grepMaxFileBytes))
	if err != nil {
		return "", false
	}
	if bytes.IndexByte(data[:min(len(data), grepBinaryProbe)], 0) >= 0 {
		return "", false
	}
	return string(data), true
}
