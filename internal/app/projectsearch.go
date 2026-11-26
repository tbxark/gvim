package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/search"
)

// projectSearch is the panel's project-wide searcher and replacer over the search package.
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

	mu            sync.Mutex
	customMatcher *explorer.IgnoreMatcher
}

// newProjectSearch returns the project search for an editor with the options o.
func newProjectSearch(o editor.Options, customPatterns ...[]string) *projectSearch {
	s := &projectSearch{}
	if o.LargeFileMode && o.LargeFileThresholdBytes > 0 {
		s.skipFrom = int64(o.LargeFileThresholdBytes)
	}
	if len(customPatterns) > 0 {
		s.SetCustomIgnorePatterns(customPatterns[0])
	}
	return s
}

// SetCustomIgnorePatterns configures custom ignore rules (e.g. *.log, build/).
func (s *projectSearch) SetCustomIgnorePatterns(pats []string) {
	var matcher *explorer.IgnoreMatcher
	if rules := explorer.ParsePatterns(pats); len(rules) > 0 {
		matcher = explorer.NewIgnoreMatcher(rules)
	}
	s.mu.Lock()
	s.customMatcher = matcher
	s.mu.Unlock()
}

func (s *projectSearch) customIgnoreMatcher() *explorer.IgnoreMatcher {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.customMatcher
}

