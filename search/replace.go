package search

import (
	"errors"
	"strings"

	"github.com/tbxark/gvim/buffer"
)

// ErrNoMatch is returned by Replace when m is not a match of q in the buffer
// as it is now, for example because the text changed after Find.
var ErrNoMatch = errors.New("search: no match of the query at the given position")

// ErrMatchTooLarge is returned by ReplaceAt for a match whose lines are more than
// 4 MiB long together. ReplaceAt reads those lines whole to check the match, so a
// match over a huge part of a large file is refused.
var ErrMatchTooLarge = errors.New("search: match too large to replace")

// maxReplaceBytes is the most text ReplaceAt reads to check a match.
const maxReplaceBytes = 4 << 20

// Decision is the caller's answer to one query-replace prompt.
type Decision int

const (
	// DecisionSkip leaves the match unchanged and moves on (the n key).
	DecisionSkip Decision = iota
	// DecisionReplace replaces this match and moves on (the y key).
	DecisionReplace
	// DecisionReplaceAll replaces this match and every later one without
	// asking again (the ! key).
	DecisionReplaceAll
	// DecisionQuit stops. Matches from this one on are left unchanged (the q key).
	DecisionQuit
)

// DecisionFromKey maps a query-replace key to its Decision: 'y' or 'n', '!' or
// 'a' for all, and 'q' or 'c' to stop. Upper case is accepted too. These are the
// keys of Fresh's prompt, "Replace? (y)es (n)o (a)ll (c)ancel". It reports false
// for any other key, so the caller can ask again.
func DecisionFromKey(key rune) (Decision, bool) {
	switch key {
	case 'y', 'Y':
		return DecisionReplace, true
	case 'n', 'N':
		return DecisionSkip, true
	case '!', 'a', 'A':
		return DecisionReplaceAll, true
	case 'q', 'Q', 'c', 'C':
		return DecisionQuit, true
	}
	return DecisionSkip, false
}

// Replace replaces the match m of q in b with repl and returns the text that
// was inserted. In regex mode repl is expanded with regexp.Expand, so $1 and
// ${name} refer to the captures of m. In literal mode repl is inserted as is.
// The edit is one undo group, so one Undo reverts it and one Redo reapplies
// it. If m is not a current match of q, Replace returns ErrNoMatch and changes
// nothing.
func Replace(b *buffer.Buffer, q Query, repl string, m Match) (string, error) {
	r, err := run(b, q)
	if err != nil {
		return "", err
	}
	for _, h := range r.hits {
		if h.Match == m {
			text := r.expansion(h, q, repl)
			b.Group(func() { apply(b, m, text) })
			return text, nil
		}
	}
	return "", ErrNoMatch
}

// ReplaceAt is Replace for a buffer of any size, a large one included. It
// replaces the match m of q in b with repl, as one undo group, and returns the
// text that was inserted. It reads only the lines that m covers, so it does not
// need the match list that Replace builds from the whole buffer; m is usually
// one of the matches of a Scan or the result of FindNext.
//
// m must be a match of q in the text of its own lines, as Find sees them. The
// text outside those lines can change which matches Find would list (a match
// that overlaps an earlier one is not listed there), which ReplaceAt does not
// see. A regex is expanded as Replace does.
//
// If m is not such a match, or the query is empty, it returns ErrNoMatch. A
// match whose lines are over 4 MiB together returns ErrMatchTooLarge, and a line
// of a large buffer that is longer than the buffer reads returns
// buffer.ErrLineTooLong. When a large buffer refuses the edit (a line that
// cannot be edited, too many unsaved changes), the text is unchanged and the
// reason is returned. A pattern that does not compile returns an error.
func ReplaceAt(b *buffer.Buffer, q Query, repl string, m Match) (string, error) {
	first, last := m.Start.Line, m.End.Line
	if q.Text == "" || first < 0 || last < first || last >= b.Lines() || m.End.Less(m.Start) {
		return "", ErrNoMatch
	}
	re, err := compile(q)
	if err != nil {
		return "", err
	}
	// Each line is at least one byte with its line break, so a span of more
	// lines than the limit is too large without asking for any of them.
	if last-first >= maxReplaceBytes {
		return "", ErrMatchTooLarge
	}
	size := 0
	for i := first; i <= last; i++ {
		if b.LineCut(i) {
			return "", buffer.ErrLineTooLong
		}
		if size += b.LineBytes(i) + 1; size > maxReplaceBytes {
			return "", ErrMatchTooLarge
		}
	}
	var sb strings.Builder
	sb.Grow(size)
	for i := first; i <= last; i++ {
		if i > first {
			sb.WriteByte('\n')
		}
		sb.WriteString(b.Line(i))
	}
	d := newWindow(first, sb.String())
	r := result{doc: d, re: re}
	for _, h := range d.hits(re, q) {
		if h.Match != m {
			continue
		}
		text := r.expansion(h, q, repl)
		_ = b.Refusal() // a refusal that was not reported is not this edit's
		b.Group(func() { apply(b, m, text) })
		if err := b.Refusal(); err != nil {
			return "", err
		}
		return text, nil
	}
	return "", ErrNoMatch
}

