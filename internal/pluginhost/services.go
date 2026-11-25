package pluginhost

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/diff"
	"github.com/tbxark/gvim/encoding"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/fuzzy"
	"github.com/tbxark/gvim/gitstate"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/search"
)

// pluginServices is pluginapi.Services over the domain packages: display width and
// truncation (ansi), line diffs (diff), fuzzy ranking (fuzzy), the workspace grep (finder
// and search, within the caps of live_grep.go), decoding (encoding) and git root and
// branch (gitstate). The plugin runtime may not import any of them, so the app hands it this.
type pluginServices struct {
	// agentDiscover is nil when the session has no agent-discovery capability (for
	// example a shared daemon runtime, whose scanner is per workspace).
	agentDiscover func(context.Context) (pluginapi.AgentReport, error)
	remote        bool
}

var _ pluginapi.Services = pluginServices{}

// Width is the number of terminal cells s takes.
func (pluginServices) Width(s string) int { return ansi.StringWidth(s) }

// Truncate cuts s to w cells and ends it with ellipsis when it was cut.
func (pluginServices) Truncate(s string, w int, ellipsis string) string {
	return ansi.Truncate(s, w, ellipsis)
}

// Diff is the line diff of a against b, as internal/diff gives it.
func (pluginServices) Diff(a, b []string) []pluginapi.Hunk {
	return convertHunks(diff.Lines(a, b))
}

// DiffText is Diff over two texts. With IgnoreEOL, CRLF and a lone CR are LF on both sides.
func (pluginServices) DiffText(a, b string, o pluginapi.DiffOptions) []pluginapi.Hunk {
	if o.IgnoreEOL {
		a, b = plugin.NormalizeEOL(a), plugin.NormalizeEOL(b)
	}
	return convertHunks(diff.Lines(strings.Split(a, "\n"), strings.Split(b, "\n")))
}

func convertHunks(hs []diff.Hunk) []pluginapi.Hunk {
	out := make([]pluginapi.Hunk, len(hs))
	for i, h := range hs {
		kind := "change"
		switch h.Kind {
		case diff.Insert:
			kind = "insert"
		case diff.Delete:
			kind = "delete"
		}
		out[i] = pluginapi.Hunk{Kind: kind, OldStart: h.OldStart, OldCount: h.OldLines, NewStart: h.NewStart, NewCount: h.NewLines}
	}
	return out
}

