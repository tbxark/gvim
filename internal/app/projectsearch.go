package app

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/search"
)

// projectSearch is the panel's project-wide searcher and replacer over internal/search.
// It reads the project's files, with the open dirty buffers' text in place of their
// files on disk, and plans a replacement as edits so the root can apply it through the
// editor: own offsets are never reused against text that a later read changed.
type projectSearch struct {
	// skipFrom is the size in bytes from which a file is skipped: the files the editor
	// opens in large file mode. Zero skips none beyond the search read cap.
	skipFrom int64
	// previewCache caches one syntax highlighter per previewed file (bounded by
	// previewHighlighterCache).
	previewCache previewHighlighterCache
}

// newProjectSearch returns the project search for an editor with the options o.
func newProjectSearch(o editor.Options) *projectSearch {
	s := &projectSearch{}
	if o.LargeFileMode && o.LargeFileThresholdBytes > 0 {
		s.skipFrom = int64(o.LargeFileThresholdBytes)
	}
	return s
}

// searchBinaryProbe is how many leading bytes are checked for a NUL when a file is
// classified as binary, the same probe live grep uses.
const searchBinaryProbe = 8000

// searchBatchFiles is the most files one batch carries, and searchBatchMatches the most
// matches: a batch is sent when either is reached, plus one final batch.
const (
	searchBatchFiles   = 128
	searchBatchMatches = 1000
)

// ProjectSearchBatches starts the streaming project search over req and returns the
// channel the batches arrive on. It starts one goroutine for the walk; closing cancel asks
// that goroutine to stop, and the channel is closed after a final Done batch (always for a
// completed search) or right after the walk notices the cancellation. Files are reported
// whole, in finder order, at most searchBatchFiles or searchBatchMatches at a time.
func (s *projectSearch) ProjectSearchBatches(req searchpanel.SearchRequest, cancel <-chan struct{}) <-chan searchpanel.SearchBatch {
	out := make(chan searchpanel.SearchBatch)
	go s.searchBatches(req, cancel, out)
	return out
}

// ProjectSearch runs the whole search and returns one result. It drains
// ProjectSearchBatches, so the streaming and the one-shot callers walk the files the same
// way.
func (s *projectSearch) ProjectSearch(req searchpanel.SearchRequest) (searchpanel.SearchResult, error) {
	var out searchpanel.SearchResult
	// The drain never cancels: it wants every batch.
	for batch := range s.ProjectSearchBatches(req, make(chan struct{})) {
		out.Files = append(out.Files, batch.Files...)
		out.Truncated = out.Truncated || batch.Truncated
		out.SkippedCount, out.Skipped = batch.SkippedCount, batch.Skipped
		if batch.Done && batch.Err != "" {
			return out, errors.New(batch.Err)
		}
	}
	return out, nil
}