// ReplaceAll replaces every match of q in b with repl and returns how many
// were replaced. The whole call is one undo group, so one Undo reverts all
// replacements and one Redo reapplies them. Nothing is changed when no match
// is found.
func ReplaceAll(b *buffer.Buffer, q Query, repl string) (int, error) {
	r, err := run(b, q)
	if err != nil {
		return 0, err
	}
	return r.replace(b, q, repl, func(Match, int) Decision {
		return DecisionReplaceAll
	}), nil
}

// QueryReplace replaces matches of q one at a time. For each match in
// document order, decide is called with the match, as it is at that point in
// the edit, and its index among the matches found before any edit. The
// result says what to do. It returns how many matches were replaced.
// Replacement stops at DecisionQuit and goes on without asking after
// DecisionReplaceAll. The whole call, including every decision, is one undo
// group, so one Undo reverts the whole session.
func QueryReplace(b *buffer.Buffer, q Query, repl string, decide func(m Match, index int) Decision) (int, error) {
	r, err := run(b, q)
	if err != nil {
		return 0, err
	}
	return r.replace(b, q, repl, decide), nil
}

// replace applies the decisions in document order, as one undo group. Each
// edit moves the positions of later matches, so they are shifted after every
// replacement. The byte offsets and captures in r come from the search before
// any edit, and the text under each later match is unchanged, so they stay
// valid.
func (r result) replace(b *buffer.Buffer, q Query, repl string, decide func(Match, int) Decision) int {
	n := 0
	b.Group(func() {
		all := false
		for i := range r.hits {
			h := r.hits[i]
			if !all {
				switch decide(h.Match, i) {
				case DecisionSkip:
					continue
				case DecisionQuit:
					return
				case DecisionReplaceAll:
					all = true
				}
			}
			newEnd := apply(b, h.Match, r.expansion(h, q, repl))
			n++
			for j := i + 1; j < len(r.hits); j++ {
				r.hits[j].Start = shift(r.hits[j].Start, h.End, newEnd)
				r.hits[j].End = shift(r.hits[j].End, h.End, newEnd)
			}
		}
	})
	return n
}

// apply replaces m with text through the buffer's history and returns the
// position just after the inserted text. An empty text only deletes.
func apply(b *buffer.Buffer, m Match, text string) buffer.Pos {
	b.Delete(m.Start, m.End)
	return b.Insert(m.Start, text)
}

// shift maps p, a position at or after oldEnd, to its place once the text
// that ended at oldEnd has been replaced by text ending at newEnd. Text after
// oldEnd is unchanged, so only its line and column offset move.
func shift(p, oldEnd, newEnd buffer.Pos) buffer.Pos {
	if p.Line == oldEnd.Line {
		return buffer.Pos{Line: newEnd.Line, Col: newEnd.Col + p.Col - oldEnd.Col}
	}
	return buffer.Pos{Line: p.Line + newEnd.Line - oldEnd.Line, Col: p.Col}
}