func matchIgnoreMatcher(m *explorer.IgnoreMatcher, rel string) bool {
	if m == nil {
		return false
	}
	return m.Match(strings.Split(filepath.ToSlash(rel), "/"), false)
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
	cq, err := search.CompileQuery(q)
	if err != nil {
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
	matchText := func(path, name string, dirty bool, text string) (searchpanel.FileMatches, bool) {
		matches := cq.FindStringLimit(text, limit)
		if len(matches) == 0 {
			return searchpanel.FileMatches{}, false
		}
		cur := newLineCursor(text)
		fm := searchpanel.FileMatches{Path: path, Name: name, Hash: hashText(text), Dirty: dirty}
		for _, m := range matches {
			rawLine := cur.lineAtRaw(m.Start.Line)
			fm.Matches = append(fm.Matches, searchpanel.Match{
				Line:    m.Start.Line + 1,
				Col:     m.Start.Col + 1,
				Len:     matchRunesText(rawLine, m),
				Text:    strings.TrimRight(rawLine, "\r"),
				EndLine: m.End.Line + 1,
				EndCol:  m.End.Col + 1,
			})
		}
		return fm, true
	}
	appendFileMatches := func(fm searchpanel.FileMatches) bool {
		if len(fm.Matches) == 0 {
			return false
		}
		avail := limit - totalMatches
		if avail <= 0 {
			truncated = true
			return true
		}
		if len(fm.Matches) >= avail {
			if len(fm.Matches) > avail || totalMatches+len(fm.Matches) >= limit {
				fm.Matches = fm.Matches[:avail]
				truncated = true
			}
		}
		totalMatches += len(fm.Matches)
		pending = append(pending, fm)
		pendingMatches += len(fm.Matches)
		return totalMatches >= limit
	}
	// addFile searches one file's text and queues its matches. It reports whether the match
	// cap stopped the walk.
	addFile := func(path, name string, dirty bool, text string) bool {
		fm, ok := matchText(path, name, dirty, text)
		if !ok {
			return false
		}
		return appendFileMatches(fm)
	}
	matcher := s.customIgnoreMatcher()
	// walk searches one candidate file (the current buffer included).
	walk := func(rel string) bool {
		if req.Scope == searchpanel.CurrentFile {
			if req.Current == nil {
				return false
			}
			name := req.Current.Name
			if name == "" {
				name = "[No Name]"
			}
			scanned++
			return addFile(req.Current.Path, name, true, req.Current.Text)
		}
		if matchIgnoreMatcher(matcher, rel) {
			return false
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
			return false
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
			if addFile(src.Path, name, true, src.Text) || !flushAfter() {
				break
			}
		}
	} else if workers := min(max(goruntime.NumCPU(), 1), 8); req.Scope == searchpanel.CurrentFile || len(files) <= 4 || limit <= 4 || workers <= 1 {
		for _, rel := range files {
			if !next() {
				return
			}
			if walk(rel) || !flushAfter() {
				break
			}
		}
	} else {
		type searchSlot struct {
			scanned bool
			reason  string
			fm      searchpanel.FileMatches
		}
		aborted := false
		runOrderedPool(len(files), workers, func() bool { return !next() }, func(idx int, stop func() bool) searchSlot {
			rel := files[idx]
			if matchIgnoreMatcher(matcher, rel) {
				return searchSlot{}
			}
			abs := filepath.Join(req.Root, filepath.FromSlash(rel))
			if src, ok := open[abs]; ok {
				if stop() {
					return searchSlot{}
				}
				fm, _ := matchText(abs, rel, src.Dirty, src.Text)
				return searchSlot{scanned: true, fm: fm}
			}
			text, reason := readSearchableFile(abs, s.skipFrom)
			if reason != "" {
				return searchSlot{scanned: true, reason: reason}
			}
			if stop() {
				return searchSlot{}
			}
			fm, _ := matchText(abs, rel, false, text)
			return searchSlot{scanned: true, fm: fm}
		}, func(_ int, sl searchSlot) bool {
			if !next() {
				aborted = true
				return false
			}
			if sl.scanned {
				scanned++
			}
			if sl.reason != "" {
				skips.add(sl.reason)
				return true
			}
			if appendFileMatches(sl.fm) {
				return false
			}
			if !flushAfter() {
				aborted = true
				return false
			}
			return true
		})
		if aborted || !next() {
			return
		}
	}
	flush(true, "")
}

// runOrderedPool processes n items across a persistent pool of at most workers
// goroutines while delivering results to consume in strict index order 0..n-1.
// In-flight lookahead is bounded to avoid scanning far ahead of an early stop.
func runOrderedPool[T any](
	n, workers int,
	cancelled func() bool,
	process func(idx int, stop func() bool) T,
	consume func(idx int, item T) bool,
) {
	if n <= 0 {
		return
	}
	window := min(n, max(workers*4, 1))
	type poolSlot struct {
		readyIdx int
		val      T
	}
	ring := make([]poolSlot, window)
	for i := range ring {
		ring[i].readyIdx = -1
	}

	var mu sync.Mutex
	workCond := sync.NewCond(&mu)
	slotCond := sync.NewCond(&mu)
	var stopFlag atomic.Bool
	nextJob := 0
	head := 0
	stop := false

	shouldStop := func() bool {
		return stopFlag.Load() || (cancelled != nil && cancelled())
	}

	var wg sync.WaitGroup
	wCount := min(workers, n)
	wg.Add(wCount)
	for range wCount {
		go func() {
			defer wg.Done()
			for {
				mu.Lock()
				for !stop && nextJob < n && nextJob-head >= window {
					workCond.Wait()
				}
				if stop || nextJob >= n {
					mu.Unlock()
					return
				}
				idx := nextJob
				nextJob++
				mu.Unlock()

				if shouldStop() {
					stopFlag.Store(true)
					mu.Lock()
					stop = true
					slotCond.Signal()
					workCond.Broadcast()
					mu.Unlock()
					return
				}

				val := process(idx, shouldStop)

				mu.Lock()
				ring[idx%window] = poolSlot{readyIdx: idx, val: val}
				if idx == head {
					slotCond.Signal()
				}
				mu.Unlock()
			}
		}()
	}

	defer func() {
		stopFlag.Store(true)
		mu.Lock()
		stop = true
		workCond.Broadcast()
		mu.Unlock()
		wg.Wait()
	}()

	for head < n {
		if cancelled != nil && cancelled() {
			return
		}
		mu.Lock()
		for !stop && ring[head%window].readyIdx != head {
			slotCond.Wait()
		}
		if stop {
			mu.Unlock()
			return
		}
		slot := ring[head%window]
		var zero T
		ring[head%window] = poolSlot{readyIdx: -1, val: zero}
		oldHead := head
		head++
		if nextJob-oldHead >= window {
			workCond.Signal()
		}
		mu.Unlock()

		if !consume(oldHead, slot.val) {
			return
		}
	}
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
	matcher := s.customIgnoreMatcher()
	edits := 0
	for _, f := range req.Files {
		if edits >= limit {
			break
		}
		if matchIgnoreMatcher(matcher, f.Name) {
			continue
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
	return s.PreviewContext(context.Background(), req)
}

// PreviewContext is the cancellable local preview seam: the file read itself is a
// bounded os.ReadFile, but a context that ended before (or while) the read returns the
// cancellation instead of continuing into syntax/range work.
func (s *projectSearch) PreviewContext(ctx context.Context, req searchpanel.PreviewRequest) (searchpanel.PreviewResult, error) {
	var out searchpanel.PreviewResult
	if req.Lines <= 0 {
		return out, nil
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if req.Text == "" {
		var reason string
		text, reason := readSearchableFile(req.Path, s.skipFrom)
		if reason != "" {
			out.Err = reason
			return out, nil
		}
		if err := ctx.Err(); err != nil {
			return out, err
		}
		req.Text = text
	}
	return previewStoredText(req, &s.previewCache), nil
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
	f, info, err := openRegularFile(path)
	if err != nil {
		return "", "unreadable file"
	}
	defer func() { _ = f.Close() }()
	switch {
	case info.Size() >= pluginhost.GrepMaxFileBytes:
		return "", "large file"
	case skipFrom > 0 && info.Size() >= skipFrom:
		return "", "large file"
	}
	size := max(info.Size(), 0)
	buf := make([]byte, min(size, int64(pluginhost.GrepMaxFileBytes))+1)
	n := 0
	for {
		if n == len(buf) {
			if len(buf) >= pluginhost.GrepMaxFileBytes {
				return "", "large file"
			}
			nextCap := min(len(buf)*2, pluginhost.GrepMaxFileBytes)
			grown := make([]byte, nextCap)
			copy(grown, buf[:n])
			buf = grown
		}
		nr, err := f.Read(buf[n:])
		n += nr
		if err == io.EOF {
			break
		}
		if err != nil || nr == 0 {
			return "", "unreadable file"
		}
	}
	data := buf[:n]
	if len(data) >= pluginhost.GrepMaxFileBytes {
		return "", "large file"
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
	_, _ = io.WriteString(h, text)
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

func matchRunesText(line string, m search.Match) int {
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
