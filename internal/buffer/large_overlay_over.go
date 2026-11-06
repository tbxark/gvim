package buffer

import (
	"errors"
	"fmt"
	"sort"
)

// A save of a large buffer writes a file whose lines are the lines of one state of the
// buffer. When the buffer cannot move onto that file (it was edited while the file was
// written, or the file could not be opened again), it keeps reading the old source, and
// its overlay is a delta over the old file. That delta does not fit the new file, so a
// stash of it would be refused. TextState and OverlayOver express the buffer's edits
// over the new file instead.

// errOverlayOverIndex is returned by OverlayOver while the index of the source is not
// complete.
var errOverlayOverIndex = errors.New("buffer: the line index is not complete")

// TextState is the text of a large buffer at one moment, kept to describe later edits
// over a file that was written from it. It holds no text of the source.
type TextState struct {
	st bigState
}

// TextState returns the current text of a large buffer, or false for a memory buffer.
// It costs nothing: the state is immutable.
func (b *Buffer) TextState() (*TextState, bool) {
	if b.big == nil {
		return nil, false
	}
	return &TextState{st: b.big.st}, true
}

// walkPieces calls f for the pieces of n in document order.
func walkPieces(n *node, f func(piece)) {
	if n == nil {
		return
	}
	walkPieces(n.l, f)
	f(n.p)
	walkPieces(n.r, f)
}

// lineSeg says that base lines [old, old+n) of the source are lines [new, new+n) of a
// file written from a text state.
type lineSeg struct{ old, n, new int }

// OverlayOver returns the unsaved edits of the buffer as a delta over the file that was
// written from the text of base: its lines are the lines of base in order, and the
// source of the buffer is the file base was taken over. A line of the source that base
// still had is a line of the file, and one that base had replaced is read from the
// source and becomes a literal line. It needs the complete line index of the source,
// and fails with an error when a line it would have to copy cannot be edited.
func (b *Buffer) OverlayOver(base *TextState) (OverlayState, error) {
	s := b.big
	switch {
	case s == nil:
		return OverlayState{}, errNotLarge
	case s.closed:
		return OverlayState{}, errClosed
	case base == nil:
		return OverlayState{}, errors.New("buffer: no text state")
	case !s.g.complete():
		return OverlayState{}, errOverlayOverIndex
	}
	baseLines := s.g.baseLines()
	var segs []lineSeg
	pos := 0
	add := func(first, n int) {
		if n > 0 {
			segs = append(segs, lineSeg{old: first, n: n, new: pos})
			pos += n
		}
	}
	walkPieces(base.st.root, func(p piece) {
		if p.isRun() {
			add(p.first, p.count)
		} else {
			pos += len(p.lit)
		}
	})
	add(base.st.tailFirst, baseLines-base.st.tailFirst)
	total := pos
	for i := 1; i < len(segs); i++ {
		if segs[i].old < segs[i-1].old+segs[i-1].n {
			return OverlayState{}, fmt.Errorf("%w: lines out of order", ErrOverlayInvalid)
		}
	}

	m := overMapper{s: s, segs: segs}
	var err error
	walkPieces(s.st.root, func(p piece) {
		switch {
		case err != nil:
		case p.isRun():
			err = m.run(p.first, p.count)
		default:
			m.out = append(m.out, literalPiece(p))
		}
	})
	if err == nil && s.st.tailFirst < baseLines {
		err = m.run(s.st.tailFirst, baseLines-s.st.tailFirst)
	}
	if err != nil {
		return OverlayState{}, err
	}
	// The lines that end the file as it is written are the tail.
	out, tail := m.out, total
	if n := len(out); n > 0 && out[n-1].Run && out[n-1].First+out[n-1].Count == total {
		tail = out[n-1].First
		out = out[:n-1]
	}
	return OverlayState{Pieces: out, TailFirst: tail}, nil
}

// literalPiece is the overlay piece of the literal piece p.
func literalPiece(p piece) OverlayPiece {
	lines := make([]string, len(p.lit))
	for i, rs := range p.lit {
		lines[i] = string(rs)
	}
	return OverlayPiece{Lines: lines, EOL: eolFlags(p)}
}

// overMapper builds the pieces of OverlayOver.
type overMapper struct {
	s    *bigStore
	segs []lineSeg
	out  []OverlayPiece
	// cost is the estimated memory of the lines copied from the source.
	cost int64
}

// addRun appends a run over lines of the new file, joined to the run before it when
// they follow each other.
func (m *overMapper) addRun(first, n int) {
	if k := len(m.out) - 1; k >= 0 && m.out[k].Run && m.out[k].First+m.out[k].Count == first {
		m.out[k].Count += n
		return
	}
	m.out = append(m.out, OverlayPiece{Run: true, First: first, Count: n})
}

// run maps the run of source lines [a, a+n) to the new file.
func (m *overMapper) run(a, n int) error {
	end := a + n
	i := sort.Search(len(m.segs), func(i int) bool { return m.segs[i].old+m.segs[i].n > a })
	for a < end {
		if i < len(m.segs) && m.segs[i].old <= a {
			sg := m.segs[i]
			x := min(end, sg.old+sg.n)
			m.addRun(sg.new+a-sg.old, x-a)
			a = x
			i++
			continue
		}
		x := end
		if i < len(m.segs) {
			x = min(end, m.segs[i].old)
		}
		if err := m.copyLines(a, x); err != nil {
			return err
		}
		a = x
	}
	return nil
}

// copyLines appends source lines [a, b) as literal lines, for lines the new file does
// not have as they were.
func (m *overMapper) copyLines(a, b int) error {
	s := m.s
	op := OverlayPiece{Lines: make([]string, 0, b-a)}
	flags := make([]byte, 0, b-a)
	mixed := false
	for j := a; j < b; j++ {
		d, err := s.decodedBase(j)
		switch {
		case err != nil:
			return err
		case d.huge:
			return ErrLineTooLong
		case d.invalid:
			return ErrInvalidUTF8
		}
		if m.cost += int64(len(d.runes))*runeCost + lineCost; m.cost > s.maxOverlay {
			return ErrOverlayFull
		}
		_, _, term, err := s.lineRange(j)
		if err != nil {
			return err
		}
		flag := byte(eolFlagDefault)
		switch term {
		case eolLF:
			flag = eolFlagLF
		case eolCRLF:
			flag = eolFlagCRLF
		}
		mixed = mixed || flag != eolFlagDefault
		op.Lines = append(op.Lines, string(d.runes))
		flags = append(flags, flag)
	}
	if mixed {
		op.EOL = string(flags)
	}
	m.out = append(m.out, op)
	return nil
}