// Fuzzy ranks the items that match query, best first, at most limit of them (all when
// limit <= 0). Equal scores keep the order of items. A cancelled ctx stops the ranking and
// returns nothing.
func (pluginServices) Fuzzy(ctx context.Context, query string, items []string, limit int) []pluginapi.FuzzyMatch {
	m := fuzzy.NewMatcher(query)
	var out []pluginapi.FuzzyMatch
	for i, it := range items {
		if ctx.Err() != nil {
			return nil
		}
		if score, idx, ok := m.Match(it); ok {
			out = append(out, pluginapi.FuzzyMatch{Index: i, Score: score, Indexes: idx})
		}
	}
	slices.SortStableFunc(out, func(a, b pluginapi.FuzzyMatch) int { return b.Score - a.Score })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

const (
	// grepDefaultMax is the number of hits Grep returns when the request names no maximum.
	grepDefaultMax = 2000
	// grepHitTextMax is the most bytes of a line that a hit carries. A minified file is one
	// line of up to GrepMaxFileBytes, and the result of a Work is bounded by neither the
	// slice budget nor the heap guard, so a hit holds a window of its line.
	grepHitTextMax = 512
	// grepHitLead is how many bytes of a long line precede the match in the window.
	grepHitLead = grepHitTextMax / 4
)

// hitWindow returns at most about grepHitTextMax bytes of line around the byte column
// col, cut at character boundaries, and col as an offset into that window.
func hitWindow(line string, col int) (string, int) {
	if len(line) <= grepHitTextMax {
		return line, col
	}
	start := max(0, col-grepHitLead)
	end := min(len(line), start+grepHitTextMax)
	for start < col && !utf8.RuneStart(line[start]) {
		start++
	}
	for end < len(line) && end > col && !utf8.RuneStart(line[end]) {
		end--
	}
	return line[start:end], col - start
}

// Grep searches the files under r.Root for r.Query. It reads what live grep reads: at most
// GrepMaxFiles files of the file index of the root, at most GrepMaxFileBytes of each, and
// it skips files that look binary. A file larger than GrepMaxFileBytes is skipped as a
// whole, since searching only its start would make a miss look like a complete search.
// A file gives one hit for each matching line. The hit's Line is 0-based, and Col the
// 0-based BYTE column of the match inside Text, which is the line as the buffer holds it
// (live grep itself reports a 1-based line and rune column), or a window of about
// grepHitTextMax bytes of it when the line is longer. That bounds the result to
// Max * (grepHitTextMax + path length) bytes.
func (s pluginServices) Grep(ctx context.Context, r pluginapi.GrepRequest) ([]pluginapi.GrepHit, error) {
	if s.remote {
		return nil, pluginapi.Errf(pluginapi.CodeUnsupported, "grep is not available on a remote workspace")
	}
	if r.Query == "" {
		return nil, nil
	}
	limit := r.Max
	if limit <= 0 {
		limit = grepDefaultMax
	}
	q := search.Query{Text: r.Query, Regex: r.Regex, CaseSensitive: r.CaseSensitive}
	var files []string
	err := finder.Enumerate(ctx, r.Root, func(rel string) bool {
		files = append(files, rel)
		return len(files) < GrepMaxFiles
	})
	if cerr := ctx.Err(); cerr != nil {
		return nil, pluginapi.Errf(pluginapi.CodeCancelled, "grep cancelled")
	}
	if err != nil {
		return nil, pluginapi.Errf(pluginapi.CodeIO, "grep: %v", err)
	}
	cq, err := search.CompileQuery(q)
	if err != nil {
		return nil, pluginapi.Errf(pluginapi.CodeInvalid, "grep: %v", err)
	}
	var hits []pluginapi.GrepHit
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return nil, pluginapi.Errf(pluginapi.CodeCancelled, "grep cancelled")
		}
		text, ok := ReadSearchable(filepath.Join(r.Root, filepath.FromSlash(rel)), GrepMaxFileBytes+1)
		if !ok {
			continue
		}
		matches := cq.FindString(text)
		last := -1
		curLine, lineStart := 0, 0
		for _, m := range matches {
			if m.Start.Line == last {
				continue
			}
			last = m.Start.Line
			for curLine < m.Start.Line {
				nl := strings.IndexByte(text[lineStart:], '\n')
				if nl < 0 {
					lineStart = len(text)
					break
				}
				lineStart += nl + 1
				curLine++
			}
			lineEnd := len(text)
			if nl := strings.IndexByte(text[lineStart:], '\n'); nl >= 0 {
				lineEnd = lineStart + nl
			}
			rawLine := text[lineStart:lineEnd]
			byteCol := runeColToByteCol(rawLine, m.Start.Col)
			line := strings.TrimRight(rawLine, "\r")
			hitText, col := hitWindow(line, byteCol)
			hits = append(hits, pluginapi.GrepHit{Path: rel, Line: m.Start.Line, Col: col, Text: hitText})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
}

// runeColToByteCol returns the byte offset of rune index runeCol inside line.
func runeColToByteCol(line string, runeCol int) int {
	if runeCol <= 0 {
		return 0
	}
	count := 0
	for i := range line {
		if count == runeCol {
			return i
		}
		count++
	}
	return len(line)
}

// encodingLabels are the labels the editor reports as the encoding of a buffer (which is
// what b:info().encoding holds) that internal/encoding has a different name for.
var encodingLabels = map[string]string{
	"ascii": "utf-8", "utf8": "utf-8", "utf-8 bom": "utf-8-bom", "utf-16 le": "utf-16le",
	"utf-16 be": "utf-16be", "latin-1": "iso-8859-1", "shift-jis": "shift_jis",
}

// Decode converts data in the named encoding to text. An empty name, "utf8" and the label
// "ASCII" are UTF-8; the other labels of the editor ("UTF-16 LE", "Latin-1") are accepted
// too, so a plugin can pass b:info().encoding.
func (pluginServices) Decode(name string, data []byte) (string, error) {
	n := strings.ToLower(name)
	if n == "" {
		n = "utf-8"
	}
	if codec, ok := encodingLabels[n]; ok {
		n = codec
	}
	name = n
	s, err := encoding.Decode(name, data)
	if err != nil {
		return "", pluginapi.Errf(pluginapi.CodeInvalid, "%v", err)
	}
	return s, nil
}

// GitRoot returns the work tree root that holds dir.
func (s pluginServices) GitRoot(dir string) (string, bool) {
	if s.remote {
		return "", false
	}
	return gitstate.FindRoot(dir)
}

// DiscoverAgents reports the app's bounded, trust-gated local agent session scan. A
// pluginServices without a callback has no discovery capability rather than a fake empty
// list, so the plugin can show its unsupported state.
func (s pluginServices) DiscoverAgents(ctx context.Context) (pluginapi.AgentReport, error) {
	if s.agentDiscover == nil {
		return pluginapi.AgentReport{}, nil
	}
	return s.agentDiscover(ctx)
}

// GitBranch returns the checked-out branch of the work tree root.
func (s pluginServices) GitBranch(root string) (string, error) {
	if s.remote {
		return "", pluginapi.Errf(pluginapi.CodeUnsupported, "git branch is not available on a remote workspace")
	}
	b, err := gitstate.Branch(root)
	if err != nil {
		return "", pluginapi.Errf(pluginapi.CodeNotFound, "%v", err)
	}
	return b, nil
}

// ValidateChord reports why chord is not a key the keymap can bind.
func (pluginServices) ValidateChord(chord string) error {
	if err := keymap.ValidateChord(chord); err != nil {
		return pluginapi.Errf(pluginapi.CodeInvalid, "%v", err)
	}
	return nil
}

// Limits of a files search. A query reads at most GrepMaxFiles files of the index,
// and at most GrepMaxFileBytes of each one. A file that looks binary is skipped.
const (
	GrepMaxFiles     = 2000
	GrepMaxFileBytes = 1 << 20
	grepBinaryProbe  = 8000
)

// ReadSearchable returns the text of the file at path when it is a valid UTF-8 regular
// file of at most GrepMaxFileBytes (and smaller than skipFrom when skipFrom is not zero),
// non-binary (no NUL byte in its first grepBinaryProbe bytes). Files larger than
// GrepMaxFileBytes are skipped as a whole instead of being silently truncated.
func ReadSearchable(path string, skipFrom int64) (string, bool) {
	// O_NONBLOCK: opening a FIFO must not wait for a writer.
	f, err := os.OpenFile(path, os.O_RDONLY|openNoBlock, 0)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > GrepMaxFileBytes || (skipFrom > 0 && info.Size() >= skipFrom) {
		return "", false
	}
	size := max(info.Size(), 0)
	data := make([]byte, 0, min(int(size)+1, GrepMaxFileBytes+1))
	for len(data) <= GrepMaxFileBytes {
		if len(data) == cap(data) {
			nextCap := min(max(cap(data)*2, 512), GrepMaxFileBytes+1)
			grown := make([]byte, len(data), nextCap)
			copy(grown, data)
			data = grown
		}
		n, rerr := f.Read(data[len(data):cap(data)])
		data = data[:len(data)+n]
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", false
		}
		if n == 0 {
			break
		}
	}
	if len(data) > GrepMaxFileBytes || (skipFrom > 0 && int64(len(data)) >= skipFrom) {
		return "", false
	}
	if bytes.IndexByte(data[:min(len(data), grepBinaryProbe)], 0) >= 0 || !utf8.Valid(data) {
		return "", false
	}
	return string(data), true
}
