package document

import (
	"errors"
	"github.com/tbxark/gvim/buffer"
	"slices"
	"strings"
	"unicode/utf8"
)

// errNoWordAtCaret is returned by add_cursor_next_match when the caret is not in
// a word and is not just after one, so there is nothing to select.
var errNoWordAtCaret = errors.New("no word at cursor position")

// errAllMatchesSelected is returned by add_cursor_next_match when every
// occurrence of the selected text already has a caret.
var errAllMatchesSelected = errors.New("all matches are already selected")

// addNextMatch implements add_cursor_next_match as Fresh does. With no selection
// it selects the word at the caret and adds no caret. With a selection it looks
// for the selected text after the selection, wrapping to the start of the
// document. An occurrence that would meet a caret already there is skipped. The
// first one that does not meet a caret gets a new caret, which becomes the
// primary caret. The old primary becomes an extra caret. A backward selection
// makes the new caret backward too. A large document selects the word only: the
// search for the next occurrence needs its whole text, and returns ErrLargeFile.
func (d *Document) AddNextMatch() error {
	if !d.HasSelection() {
		from, to, ok := d.caseTarget()
		if !ok {
			return errNoWordAtCaret
		}
		cs := d.AllCarets()
		for i := range cs {
			if cs[i].Primary {
				cs[i].Anchor, cs[i].Head = from, to
			}
		}
		d.SetCarets(cs)
		return nil
	}

	if d.large != nil {
		// The next occurrence is found in the whole text, which a large document
		// does not hold.
		return errLargeAction("add_cursor_next_match")
	}
	lo, hi := d.Sel.Ordered()
	backward := d.Sel.Head.Less(d.Sel.Anchor)
	text := d.Buffer.Text()
	from := len(d.Buffer.Range(buffer.Pos{}, lo))
	to := len(d.Buffer.Range(buffer.Pos{}, hi))
	pattern := text[from:to]

	cs := d.AllCarets()
	seen := make(map[int]bool)
	search := to
	for {
		// pattern occurs at from, so the search always finds an occurrence.
		off := nextOccurrence(text, pattern, search)
		if seen[off] {
			return errAllMatchesSelected
		}
		seen[off] = true
		m := caret{
			Anchor: posAtOffset(text, off),
			Head:   posAtOffset(text, off+len(pattern)),
		}
		if !slices.ContainsFunc(cs, m.meets) {
			if backward {
				m.Anchor, m.Head = m.Head, m.Anchor
			}
			m.Primary = true
			for i := range cs {
				cs[i].Primary = false
			}
			d.SetCarets(append(cs, m))
			return nil
		}
		search = off + len(pattern)
	}
}

// nextOccurrence returns the byte offset of the first occurrence of pattern in
// text at or after from, wrapping to the start of text when there is none. The
// pattern must occur in text.
func nextOccurrence(text, pattern string, from int) int {
	if i := strings.Index(text[from:], pattern); i >= 0 {
		return from + i
	}
	return strings.Index(text, pattern)
}

// posAtOffset returns the position of the byte offset off in text, where text
// is the buffer's text joined with '\n'.
func posAtOffset(text string, off int) buffer.Pos {
	before := text[:off]
	start := strings.LastIndexByte(before, '\n') + 1
	return buffer.Pos{
		Line: strings.Count(before, "\n"),
		Col:  utf8.RuneCountInString(before[start:]),
	}
}
