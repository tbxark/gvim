package search

import (
	"errors"

	"github.com/tbxark/gvim/internal/buffer"
)

// ErrNoMatch is returned by Replace when m is not a match of q in the buffer
// as it is now, for example because the text changed after Find.
var ErrNoMatch = errors.New("search: no match of the query at the given position")

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

// DecisionFromKey maps a query-replace key to its Decision: 'y', 'n', '!' or
// 'q'. It reports false for any other key, so the caller can ask again.
func DecisionFromKey(key rune) (Decision, bool) {
	switch key {
	case 'y':
		return DecisionReplace, true
	case 'n':
		return DecisionSkip, true
	case '!':
		return DecisionReplaceAll, true
	case 'q':
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
