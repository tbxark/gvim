package buffer

import (
	"errors"
	"slices"
	"strings"
)

// Edits of a large buffer never change a published state. Each one plans a
// new bigState (the planX methods below read the old state and change
// nothing), and only when the plan succeeds does the Buffer record it and make
// it current. A refused edit therefore leaves no trace: no history, no
// version, no redo change.

// Estimated memory of the overlay, per literal rune and per literal line (the
// slice header and bookkeeping).
const (
	runeCost = 4
	lineCost = 24
)

// overlayBytes estimates the memory held by the literal lines of st.
func overlayBytes(st bigState) int64 {
	if st.root == nil {
		return 0
	}
	return int64(st.root.litRunes)*runeCost + int64(st.root.litLines)*lineCost
}

// bigInsert inserts text at p, which is already clamped, in a large buffer.
func (b *Buffer) bigInsert(p Pos, text string) Pos {
	ns, cost, err := b.big.planInsert(p, text)
	if err != nil {
		b.refuse(err)
		return p
	}
	end := advance(p, text)
	b.recordBig(opInsert, p, end, text, len(text) > InlineOpMax, ns, cost)
	b.notifyCoarse(false)
	return end
}

// bigDelete removes from..to, which are clamped and ordered with from before
// to, in a large buffer. It returns the removed text, or "" when that is over
// InlineOpMax.
func (b *Buffer) bigDelete(from, to Pos) string {
	ns, text, tooBig, cost, err := b.big.planDelete(from, to)
	if err != nil {
		b.refuse(err)
		return ""
	}
	b.recordBig(opDelete, from, from, text, tooBig, ns, cost)
	b.notifyCoarse(false)
	return text
}

// checkEdit reports why no edit may be made now: the source failed, the first
// line is still provisional, or the overlay would pass its cap. growth is the
// estimated overlay bytes the edit adds beyond the lines it rewrites.
func (s *bigStore) checkEdit(growth int64) error {
	switch cur := overlayBytes(s.st); {
	case s.err != nil:
		return s.err
	case s.docLines() == 0:
		return ErrLineTooLong
	case cur >= s.maxOverlay || cur+growth > s.maxOverlay:
		return ErrOverlayFull
	}
	return nil
}

// editableLine returns document line i for an edit that reads it, or the
// reason it cannot be edited: its source bytes are not valid UTF-8, or it is
// longer than MaxLine and only shown cut.
func (s *bigStore) editableLine(i int) (lineInfo, error) {
	li, err := s.info(i)
	switch {
	case err != nil:
		return li, err
	case s.err != nil:
		return li, s.err
	case li.huge:
		return li, ErrLineTooLong
	case li.invalid:
		return li, ErrInvalidUTF8
	}
	return li, nil
}

// termAt is the terminator flag of document line i: the real terminator of a
// base line, the recorded flag of a literal one.
func (s *bigStore) termAt(i int) uint8 {
	r := s.ref(i)
	if r.isLit {
		return r.eol
	}
	_, _, term, err := s.lineRange(r.base)
	if err != nil {
		s.fail(err)
		return eolDefault
	}
	return term
}

// settleEOL gives the lines that replace the document lines starting at first
// a terminator when they only have the default one: they take the previous
// line's, so the line breaks an edit adds look like their neighbours. final
// says the last of repl is the document's last line, which has none.
func (s *bigStore) settleEOL(repl []litLine, first int, final bool) {
	for k := range repl {
		if repl[k].eol != eolDefault || k == len(repl)-1 && final {
			continue
		}
		switch {
		case k > 0:
			repl[k].eol = repl[k-1].eol
		case first > 0:
			repl[k].eol = s.termAt(first - 1)
		}
	}
}

// planReplace returns the state in which document lines [i, j) are replaced
// by repl, and the estimated bytes repl adds to the overlay.
func (s *bigStore) planReplace(i, j int, repl []litLine) (bigState, int64) {
	docLines := s.docLines()
	s.settleEOL(repl, i, j >= docLines)
	var cost int64
	for _, l := range repl {
		cost += int64(len(l.runes))*runeCost + lineCost
	}
	return s.tree.replace(s.st, docLines, i, j, repl), cost
}

// planInsert plans inserting text at p, which is clamped. The line is
// rewritten as literal lines, like the memory buffer does.
func (s *bigStore) planInsert(p Pos, text string) (bigState, int64, error) {
	if err := s.checkEdit(int64(len(text)) * runeCost); err != nil {
		return bigState{}, 0, err
	}
	li, err := s.editableLine(p.Line)
	if err != nil {
		return bigState{}, 0, err
	}
	head, tail := li.runes[:p.Col], li.runes[p.Col:]
	parts := strings.Split(text, "\n")
	repl := make([]litLine, 0, len(parts))
	if len(parts) == 1 {
		repl = append(repl, litLine{runes: slices.Concat(head, []rune(text), tail)})
	} else {
		repl = append(repl, litLine{runes: slices.Concat(head, []rune(parts[0]))})
		for _, part := range parts[1 : len(parts)-1] {
			repl = append(repl, litLine{runes: []rune(part)})
		}
		repl = append(repl, litLine{runes: slices.Concat([]rune(parts[len(parts)-1]), tail)})
	}
	// The edited line keeps its terminator, and so does every line the text
	// breaks it into.
	term := s.termAt(p.Line)
	for k := range repl {
		repl[k].eol = term
	}
	ns, cost := s.planReplace(p.Line, p.Line+1, repl)
	return ns, cost, nil
}

// planDelete plans removing from..to, which are clamped and ordered with from
// before to. It returns the removed text, or "" and tooBig when that is over
// InlineOpMax.
//
// The lines the range only drops are never read. The first line is read only
// when text before from stays, the last only when text after to stays; a
// range from column 0 to column 0 drops whole lines and reads nothing.
func (s *bigStore) planDelete(from, to Pos) (ns bigState, text string, tooBig bool, cost int64, err error) {
	if err = s.checkEdit(0); err != nil {
		return
	}
	var repl []litLine
	last := to.Line + 1
	if from.Col == 0 && to.Col == 0 {
		last = to.Line
	} else {
		var head, tail []rune
		if from.Col > 0 {
			var li lineInfo
			if li, err = s.editableLine(from.Line); err != nil {
				return
			}
			head = li.runes[:from.Col]
		}
		if to.Col > 0 || s.lineBytesLen(to.Line) > 0 {
			var li lineInfo
			if li, err = s.info(to.Line); err != nil {
				return
			}
			switch {
			case li.huge:
				// A line shown cut can only go whole, with the document's end.
				if to.Line != s.lines()-1 || to.Col < len(li.runes) {
					return ns, "", false, 0, ErrLineTooLong
				}
			case to.Col < len(li.runes):
				if li.invalid {
					return ns, "", false, 0, ErrInvalidUTF8
				}
				tail = li.runes[to.Col:]
			}
		}
		repl = []litLine{{runes: slices.Concat(head, tail), eol: s.termAt(to.Line)}}
	}
	if s.err != nil {
		return ns, "", false, 0, s.err
	}
	text, err = s.rangeLimited(from, to, InlineOpMax)
	switch {
	case errors.Is(err, ErrOverCap):
		text, tooBig = "", true
	case err != nil:
		return ns, "", false, 0, err
	}
	ns, cost = s.planReplace(from.Line, last, repl)
	return ns, text, tooBig, cost, nil
}