// searchBatches walks the files of req and sends its batches on out, closing out when it is
// done. It checks cancel between files and before every send, and never sends after the
// close.
func (s *projectSearch) searchBatches(req searchpanel.SearchRequest, cancel <-chan struct{}, out chan<- searchpanel.SearchBatch) {
	defer close(out)
	send := func(batch searchpanel.SearchBatch) bool {
		select {
		case <-cancel:
			return false
		case out <- batch:
			return true
		}
	}
	// next reports that the walk may go on, and does not when the search was cancelled.
	next := func() bool {
		select {
		case <-cancel:
			return false
		default:
			return true
		}
	}
	if req.Query == "" {
		send(searchpanel.SearchBatch{Done: true})
		return
	}
	q := search.Query{
		Text:          req.Query,
		CaseSensitive: req.Options.CaseSensitive,
		WholeWord:     req.Options.WholeWord,
		Regex:         req.Options.Regex,
	}
	if err := validateQuery(q); err != nil {
		send(searchpanel.SearchBatch{Done: true, Err: err.Error()})
		return
	}
	limit := req.Limit
	if limit <= 0 {
		limit = searchpanel.DefaultLimit
	}
	open := openSourcesByPath(req.OpenFiles)

	// The candidates to walk, in result order. CurrentFile is the active buffer, OpenBuffers
	// are the open buffers' snapshots in tab order (one per tab, unnamed buffers included),
	// and AllFiles is the request's file snapshot, capped like live grep.
	var files []string
	var openSources []searchpanel.SourceFile
	truncated := false
	switch req.Scope {
	case searchpanel.CurrentFile:
		files = []string{""}
	case searchpanel.OpenBuffers:
		openSources = dedupeSources(req.OpenFiles)
	default:
		files = req.Files
		if len(files) > pluginhost.GrepMaxFiles {
			files = files[:pluginhost.GrepMaxFiles]
		}
		truncated = len(req.Files) > pluginhost.GrepMaxFiles
	}
	total := len(files)
	if req.Scope == searchpanel.OpenBuffers {
		total = len(openSources)
	}
	if total == 0 {
		send(searchpanel.SearchBatch{Done: true, Truncated: truncated, Total: 0})
		return
	}
	skips := newSkipCounts()
	var pending []searchpanel.FileMatches
	pendingMatches := 0
	totalMatches := 0
	scanned := 0
	flush := func(done bool, errText string) bool {
		skippedCount, skipped := skips.summary()
		batch := searchpanel.SearchBatch{
			Files: pending, Scanned: scanned, Total: total,
			Truncated: truncated, SkippedCount: skippedCount, Skipped: skipped,
			Done: done, Err: errText,
		}
		if !send(batch) {
			return false
		}
		pending, pendingMatches = nil, 0
		return true
	}
	// addFile searches one file's text and queues its matches. It reports whether the match
	// cap stopped the walk, and an error only for a query the search cannot run (already
	// pre-validated).
	addFile := func(path, name string, dirty bool, text string) (bool, error) {
		b := buffer.New(text)
		matches, err := search.Find(b, q)
		if err != nil {
			return false, err
		}
		if len(matches) == 0 {
			return false, nil
		}
		fm := searchpanel.FileMatches{Path: path, Name: name, Hash: hashText(text), Dirty: dirty}
		for _, m := range matches {
			fm.Matches = append(fm.Matches, searchpanel.Match{
				Line:    m.Start.Line + 1,
				Col:     m.Start.Col + 1,
				Len:     matchRunes(b, m),
				Text:    strings.TrimRight(b.Line(m.Start.Line), "\r"),
				EndLine: m.End.Line + 1,
				EndCol:  m.End.Col + 1,
			})
			if totalMatches+len(fm.Matches) >= limit {
				truncated = true
				break
			}
		}
		totalMatches += len(fm.Matches)
		pending = append(pending, fm)
		pendingMatches += len(fm.Matches)
		return totalMatches >= limit, nil
	}
	// walk searches one candidate file (the current buffer included).
	walk := func(rel string) (bool, error) {
		if req.Scope == searchpanel.CurrentFile {
			if req.Current == nil {
				return false, nil
			}
			name := req.Current.Name
			if name == "" {
				name = "[No Name]"
			}
			scanned++
			return addFile(req.Current.Path, name, true, req.Current.Text)
		}
		abs := filepath.Join(req.Root, filepath.FromSlash(rel))
		if src, ok := open[abs]; ok {
			scanned++
			return addFile(abs, rel, src.Dirty, src.Text)
		}
		text, reason := readSearchableFile(abs, s.skipFrom)
		scanned++
		if reason != "" {
			skips.add(reason)
			return false, nil
		}
		return addFile(abs, rel, false, text)
	}

	flushAfter := func() bool {
		if len(pending) >= searchBatchFiles || pendingMatches >= searchBatchMatches {
			return flush(false, "")
		}
		return true
	}
	if req.Scope == searchpanel.OpenBuffers {
		for _, src := range openSources {
			if !next() {
				return
			}
			name := src.Name
			if name == "" {
				name = "[No Name]"
			}
			scanned++
			reached, err := addFile(src.Path, name, true, src.Text)
			if err != nil {
				flush(true, err.Error())
				return
			}
			if reached || !flushAfter() {
				break
			}
		}
	} else {
		for _, rel := range files {
			if !next() {
				return
			}
			reached, err := walk(rel)
			if err != nil {
				flush(true, err.Error())
				return
			}
			if reached || !flushAfter() {
				break
			}
		}
	}
	flush(true, "")
}

