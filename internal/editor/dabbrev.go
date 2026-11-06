package editor

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// dabbrevCycle is the open dabbrev session. Its candidates are the words before
// the typed prefix, nearest first. Index -1 stands for the prefix itself, so a
// full cycle ends by restoring what the user typed. Any change to the text or
// the caret that the session did not make ends it.
type dabbrevCycle struct {
	doc        *Document
	start, end buffer.Pos // the text the session replaces
	prefix     string
	candidates []string
	index      int
}

// registerDabbrevHandlers registers dabbrev_expand.
func registerDabbrevHandlers(h map[Action]handler) {
	h["dabbrev_expand"] = dabbrevExpand
}

// dabbrevExpand completes the word before the caret from earlier words of the
// document. A repeated press, with the text untouched, moves to the next
// candidate.
func dabbrevExpand(e *Editor, _ map[string]any) error {
	d := e.Active()
	c := &e.extras.dabbrev
	if !c.shows(d) {
		if err := c.begin(d); err != nil {
			*c = dabbrevCycle{}
			return err
		}
	}
	c.step(d)
	return nil
}

// shows reports whether the open session still describes the text of d.
func (c *dabbrevCycle) shows(d *Document) bool {
	if c.doc != d || d.Cursor != c.end {
		return false
	}
	return d.Buffer.Range(c.start, c.end) == c.current()
}

// current is the text the session has in the document now.
func (c *dabbrevCycle) current() string {
	if c.index < 0 {
		return c.prefix
	}
	return c.candidates[c.index]
}

// begin starts a session for the word that ends at the caret.
func (c *dabbrevCycle) begin(d *Document) error {
	start := dabbrevWordStart(d)
	prefix := d.Buffer.Range(start, d.Cursor)
	if prefix == "" {
		return errors.New("dabbrev: no word before the cursor")
	}
	candidates := dabbrevCandidates(d.Buffer, start, prefix)
	if len(candidates) == 0 {
		return fmt.Errorf("dabbrev: no earlier word starts with %q", prefix)
	}
	*c = dabbrevCycle{doc: d, start: start, end: d.Cursor, prefix: prefix, candidates: candidates, index: -1}
	return nil
}

// step moves to the next text of the cycle. After the last candidate it puts
// the prefix back and ends the session.
func (c *dabbrevCycle) step(d *Document) {
	c.index++
	if c.index == len(c.candidates) {
		c.replace(d, c.prefix)
		c.doc = nil
		return
	}
	c.replace(d, c.candidates[c.index])
}

// replace swaps the session text for text, as one undo step, and puts the caret
// after it.
func (c *dabbrevCycle) replace(d *Document, text string) {
	var after buffer.Pos
	d.Buffer.Group(func() {
		d.Buffer.Delete(c.start, d.Cursor)
		after = d.Buffer.Insert(c.start, text)
	})
	d.Cursor = after
	d.Sel = nil
	d.want = -1
	c.end = after
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
// uppercase letter, which is Fresh's smart case.
func dabbrevCandidates(b *buffer.Buffer, start buffer.Pos, prefix string) []string {
	exact := strings.ContainsFunc(prefix, unicode.IsUpper)
	fold := strings.ToLower(prefix)
	plen := utf8.RuneCountInString(prefix)
	seen := map[string]bool{}
	var out []string
	for line := start.Line; line >= 0; line-- {
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
	return out
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
