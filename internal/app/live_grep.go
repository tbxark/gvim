package app

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/search"
)

// liveGrep is the palette's Grepper over the search package. It reads the project files
// one at a time, within the limits above, and searches each one with the query the
// palette asked for. The open buffers' text comes in the request, so it needs no IO.
// It is called from a tea.Cmd.
type liveGrep struct {
	// skipFrom is the size in bytes from which a file is skipped: the files that open
	// in large file mode. Only the first pluginhost.GrepMaxFileBytes of such a file would be
	// searched, and a miss in the rest would look like no match. Zero skips none.
	skipFrom int64

	// mu guards the listing of the hidden and ignored files. That listing walks the
	// tree, which is expensive, so it is cached for the root last searched.
	mu       sync.Mutex
	walkRoot string
	walkList []string
}

// grepFor returns the live grep for an editor with the options o. It skips the
// files that the editor opens in large file mode.
func grepFor(o editor.Options) *liveGrep {
	g := &liveGrep{}
	if o.LargeFileMode && o.LargeFileThresholdBytes > 0 {
		g.skipFrom = int64(o.LargeFileThresholdBytes)
	}
	return g
}

// Search returns at most req.Limit hits of req.Text, one per matching line: the file
// hits first, in file order, then the buffer hits in buffer order, then the terminal
// transcript hits in dock-tab/line order. The query is matched with the modes of
// req.Query. A regex that does not compile is an error, which the palette shows as a
// message row. The files are relative to req.Root. A file that cannot be read is
// skipped, and so is a file of skipFrom bytes or more. The terminal and buffer
// snapshots come in the request: searching them is in-memory text work, never a PTY
// or process read.
func (g *liveGrep) Search(req palette.GrepRequest) ([]palette.GrepHit, error) {
	if req.Text == "" || req.Limit <= 0 {
		return nil, nil
	}
	q := search.Query{
		Text:          req.Text,
		CaseSensitive: req.Query.CaseSensitive,
		WholeWord:     req.Query.WholeWord,
		Regex:         req.Query.Regex,
	}
	// Compile up front, so an invalid regex is reported even when no file is searched
	// and the palette shows the error instead of no matches.
	if q.Regex {
		if _, err := regexp.Compile(q.Text); err != nil {
			return nil, err
		}
	}
	var hits []palette.GrepHit
	if req.Sources.Files || req.Sources.Ignored {
		files := req.Files
		if req.Sources.Ignored {
			// The ignored scope searches hidden and .gitignore-matched files too, which
			// the file index does not list: the walk lists them, and its result is
			// cached for the root.
			files = g.ignoredFiles(req.Root)
		}
		var err error
		if hits, err = searchFiles(req.Root, files, q, req.Limit, hits, g.skipFrom); err != nil {
			return nil, err
		}
	}
	if len(hits) < req.Limit && req.Sources.Buffers {
		var err error
		if hits, err = searchBuffers(req.Buffers, q, req.Limit, hits); err != nil {
			return nil, err
		}
	}
	if len(hits) < req.Limit && req.Sources.Terminals {
		var err error
		if hits, err = searchTerminals(req.Terminals, q, req.Limit, hits); err != nil {
			return nil, err
		}
	}
	return hits, nil
}