// dedupeSources returns the open buffers in tab order, one per tab. An unnamed buffer is
// part of the OpenBuffers scope too.
func dedupeSources(files []searchpanel.SourceFile) []searchpanel.SourceFile {
	seen := make(map[string]bool, len(files))
	out := make([]searchpanel.SourceFile, 0, len(files))
	for _, f := range files {
		key := f.Path + "\x00" + f.Name
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

// ProjectReplace plans the replacement of req.Query with req.Replacement in each file.
// A file whose text no longer hashes to the version the search recorded is reported as
// stale and left alone; a dirty file is planned from the text the request carries. It
// returns edits, already expanded, so the root can apply them without importing search.
func (s *projectSearch) ProjectReplace(req searchpanel.ReplaceRequest) (searchpanel.ReplaceResult, error) {
	var out searchpanel.ReplaceResult
	if req.Query == "" {
		return out, nil
	}
	q := search.Query{
		Text:          req.Query,
		CaseSensitive: req.Options.CaseSensitive,
		WholeWord:     req.Options.WholeWord,
		Regex:         req.Options.Regex,
	}
	if err := validateQuery(q); err != nil {
		return out, err
	}
	limit := req.Limit
	if limit <= 0 {
		limit = searchpanel.DefaultLimit
	}
	edits := 0
	for _, f := range req.Files {
		if edits >= limit {
			break
		}
		fe := searchpanel.FileEdits{Path: f.Path, Name: f.Name, Dirty: f.Dirty, Hash: f.Hash}
		var text string
		if f.Dirty {
			text = f.Text
		} else {
			var reason string
			text, reason = readSearchableFile(f.Path, s.skipFrom)
			if reason != "" {
				fe.Err = reason
				out.Files = append(out.Files, fe)
				continue
			}
		}
		if f.Hash != 0 && hashText(text) != f.Hash {
			fe.Stale = true
			out.Files = append(out.Files, fe)
			continue
		}
		plan, err := search.PlanReplace(buffer.New(text), q, req.Replacement)
		if err != nil {
			fe.Err = err.Error()
			out.Files = append(out.Files, fe)
			continue
		}
		// The panel checks the matches it wants; a plan entry whose start is not in the
		// checked set is left alone. No selection means the whole file.
		selected := selectedSet(f.Selected)
		for _, e := range plan {
			if edits >= limit {
				break
			}
			if len(selected) > 0 {
				if _, ok := selected[[2]int{e.From.Line, e.From.Col}]; !ok {
					continue
				}
			}
			fe.Edits = append(fe.Edits, searchpanel.ReplaceEdit{
				From: searchpanel.Position{Line: e.From.Line, Col: e.From.Col},
				To:   searchpanel.Position{Line: e.To.Line, Col: e.To.Col},
				Text: e.Text,
			})
			edits++
		}
		if len(fe.Edits) > 0 || fe.Err != "" || fe.Stale {
			out.Files = append(out.Files, fe)
		}
	}
	return out, nil
}

// Preview returns a window of a file's lines around one match, for the search panel's
// preview column: the request's Text when the panel already holds an open buffer, else
// a bounded read of the file with the same large/binary/non-UTF-8 caps as a search. The
// window is centred on the match and shifted to reach the match's end when it spans
// lines.
func (s *projectSearch) Preview(req searchpanel.PreviewRequest) (searchpanel.PreviewResult, error) {
	var out searchpanel.PreviewResult
	if req.Lines <= 0 {
		return out, nil
	}
	text := req.Text
	if text == "" {
		var reason string
		text, reason = readSearchableFile(req.Path, s.skipFrom)
		if reason != "" {
			out.Err = reason
			return out, nil
		}
	}
	lines := strings.Split(text, "\n")
	if n := len(lines); n > 1 && lines[n-1] == "" {
		// A trailing line break does not add a line to preview.
		lines = lines[:n-1]
	}
	total := len(lines)
	out.Total = total
	start := clampInt(req.Line-1, 0, max(total-1, 0))
	end := req.EndLine - 1
	if end < start {
		end = start
	}
	first := 0
	if req.Start >= 0 {
		// An explicit window, from the panel scrolling through the file.
		first = clampInt(req.Start, 0, max(total-req.Lines, 0))
	} else {
		// Centre the window on the match; a match longer than the window starts at its
		// first line, so the panel pages through it from the start.
		span := end - start + 1
		first = start
		if span < req.Lines {
			first = start - (req.Lines-span)/2
		}
		first = clampInt(first, 0, max(total-req.Lines, 0))
	}
	out.Start = first
	for k := first; k < total && k < first+req.Lines; k++ {
		out.Lines = append(out.Lines, strings.TrimRight(lines[k], "\r"))
	}
	out.Spans = s.previewCache.spans(req.Path, text, lines, first, len(out.Lines))
	return out, nil
}

// clampInt clamps v into [lo, hi].
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// validateQuery reports a query the search cannot run at all. An invalid regex is the
// one case, and it is detected before any file is read so the panel can show it even
// when the project has no files.
func validateQuery(q search.Query) error {
	if !q.Regex {
		return nil
	}
	_, err := regexp.Compile(q.Text)
	return err
}

// openSourcesByPath maps the open buffers' absolute paths to their snapshots.
func openSourcesByPath(files []searchpanel.SourceFile) map[string]searchpanel.SourceFile {
	out := make(map[string]searchpanel.SourceFile, len(files))
	for _, f := range files {
		if f.Path != "" {
			out[f.Path] = f
		}
	}
	return out
}

// readSearchableFile reads path for a search, like live grep reads a file: at most
// GrepMaxFileBytes, skipping a file larger than that, one in large file mode, one that
// looks binary, and one that is not UTF-8. The second return is the skip reason, empty
// when the text is returned.
func readSearchableFile(path string, skipFrom int64) (string, string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "unreadable file"
	}
	defer func() { _ = f.Close() }()
	if info, err := f.Stat(); err == nil {
		switch {
		case info.IsDir():
			return "", "unreadable file"
		case info.Size() >= pluginhost.GrepMaxFileBytes:
			return "", "large file"
		case skipFrom > 0 && info.Size() >= skipFrom:
			return "", "large file"
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, pluginhost.GrepMaxFileBytes))
	if err != nil {
		return "", "unreadable file"
	}
	probe := data[:min(len(data), searchBinaryProbe)]
	if bytes.IndexByte(probe, 0) >= 0 {
		return "", "binary file"
	}
	if !utf8.Valid(data) {
		return "", "non-UTF-8 file"
	}
	return string(data), ""
}

// selectedSet is the set of 0-based match starts a replacement is limited to; an empty
// selection means no limiting.
func selectedSet(positions []searchpanel.Position) map[[2]int]struct{} {
	if len(positions) == 0 {
		return nil
	}
	out := make(map[[2]int]struct{}, len(positions))
	for _, p := range positions {
		out[[2]int{p.Line, p.Col}] = struct{}{}
	}
	return out
}

// hashText is the version token a search records and a replacement verifies: an FNV-64a
// hash of the exact text searched.
func hashText(text string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(text))
	return h.Sum64()
}

