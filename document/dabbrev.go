package document

import (
	"errors"
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Bounds of the multi-buffer dabbrev scan (Fresh's dabbrev provider).
const (
	// DabbrevMaxCandidates is the most candidates one session collects (Fresh's
	// MAX_CANDIDATES, 30).
	DabbrevMaxCandidates = 30
	// DabbrevOtherBufferLimit is the most other open buffers one session scans
	// (Fresh reads every window buffer; gvim bounds the list).
	DabbrevOtherBufferLimit = 8
	// DabbrevOtherBufferBytes is the byte window read from the middle of another
	// open buffer (Fresh's OTHER_BUFFER_SCAN_RADIUS, 64 KiB).
	DabbrevOtherBufferBytes = 64 << 10
	// dabbrevOtherMaxLines bounds the lines of that window even in a file of very
	// short lines.
	dabbrevOtherMaxLines = 4000
)

// dabbrevCycle is the open dabbrev session. Its candidates are the words before
// the typed prefix in the active buffer (nearest first), then words from the
// other open buffers in tab order. Index -1 stands for the prefix itself, so a
// full cycle ends by restoring what the user typed. Any change to the text or
// the caret that the session did not make ends it.
type dabbrevCycle struct {
	Doc        *Document
	Start, End buffer.Pos // the text the session replaces
	Prefix     string
	Candidates []string
	Index      int
}

// shows reports whether the open session still describes the text of d.
func (c *dabbrevCycle) Shows(d *Document) bool {
	if c.Doc != d || d.Cursor != c.End {
		return false
	}
	return d.Buffer.Range(c.Start, c.End) == c.current()
}

// current is the text the session has in the document now.
func (c *dabbrevCycle) current() string {
	if c.Index < 0 {
		return c.Prefix
	}
	return c.Candidates[c.Index]
}

// begin starts a session for the word that ends at the caret. others are the
// other open buffers in tab order; their words follow the active buffer's and are
// read from a bounded middle window per buffer (see DabbrevOtherBufferBytes and
// DabbrevOtherBufferLimit). A large or virtual other buffer is skipped; a large
// ACTIVE buffer keeps its existing bounded backward scan.
func (c *dabbrevCycle) Begin(d *Document, others []*Document) error {
	start := dabbrevWordStart(d)
	prefix := d.Buffer.Range(start, d.Cursor)
	if prefix == "" {
		return errors.New("dabbrev: no word before the cursor")
	}
	candidates, limited := dabbrevCandidates(d.Buffer, start, prefix)
	if len(candidates) < DabbrevMaxCandidates && len(others) > 0 {
		candidates = append(candidates, dabbrevOtherCandidates(others, prefix, candidates, DabbrevMaxCandidates-len(candidates))...)
	}
	if len(candidates) == 0 {
		if limited {
			return fmt.Errorf("dabbrev: %w", ErrScanLimit)
		}
		return fmt.Errorf("dabbrev: no earlier word starts with %q", prefix)
	}
	*c = dabbrevCycle{Doc: d, Start: start, End: d.Cursor, Prefix: prefix, Candidates: candidates, Index: -1}
	return nil
}

// step moves to the next text of the cycle. After the last candidate it puts
// the prefix back and ends the session.
func (c *dabbrevCycle) Step(d *Document) {
	c.Index++
	if c.Index == len(c.Candidates) {
		c.replace(d, c.Prefix)
		c.Doc = nil
		return
	}
	c.replace(d, c.Candidates[c.Index])
}

// replace swaps the session text for text, as one undo step, and puts the caret
// after it.
func (c *dabbrevCycle) replace(d *Document, text string) {
	var after buffer.Pos
	d.Buffer.Group(func() {
		d.Buffer.Delete(c.Start, d.Cursor)
		after = d.Buffer.Insert(c.Start, text)
	})
	d.Cursor = after
	d.Sel = nil
	d.Want = -1
	c.End = after
}

// dabbrevWordStart returns where the word that ends at the caret begins.
func dabbrevWordStart(d *Document) buffer.Pos {
	line := []rune(d.Buffer.Line(d.Cursor.Line))
	col := min(d.Cursor.Col, len(line))
	for col > 0 && dabbrevIsWord(line[col-1]) {
		col--
	}
	return buffer.Pos{Line: d.Cursor.Line, Col: col}
}

// dabbrevIsWord reports whether r is a letter, a digit or an underscore.
func dabbrevIsWord(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

// dabbrevCandidates lists the distinct words that begin before start and start
// with prefix, nearest first. Those are the words left of the prefix on its
// line, then the words of the lines above, each read from right to left. A word
// must be longer than the prefix. Matching ignores case unless the prefix has an
// uppercase letter, which is Fresh's smart case. In a large document the scan reads
// at most the lines and bytes of a scan budget; limited reports that it stopped there
// before it reached the first line, so earlier words may exist.
func dabbrevCandidates(b *buffer.Buffer, start buffer.Pos, prefix string) (out []string, limited bool) {
	exact := strings.ContainsFunc(prefix, unicode.IsUpper)
	fold := strings.ToLower(prefix)
	plen := utf8.RuneCountInString(prefix)
	seen := map[string]bool{}
	budget := newScanBudget(b)
	for line := start.Line; line >= 0; line-- {
		if !budget.Take(b, line) {
			return out, true
		}
		rs := []rune(b.Line(line))
		end := len(rs)
		if line == start.Line {
			end = min(start.Col, len(rs))
		}
		words := dabbrevWords(rs[:end])
		for i := len(words) - 1; i >= 0; i-- {
			w := words[i]
			key := strings.ToLower(w)
			if utf8.RuneCountInString(w) <= plen || seen[key] {
				continue
			}
			matched := strings.HasPrefix(w, prefix)
			if !exact {
				matched = strings.HasPrefix(key, fold)
			}
			if matched {
				seen[key] = true
				out = append(out, w)
				if len(out) >= DabbrevMaxCandidates {
					return out, false
				}
			}
		}
	}
	return out, false
}

// dabbrevOtherCandidates lists prefix-matching words from other open buffers in
// tab order, skipping large and virtual buffers. Each buffer contributes words from
// a bounded middle line window (DabbrevOtherBufferBytes), earlier lines first; a
// word already seen case-insensitively (in the active buffer or a previous other
// buffer) is not offered again. The caller's remaining limit bounds the result.
func dabbrevOtherCandidates(others []*Document, prefix string, active []string, remaining int) []string {
	exact := strings.ContainsFunc(prefix, unicode.IsUpper)
	fold := strings.ToLower(prefix)
	plen := utf8.RuneCountInString(prefix)
	seen := map[string]bool{}
	for _, w := range active {
		seen[strings.ToLower(w)] = true
	}
	seen[fold] = true
	var out []string
	for _, other := range others {
		if len(out) >= remaining {
			break
		}
		if other == nil || other.Large() || other.Virtual() {
			continue
		}
		for _, line := range dabbrevMiddleWindow(other.Buffer) {
			for _, w := range dabbrevWords([]rune(line)) {
				if utf8.RuneCountInString(w) <= plen {
					continue
				}
				key := strings.ToLower(w)
				if seen[key] {
					continue
				}
				matched := strings.HasPrefix(w, prefix)
				if !exact {
					matched = strings.HasPrefix(key, fold)
				}
				if !matched {
					continue
				}
				seen[key] = true
				out = append(out, w)
				if len(out) >= remaining {
					return out
				}
			}
		}
	}
	return out
}

// dabbrevMiddleWindow reads a bounded line window from the middle of b: up to
// about DabbrevOtherBufferBytes, capped at dabbrevOtherMaxLines lines, without
// materializing the whole buffer. A memory buffer is expected; a large buffer is
// skipped by the caller.
func dabbrevMiddleWindow(b *buffer.Buffer) []string {
	n := b.Lines()
	if n <= 0 {
		return nil
	}
	mid := n / 2
	start, end := mid, mid
	spent := b.LineBytes(mid)
	half := DabbrevOtherBufferBytes / 2
	for start > 0 && spent < half && mid-start < dabbrevOtherMaxLines {
		start--
		spent += b.LineBytes(start)
	}
	for end+1 < n && spent < DabbrevOtherBufferBytes && end-mid < dabbrevOtherMaxLines {
		end++
		spent += b.LineBytes(end)
	}
	if end-start+1 > dabbrevOtherMaxLines*3 {
		end = start + dabbrevOtherMaxLines*3 - 1
	}
	lines := make([]string, 0, end-start+1)
	for line := start; line <= end; line++ {
		lines = append(lines, b.Line(line))
	}
	return lines
}

// dabbrevWords splits rs into its runs of word characters, left to right.
func dabbrevWords(rs []rune) []string {
	var words []string
	begin := -1
	for i, r := range rs {
		switch {
		case dabbrevIsWord(r) && begin < 0:
			begin = i
		case !dabbrevIsWord(r) && begin >= 0:
			words = append(words, string(rs[begin:i]))
			begin = -1
		}
	}
	if begin >= 0 {
		words = append(words, string(rs[begin:]))
	}
	return words
}
