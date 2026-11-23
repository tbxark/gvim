package app

import (
	"context"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui/palette"
	"github.com/tbxark/gvim/search"
)

// liveGrep is the palette's Grepper over the search package. It reads the project files
// one at a time, within pluginhost's grep limits (GrepMaxFiles, GrepMaxFileBytes), and searches each one with the query the
// palette asked for. The open buffers' text comes in the request, so it needs no IO.
// It is called from a tea.Cmd.
type liveGrep struct {
	// skipFrom is the size in bytes from which a file is skipped: the files that open
	// in large file mode. Only the first pluginhost.GrepMaxFileBytes of such a file would be
	// searched, and a miss in the rest would look like no match. Zero skips none.
	skipFrom int64

	customMu      sync.Mutex
	customMatcher gitignore.Matcher

	// mu guards the listing of the hidden and ignored files. That listing walks the
	// tree, which is expensive, so it is cached for the root last searched, for
	// ignoredWalkTTL: typing in the palette reuses it, and a file created since is
	// found once it expires.
	mu       sync.Mutex
	walkRoot string
	walkList []string
	walkAt   time.Time
}

// ignoredWalkTTL is how long the walk of the ignored scope is reused.
const ignoredWalkTTL = 5 * time.Second

// grepFor returns the live grep for an editor with the options o. It skips the
// files that the editor opens in large file mode.
func grepFor(o editor.Options, customPatterns ...[]string) *liveGrep {
	g := &liveGrep{}
	if o.LargeFileMode && o.LargeFileThresholdBytes > 0 {
		g.skipFrom = int64(o.LargeFileThresholdBytes)
	}
	if len(customPatterns) > 0 {
		g.SetCustomIgnorePatterns(customPatterns[0])
	}
	return g
}

// SetCustomIgnorePatterns configures custom ignore rules (e.g. *.log, build/).
func (g *liveGrep) SetCustomIgnorePatterns(pats []string) {
	var rules []gitignore.Pattern
	for _, p := range pats {
		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}
		rules = append(rules, gitignore.ParsePattern(p, nil))
	}
	var matcher gitignore.Matcher
	if len(rules) > 0 {
		matcher = gitignore.NewMatcher(rules)
	}
	g.customMu.Lock()
	g.customMatcher = matcher
	g.customMu.Unlock()

	g.mu.Lock()
	g.walkList = nil
	g.mu.Unlock()
}

func (g *liveGrep) customIgnoreMatcher() gitignore.Matcher {
	g.customMu.Lock()
	defer g.customMu.Unlock()
	return g.customMatcher
}

func (g *liveGrep) matchesCustom(rel string) bool {
	return matchIgnoreMatcher(g.customIgnoreMatcher(), rel)
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
	cq, err := search.CompileQuery(q)
	if err != nil {
		return nil, err
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
		if matcher := g.customIgnoreMatcher(); matcher != nil {
			filtered := make([]string, 0, len(files))
			for _, f := range files {
				if !matchIgnoreMatcher(matcher, f) {
					filtered = append(filtered, f)
				}
			}
			files = filtered
		}
		hits = searchFiles(req.Root, files, cq, req.Limit, hits, g.skipFrom)
	}
	if len(hits) < req.Limit && req.Sources.Buffers {
		hits = searchBuffers(req.Buffers, cq, req.Limit, hits)
	}
	if len(hits) < req.Limit && req.Sources.Terminals {
		hits = searchTerminals(req.Terminals, cq, req.Limit, hits)
	}
	return hits, nil
}

// searchFiles appends the hits of cq in the files of root to hits, in file order and one
// per matching line, up to limit hits.
func searchFiles(root string, files []string, cq *search.CompiledQuery, limit int, hits []palette.GrepHit, skipFrom int64) []palette.GrepHit {
	n := min(len(files), pluginhost.GrepMaxFiles)
	if n <= 0 || len(hits) >= limit {
		return hits
	}
	workers := min(max(goruntime.NumCPU(), 1), 8)
	if n <= 4 || limit-len(hits) <= 4 || workers <= 1 {
		for i := range n {
			if len(hits) >= limit {
				break
			}
			rel := files[i]
			text, ok := pluginhost.ReadSearchable(filepath.Join(root, filepath.FromSlash(rel)), skipFrom)
			if !ok {
				continue
			}
			hits = appendLineHitsText(text, cq, limit, hits, func(h palette.GrepHit) palette.GrepHit {
				h.Rel, h.Source = rel, palette.GrepFiles
				return h
			})
		}
		return hits
	}
	need := limit - len(hits)
	runOrderedPool(n, workers, nil, func(idx int, stop func() bool) []palette.GrepHit {
		rel := files[idx]
		text, ok := pluginhost.ReadSearchable(filepath.Join(root, filepath.FromSlash(rel)), skipFrom)
		if !ok || stop() {
			return nil
		}
		return appendLineHitsText(text, cq, need, nil, func(h palette.GrepHit) palette.GrepHit {
			h.Rel, h.Source = rel, palette.GrepFiles
			return h
		})
	}, func(_ int, fh []palette.GrepHit) bool {
		for _, h := range fh {
			if len(hits) >= limit {
				return false
			}
			hits = append(hits, h)
		}
		return len(hits) < limit
	})
	return hits
}

