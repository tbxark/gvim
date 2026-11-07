package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/diff"
	"github.com/tbxark/gvim/internal/encoding"
	"github.com/tbxark/gvim/internal/finder"
	"github.com/tbxark/gvim/internal/fuzzy"
	"github.com/tbxark/gvim/internal/gitx"
	"github.com/tbxark/gvim/internal/keymap"
	"github.com/tbxark/gvim/internal/pluginapi"
	"github.com/tbxark/gvim/internal/search"
)

// pluginServices is pluginapi.Services over the domain packages: display width and
// truncation (ansi), line diffs (diff), fuzzy ranking (fuzzy), the workspace grep (finder
// and search, within the caps of live_grep.go), decoding (encoding) and git root and
// branch (gitx). The plugin runtime may not import any of them, so the app hands it this.
type pluginServices struct{}

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
		a, b = normalizeEOL(a), normalizeEOL(b)
	}
	return convertHunks(diff.Lines(strings.Split(a, "\n"), strings.Split(b, "\n")))
}

func normalizeEOL(s string) string {
	if !strings.Contains(s, "\r") {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
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
	pat := fuzzy.NewPattern(query)
	var out []pluginapi.FuzzyMatch
	for i, it := range items {
		if ctx.Err() != nil {
			return nil
		}
		if score, idx, ok := pat.Match(it); ok {
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
	// line of up to grepMaxFileBytes, and the result of a Work is bounded by neither the
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
// grepMaxFiles files of the file index of the root, at most grepMaxFileBytes of each, and
// it skips files that look binary. A file larger than grepMaxFileBytes is skipped as a
// whole, since searching only its start would make a miss look like a complete search.
// A file gives one hit for each matching line. The hit's Line is 0-based, and Col the
// 0-based BYTE column of the match inside Text, which is the line as the buffer holds it
// (live grep itself reports a 1-based line and rune column), or a window of about
// grepHitTextMax bytes of it when the line is longer. That bounds the result to
// Max * (grepHitTextMax + path length) bytes.
func (pluginServices) Grep(ctx context.Context, r pluginapi.GrepRequest) ([]pluginapi.GrepHit, error) {
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
		return len(files) < grepMaxFiles
	})
	if cerr := ctx.Err(); cerr != nil {
		return nil, pluginapi.Errf(pluginapi.CodeCancelled, "grep cancelled")
	}
	if err != nil {
		return nil, pluginapi.Errf(pluginapi.CodeIO, "grep: %v", err)
	}
	var hits []pluginapi.GrepHit
	for _, rel := range files {
		if err := ctx.Err(); err != nil {
			return nil, pluginapi.Errf(pluginapi.CodeCancelled, "grep cancelled")
		}
		text, ok := readSearchable(filepath.Join(r.Root, filepath.FromSlash(rel)), grepMaxFileBytes+1)
		if !ok {
			continue
		}
		b := buffer.New(text)
		matches, err := search.Find(b, q)
		if err != nil {
			return hits, pluginapi.Errf(pluginapi.CodeInvalid, "grep: %v", err)
		}
		last := -1
		for _, m := range matches {
			if m.Start.Line == last {
				continue
			}
			last = m.Start.Line
			line := strings.TrimRight(b.Line(m.Start.Line), "\r")
			text, col := hitWindow(line, b.ByteCol(m.Start))
			hits = append(hits, pluginapi.GrepHit{Path: rel, Line: m.Start.Line, Col: col, Text: text})
			if len(hits) >= limit {
				return hits, nil
			}
		}
	}
	return hits, nil
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
func (pluginServices) GitRoot(dir string) (string, bool) { return gitx.FindRoot(dir) }

// GitBranch returns the checked-out branch of the work tree root.
func (pluginServices) GitBranch(root string) (string, error) {
	b, err := gitx.Branch(root)
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