// searchFiles appends the hits of q in the files of root to hits, in file order and one
// per matching line, up to limit hits.
func searchFiles(root string, files []string, q search.Query, limit int, hits []palette.GrepHit, skipFrom int64) ([]palette.GrepHit, error) {
	for i, rel := range files {
		if i >= pluginhost.GrepMaxFiles || len(hits) >= limit {
			break
		}
		text, ok := pluginhost.ReadSearchable(filepath.Join(root, filepath.FromSlash(rel)), skipFrom)
		if !ok {
			continue
		}
		b := buffer.New(text)
		matches, err := search.Find(b, q)
		if err != nil {
			return nil, err
		}
		last := -1
		for _, m := range matches {
			if m.Start.Line == last {
				continue
			}
			last = m.Start.Line
			hits = append(hits, palette.GrepHit{
				Rel:    rel,
				Line:   m.Start.Line + 1,
				Col:    m.Start.Col + 1,
				Text:   strings.TrimRight(b.Line(m.Start.Line), "\r"),
				Source: palette.GrepFiles,
			})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// searchBuffers appends the hits of q in the open buffers' snapshot to hits, in buffer
// order and one per matching line, up to limit hits. A buffer with no text is skipped.
func searchBuffers(buffers []palette.BufferSource, q search.Query, limit int, hits []palette.GrepHit) ([]palette.GrepHit, error) {
	for _, src := range buffers {
		if len(hits) >= limit {
			break
		}
		if src.Text == "" {
			continue
		}
		b := buffer.New(src.Text)
		matches, err := search.Find(b, q)
		if err != nil {
			return nil, err
		}
		label := src.Name
		if label == "" {
			label = src.Path
		}
		if label == "" {
			label = "[No Name]"
		}
		last := -1
		for _, m := range matches {
			if m.Start.Line == last {
				continue
			}
			last = m.Start.Line
			hits = append(hits, palette.GrepHit{
				Rel:    label,
				Line:   m.Start.Line + 1,
				Col:    m.Start.Col + 1,
				Text:   strings.TrimRight(b.Line(m.Start.Line), "\r"),
				Source: palette.GrepBuffers,
				Index:  src.Index,
			})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// searchTerminals appends the hits of q in the bounded dock terminal snapshots to
// hits, in dock-tab order and one per matching line, up to limit hits. The snapshot is
// already bounded by the palette/root caps (≤500 rows/1 MiB per tab, ≤16 tabs/4 MiB
// total), so this is a bounded in-memory search: no PTY read, no process, no IO.
func searchTerminals(terms []palette.TerminalSource, q search.Query, limit int, hits []palette.GrepHit) ([]palette.GrepHit, error) {
	for _, src := range terms {
		if len(hits) >= limit {
			break
		}
		if len(src.Lines) == 0 {
			continue
		}
		b := buffer.New(strings.Join(src.Lines, "\n"))
		matches, err := search.Find(b, q)
		if err != nil {
			return nil, err
		}
		label := src.Name
		if label == "" {
			label = "terminal"
		}
		last := -1
		for _, m := range matches {
			if m.Start.Line == last {
				continue
			}
			last = m.Start.Line
			hits = append(hits, palette.GrepHit{
				Rel:                 label,
				Line:                m.Start.Line + 1,
				Col:                 m.Start.Col + 1,
				Text:                strings.TrimRight(b.Line(m.Start.Line), "\r"),
				Source:              palette.GrepTerminals,
				Index:               src.Index,
				TerminalID:          src.ID,
				TranscriptFirst:     src.First,
				TerminalTotal:       src.Total,
				TerminalBefore:      terminalContextBefore(src.Lines, m.Start.Line),
				TerminalAfter:       terminalContextAfter(src.Lines, m.Start.Line),
				TerminalSnapshot:    true,
				TerminalValid:       true,
				TerminalBeforeKnown: m.Start.Line > 0,
				TerminalAfterKnown:  m.Start.Line+1 < len(src.Lines),
			})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// terminalContextBefore/After return the snapshot neighbours of the terminal hit at
// line index (0-based), or "" at the window edge. They let a reveal validate the
// snapshot row after output/eviction and disambiguate duplicate lines; the strings are
// copied into the hit, so the export can carry them too.
func terminalContextBefore(lines []string, index int) string {
	if index <= 0 || index > len(lines) {
		return ""
	}
	return lines[index-1]
}

func terminalContextAfter(lines []string, index int) string {
	if index < 0 || index+1 >= len(lines) {
		return ""
	}
	return lines[index+1]
}

// ignoredFiles lists the files of root including the hidden and .gitignore-matched
// ones, for the Ignored scope. The list stops at finder.MaxFiles and is cached for the
// root last searched, so typing in the palette does not walk the tree on every key. A
// listing error gives the files found so far.
func (g *liveGrep) ignoredFiles(root string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.walkRoot == root && g.walkList != nil {
		return g.walkList
	}
	list := make([]string, 0, 1024)
	_ = finder.EnumerateAll(context.Background(), root, finder.Options{ShowHidden: true, ShowIgnored: true}, func(rel string) bool {
		list = append(list, rel)
		return len(list) < finder.MaxFiles
	})
	g.walkRoot, g.walkList = root, list
	return list
}