// matchRunes is the match's length in runes on its first line: the whole rest of the
// line for a multi-line match, which a result row shows from its first line.
func matchRunes(b *buffer.Buffer, m search.Match) int {
	line := b.Line(m.Start.Line)
	if m.End.Line == m.Start.Line {
		return max(m.End.Col-m.Start.Col, 1)
	}
	return max(utf8.RuneCountInString(line)-m.Start.Col, 1)
}

// skipCounts counts the files left out of a search, by reason.
type skipCounts struct {
	counts map[string]int
	order  []string
}

func newSkipCounts() *skipCounts { return &skipCounts{counts: map[string]int{}} }

func (s *skipCounts) add(reason string) {
	if _, ok := s.counts[reason]; !ok {
		s.order = append(s.order, reason)
	}
	s.counts[reason]++
}

// summary returns the total and one summary line per reason, in name order: "12 large
// files", "3 binary files".
func (s *skipCounts) summary() (int, []string) {
	total := 0
	sort.Strings(s.order)
	lines := make([]string, 0, len(s.order))
	for _, reason := range s.order {
		n := s.counts[reason]
		total += n
		lines = append(lines, fmt.Sprintf("%d %s%s", n, reason, pluralSuffix(n)))
	}
	return total, lines
}

// pluralSuffix is the "s" of a count of several files.
func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
