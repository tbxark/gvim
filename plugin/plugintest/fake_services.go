package plugintest

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/tbxark/gvim/pluginapi"
)

// FakeServices is a deterministic pluginapi.Services: display width counts wide East Asian
// runes as 2 and combining marks as 0, diff is an LCS line diff, fuzzy is subsequence
// scoring, grep returns scripted hits, decode knows utf-8 and latin-1, git answers from
// maps.
type FakeServices struct {
	// GrepHits are returned by Grep when their Text contains the query (case-insensitive
	// unless the request is case sensitive), at most Max.
	GrepHits []pluginapi.GrepHit
	// GitRoots maps a directory to its repository root; Grep and the git calls also match
	// the parents of the directory.
	GitRoots map[string]string
	// Branches maps a repository root to its current branch.
	Branches map[string]string
}

var _ pluginapi.Services = (*FakeServices)(nil)

func runeWidth(r rune) int {
	switch {
	case unicode.Is(unicode.Mn, r), unicode.Is(unicode.Me, r), r == 0x200d:
		return 0
	case r >= 0x1100 && r <= 0x115f,
		r >= 0x2e80 && r <= 0xa4cf,
		r >= 0xac00 && r <= 0xd7a3,
		r >= 0xf900 && r <= 0xfaff,
		r >= 0xfe30 && r <= 0xfe6f,
		r >= 0xff00 && r <= 0xff60,
		r >= 0xffe0 && r <= 0xffe6,
		r >= 0x1f300 && r <= 0x1faff:
		return 2
	}
	return 1
}

// Width is the display width in cells.
func (*FakeServices) Width(s string) int {
	n := 0
	for _, r := range s {
		n += runeWidth(r)
	}
	return n
}

// Truncate cuts s to w cells, ending in ellipsis when it was cut.
func (f *FakeServices) Truncate(s string, w int, ellipsis string) string {
	if f.Width(s) <= w {
		return s
	}
	room := w - f.Width(ellipsis)
	if room < 0 {
		return ""
	}
	var sb strings.Builder
	used := 0
	for _, r := range s {
		rw := runeWidth(r)
		if used+rw > room {
			break
		}
		sb.WriteRune(r)
		used += rw
	}
	return sb.String() + ellipsis
}

// Diff is an LCS line diff with the hunk conventions of diff.
func (*FakeServices) Diff(a, b []string) []pluginapi.Hunk {
	n, m := len(a), len(b)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var hunks []pluginapi.Hunk
	i, j := 0, 0
	for i < n || j < m {
		if i < n && j < m && a[i] == b[j] {
			i++
			j++
			continue
		}
		oi, oj := i, j
		for (i < n || j < m) && (i >= n || j >= m || a[i] != b[j]) {
			switch {
			case j >= m || (i < n && lcs[i+1][j] >= lcs[i][j+1]):
				i++
			default:
				j++
			}
		}
		h := pluginapi.Hunk{OldStart: oi, OldCount: i - oi, NewStart: oj, NewCount: j - oj}
		switch {
		case h.OldCount == 0:
			h.Kind = "insert"
		case h.NewCount == 0:
			h.Kind = "delete"
		default:
			h.Kind = "change"
		}
		hunks = append(hunks, h)
	}
	return hunks
}

// DiffText diffs two texts line by line.
func (f *FakeServices) DiffText(a, b string, o pluginapi.DiffOptions) []pluginapi.Hunk {
	if o.IgnoreEOL {
		norm := strings.NewReplacer("\r\n", "\n", "\r", "\n")
		a, b = norm.Replace(a), norm.Replace(b)
	}
	return f.Diff(strings.Split(a, "\n"), strings.Split(b, "\n"))
}

