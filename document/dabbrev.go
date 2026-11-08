package document

import (
	"errors"
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"strings"
	"unicode"
	"unicode/utf8"
)

// dabbrevCycle is the open dabbrev session. Its candidates are the words before
// the typed prefix, nearest first. Index -1 stands for the prefix itself, so a
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

// begin starts a session for the word that ends at the caret.
func (c *dabbrevCycle) Begin(d *Document) error {
	start := dabbrevWordStart(d)
	prefix := d.Buffer.Range(start, d.Cursor)
	if prefix == "" {
		return errors.New("dabbrev: no word before the cursor")
	}
	candidates, limited := dabbrevCandidates(d.Buffer, start, prefix)
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
			if utf8.RuneCountInString(w) <= plen || seen[w] {
				continue
			}
			matched := strings.HasPrefix(w, prefix)
			if !exact {
				matched = strings.HasPrefix(strings.ToLower(w), fold)
			}
			if matched {
				seen[w] = true
				out = append(out, w)
			}
		}
	}
	return out, false
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