// lineCursor walks text forward line-by-line without allocating a buffer.Buffer.
type lineCursor struct {
	text    string
	curLine int
	curOff  int
	cachedL int
	cachedS string
}

func newLineCursor(text string) lineCursor {
	return lineCursor{text: text, cachedL: -1}
}

func (c *lineCursor) lineAtRaw(target int) string {
	if target < 0 {
		return ""
	}
	if target == c.cachedL {
		return c.cachedS
	}
	if target < c.curLine {
		c.curLine, c.curOff = 0, 0
	}
	for c.curLine < target && c.curOff < len(c.text) {
		idx := strings.IndexByte(c.text[c.curOff:], '\n')
		if idx < 0 {
			c.curOff = len(c.text)
			c.curLine++
			break
		}
		c.curOff += idx + 1
		c.curLine++
	}
	if c.curLine != target || c.curOff > len(c.text) {
		return ""
	}
	end := len(c.text)
	if idx := strings.IndexByte(c.text[c.curOff:], '\n'); idx >= 0 {
		end = c.curOff + idx
	}
	s := c.text[c.curOff:end]
	c.cachedL, c.cachedS = target, s
	return s
}

func (c *lineCursor) lineAt(target int) string {
	return strings.TrimRight(c.lineAtRaw(target), "\r")
}

// appendLineHitsText appends the hits of cq in text to hits, one per matching line, up to limit
// hits. Each hit comes with its line, column and text; fill sets the source-specific fields.
func appendLineHitsText(text string, cq *search.CompiledQuery, limit int, hits []palette.GrepHit, fill func(palette.GrepHit) palette.GrepHit) []palette.GrepHit {
	matches := cq.FindString(text)
	if len(matches) == 0 {
		return hits
	}
	cur := newLineCursor(text)
	last := -1
	for _, m := range matches {
		if len(hits) >= limit {
			break
		}
		if m.Start.Line == last {
			continue
		}
		last = m.Start.Line
		hits = append(hits, fill(palette.GrepHit{
			Line: m.Start.Line + 1,
			Col:  m.Start.Col + 1,
			Text: cur.lineAt(m.Start.Line),
		}))
	}
	return hits
}

// searchBuffers appends the hits of cq in the open buffers' snapshot to hits, in buffer
// order and one per matching line, up to limit hits. A buffer with no text is skipped.
func searchBuffers(buffers []palette.BufferSource, cq *search.CompiledQuery, limit int, hits []palette.GrepHit) []palette.GrepHit {
	for _, src := range buffers {
		if len(hits) >= limit {
			break
		}
		if src.Text == "" {
			continue
		}
		label := src.Name
		if label == "" {
			label = src.Path
		}
		if label == "" {
			label = "[No Name]"
		}
		hits = appendLineHitsText(src.Text, cq, limit, hits, func(h palette.GrepHit) palette.GrepHit {
			h.Rel, h.Source, h.Index = label, palette.GrepBuffers, src.Index
			return h
		})
	}
	return hits
}

// searchTerminals appends the hits of cq in the bounded dock terminal snapshots to
// hits, in dock-tab order and one per matching line, up to limit hits. The snapshot is
// already bounded by the palette/root caps (≤500 rows/1 MiB per tab, ≤16 tabs/4 MiB
// total), so this is a bounded in-memory search: no PTY read, no process, no IO.
func searchTerminals(terms []palette.TerminalSource, cq *search.CompiledQuery, limit int, hits []palette.GrepHit) []palette.GrepHit {
	for _, src := range terms {
		if len(hits) >= limit {
			break
		}
		if len(src.Lines) == 0 {
			continue
		}
		label := src.Name
		if label == "" {
			label = "terminal"
		}
		hits = appendLineHitsText(strings.Join(src.Lines, "\n"), cq, limit, hits, func(h palette.GrepHit) palette.GrepHit {
			line := h.Line - 1
			h.Rel, h.Source, h.Index = label, palette.GrepTerminals, src.Index
			h.TerminalID, h.TranscriptFirst, h.TerminalTotal = src.ID, src.First, src.Total
			h.TerminalBefore, h.TerminalAfter = terminalContextBefore(src.Lines, line), terminalContextAfter(src.Lines, line)
			h.TerminalSnapshot, h.TerminalValid = true, true
			h.TerminalBeforeKnown, h.TerminalAfterKnown = line > 0, line+1 < len(src.Lines)
			return h
		})
	}
	return hits
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
// root last searched for ignoredWalkTTL, so typing in the palette does not walk the
// tree on every key. A listing error gives the files found so far.
func (g *liveGrep) ignoredFiles(root string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.walkRoot == root && g.walkList != nil && time.Since(g.walkAt) < ignoredWalkTTL {
		return g.walkList
	}
	list := make([]string, 0, 1024)
	_ = finder.EnumerateAll(context.Background(), root, finder.Options{ShowHidden: true, ShowIgnored: true}, func(rel string) bool {
		if g.matchesCustom(rel) {
			return true
		}
		list = append(list, rel)
		return len(list) < finder.MaxFiles
	})
	g.walkRoot, g.walkList, g.walkAt = root, list, time.Now()
	return list
}
