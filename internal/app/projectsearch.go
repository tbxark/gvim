package app

import (
	"bytes"
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

// ProjectSearch searches the project's files for req.Query. A file is read once, at
// most GrepMaxFileBytes, and skipped with a reason when it is too large, binary, not
// UTF-8 or unreadable. The result carries at most req.Limit matches; Files is the file
// index snapshot and OpenFiles the unsaved text that replaces their files. It returns an
// error only for a query that cannot be run at all, such as an invalid regex; a file
// that cannot be read is a skip, not a failure.
func (s *projectSearch) ProjectSearch(req searchpanel.SearchRequest) (searchpanel.SearchResult, error) {
	var out searchpanel.SearchResult
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
	total := 0
	capped := false
	skips := newSkipCounts()
	open := openSourcesByPath(req.OpenFiles)

	// add searches one file's text and appends its matches. It reports whether the
	// match cap was reached.
	add := func(path, name string, dirty bool, text string) (bool, error) {
		if total >= limit {
			return true, nil
		}
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
			if total+len(fm.Matches) >= limit {
				capped = true
				break
			}
		}
		total += len(fm.Matches)
		out.Files = append(out.Files, fm)
		return capped, nil
	}

	switch req.Scope {
	case searchpanel.CurrentFile:
		if req.Current == nil {
			return out, nil
		}
		name := req.Current.Name
		if name == "" {
			name = "[No Name]"
		}
		if _, err := add(req.Current.Path, name, true, req.Current.Text); err != nil {
			return out, err
		}
	default:
		files := req.Files
		if len(files) > pluginhost.GrepMaxFiles {
			files = files[:pluginhost.GrepMaxFiles]
			capped = true
		}
		for _, rel := range files {
			abs := filepath.Join(req.Root, filepath.FromSlash(rel))
			if src, ok := open[abs]; ok {
				// An open buffer is searched as it is, so unsaved edits are seen.
				reached, err := add(abs, rel, src.Dirty, src.Text)
				if err != nil {
					return out, err
				}
				if reached {
					break
				}
				continue
			}
			text, reason := readSearchableFile(abs, s.skipFrom)
			if reason != "" {
				skips.add(reason)
				continue
			}
			reached, err := add(abs, rel, false, text)
			if err != nil {
				return out, err
			}
			if reached {
				break
			}
		}
	}
	out.Truncated = capped
	out.SkippedCount, out.Skipped = skips.summary()
	return out, nil
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
	start := req.Line - 1
	start = clampInt(start, 0, max(len(lines)-1, 0))
	end := req.EndLine - 1
	if end < start {
		end = start
	}
	// Centre the window on the match; a match longer than the window starts at its
	// first line and so truncates (the bounded column cannot show it whole).
	span := end - start + 1
	first := start
	if span < req.Lines {
		first = start - (req.Lines-span)/2
	}
	first = clampInt(first, 0, max(len(lines)-req.Lines, 0))
	out.Start = first
	for k := first; k < len(lines) && k < first+req.Lines; k++ {
		out.Lines = append(out.Lines, strings.TrimRight(lines[k], ""))
	}
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