// Fuzzy matches query as a case-insensitive subsequence; consecutive and early matches
// score higher. Results are ordered by score, then index.
func (*FakeServices) Fuzzy(ctx context.Context, query string, items []string, limit int) []pluginapi.FuzzyMatch {
	q := []rune(strings.ToLower(query))
	var out []pluginapi.FuzzyMatch
	for idx, it := range items {
		if ctx.Err() != nil {
			return nil
		}
		rs := []rune(strings.ToLower(it))
		var pos []int
		score, qi, last := 0, 0, -2
		for i, r := range rs {
			if qi < len(q) && r == q[qi] {
				pos = append(pos, i)
				score += 10 - min(i, 9)
				if i == last+1 {
					score += 5
				}
				last = i
				qi++
			}
		}
		if qi == len(q) {
			out = append(out, pluginapi.FuzzyMatch{Index: idx, Score: score, Indexes: pos})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Grep returns the scripted hits that contain the query.
func (f *FakeServices) Grep(ctx context.Context, r pluginapi.GrepRequest) ([]pluginapi.GrepHit, error) {
	var out []pluginapi.GrepHit
	for _, h := range f.GrepHits {
		if err := ctx.Err(); err != nil {
			return nil, pluginapi.Errf(pluginapi.CodeCancelled, "grep cancelled")
		}
		a, b := h.Text, r.Query
		if !r.CaseSensitive {
			a, b = strings.ToLower(a), strings.ToLower(b)
		}
		if strings.Contains(a, b) {
			out = append(out, h)
			if r.Max > 0 && len(out) >= r.Max {
				break
			}
		}
	}
	return out, nil
}

// Decode converts utf-8 and latin-1 data to a string.
func (*FakeServices) Decode(encoding string, data []byte) (string, error) {
	switch strings.ToLower(encoding) {
	case "", "utf-8", "utf8":
		return string(data), nil
	case "latin1", "latin-1", "iso-8859-1":
		rs := make([]rune, len(data))
		for i, b := range data {
			rs[i] = rune(b)
		}
		return string(rs), nil
	}
	return "", pluginapi.Errf(pluginapi.CodeUnsupported, "unknown encoding %q", encoding)
}

// GitRoot finds dir (or a parent) in GitRoots.
func (f *FakeServices) GitRoot(dir string) (string, bool) {
	for {
		if root, ok := f.GitRoots[dir]; ok {
			return root, true
		}
		i := strings.LastIndexByte(dir, '/')
		if i <= 0 {
			return "", false
		}
		dir = dir[:i]
	}
}

// GitBranch answers from Branches.
func (f *FakeServices) GitBranch(root string) (string, error) {
	if b, ok := f.Branches[root]; ok {
		return b, nil
	}
	return "", pluginapi.Errf(pluginapi.CodeNotFound, "not a repository: %s", root)
}

var (
	chordMods  = map[string]bool{"ctrl": true, "alt": true, "shift": true, "super": true}
	chordNames = map[string]bool{"backspace": true, "backtab": true, "delete": true, "down": true, "end": true,
		"enter": true, "escape": true, "home": true, "insert": true, "left": true, "pagedown": true,
		"pageup": true, "right": true, "tab": true, "up": true, "space": true}
)

// ValidateChord accepts the chords the real keymap accepts: up to four space-separated
// steps, each ctrl/alt/shift/super modifiers joined by "+" and one key (a single character,
// F1 to F12 or a named key such as enter or pagedown).
func (*FakeServices) ValidateChord(chord string) error {
	steps := strings.Fields(chord)
	if len(steps) == 0 || len(steps) > 4 {
		return pluginapi.Errf(pluginapi.CodeInvalid, "keymap: %q is not 1 to 4 keys", chord)
	}
	for _, step := range steps {
		key, mods := step, ""
		if i := strings.LastIndexByte(step, '+'); i > 0 {
			key, mods = step[i+1:], step[:i]
			if key == "" { // "ctrl++": the plus key
				key, mods = "+", strings.TrimSuffix(mods, "+")
			}
		}
		if mods != "" {
			for _, m := range strings.Split(mods, "+") {
				if !chordMods[strings.ToLower(m)] {
					return pluginapi.Errf(pluginapi.CodeInvalid, "keymap: %q: unknown key name %q", step, m)
				}
			}
		}
		lk := strings.ToLower(key)
		n, numErr := strconv.Atoi(strings.TrimPrefix(lk, "f"))
		isFn := strings.HasPrefix(lk, "f") && numErr == nil && n >= 1 && n <= 12
		if !isFn && !chordNames[lk] && len([]rune(key)) != 1 {
			return pluginapi.Errf(pluginapi.CodeInvalid, "keymap: %q: unknown key name %q", step, key)
		}
	}
	return nil
}
