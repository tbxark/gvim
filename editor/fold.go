package editor

import (
	"errors"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
)

// ErrNoFold is returned by toggle_fold when no fold starts on the caret line.
var ErrNoFold = errors.New("no fold starts on this line")

// Folds returns the indentation fold ranges of the text, ordered by start line,
// with tabWidth as the width of a tab. The ranges always match the text: they are
// found again when the text or tabWidth is not the one of the last call, and the
// same slice is returned until then, so the caller must not change it. A fold
// nested in another comes after it. A large document has no folds: finding them
// reads every line.
func (d *Document) Folds(tabWidth int) []fold.Range {
	if d.large != nil {
		return nil
	}
	key := foldKey{gen: d.Gen(), ver: d.Buffer.Version(), tab: tabWidth}
	m := &d.foldMemo
	if m.ok && m.key == key {
		return m.ranges
	}
	b := d.Buffer
	indentOf := func(i int) int32 {
		if w, blank := b.LineIndent(i, tabWidth); !blank {
			return int32(w)
		}
		return -1
	}
	if m.ok && m.buf == b && m.key.gen == key.gen && m.key.tab == tabWidth && len(m.indents) == b.Lines() {
		// Typing does not change the indentation of a line, nor the folds. The lines that
		// edits touched since the last call are read again, and when each is as it was, so
		// are the ranges.
		if lo, hi, ok := b.EditedSince(m.serial); ok && hi < len(m.indents) {
			same := true
			for i := lo; i <= hi; i++ {
				if v := indentOf(i); v != m.indents[i] {
					m.indents[i], same = v, false
				}
			}
			m.key, m.serial = key, b.EditSerial()
			if !same {
				m.ranges = m.findRanges()
			}
			return m.ranges
		}
	}
	m.indents = make([]int32, b.Lines())
	for i := range m.indents {
		m.indents[i] = indentOf(i)
	}
	m.buf, m.key, m.serial, m.ok = b, key, b.EditSerial(), true
	m.ranges = m.findRanges()
	return m.ranges
}

// foldKey identifies the input of fold.Ranges: the buffer, its content version and
// the tab width.
type foldKey struct {
	gen, ver uint64
	tab      int
}

// foldMemo is the ranges Folds found last, and what they were found from: the indentation of
// every line (or -1 for a blank one) in the buffer at edit serial serial.
type foldMemo struct {
	ok      bool
	key     foldKey
	buf     *buffer.Buffer
	serial  uint64
	indents []int32
	ranges  []fold.Range
}

// findRanges finds the ranges of the remembered indentation.
func (m *foldMemo) findRanges() []fold.Range {
	return fold.RangesFunc(len(m.indents), func(i int) (int, bool) {
		return int(m.indents[i]), m.indents[i] < 0
	})
}

// Folded reports whether the fold that starts at line is collapsed. It does not
// check that a fold starts there.
func (d *Document) Folded(line int) bool {
	return d.folded[line]
}

// Hidden reports whether line is hidden by a collapsed fold. The start line of a
// fold is never hidden by that fold.
func (d *Document) Hidden(line, tabWidth int) bool {
	_, ok := d.hidingFold(line, tabWidth)
	return ok
}

// hidingFold returns the outermost collapsed fold that hides line. Folds are
// ordered by start line, and folds nest, so the first match is the outermost.
func (d *Document) hidingFold(line, tabWidth int) (fold.Range, bool) {
	if len(d.folded) == 0 {
		return fold.Range{}, false
	}
	return d.hidingIn(d.Folds(tabWidth), line)
}

// hidingIn is hidingFold over ranges, which must be d.Folds for the same text.
func (d *Document) hidingIn(ranges []fold.Range, line int) (fold.Range, bool) {
	for _, r := range ranges {
		if d.folded[r.Start] && r.Start < line && line <= r.End {
			return r, true
		}
	}
	return fold.Range{}, false
}

// revealCarets moves every caret off a collapsed fold. from is the line of the
// primary caret before the action. The primary caret, when its head moved down,
// lands on the first line below the fold, so the arrow keys skip the body. Any
// other hidden end goes to the fold start with its column kept: the anchor of a
// selection, and the head of an extra caret, which has no direction. A fold that
// runs to the end of the text keeps a downward caret at its start. Carets that
// meet after the move are merged.
func (d *Document) revealCarets(from, tabWidth int) {
	if len(d.folded) == 0 {
		return
	}
	ranges := d.Folds(tabWidth)
	lines := d.Buffer.Lines()
	cs := d.allCarets()
	changed := false
	for i := range cs {
		c := &cs[i]
		head, anchor := c.head, c.anchor
		if r, ok := d.hidingIn(ranges, head.Line); ok {
			line := r.Start
			if c.primary && head.Line > from && r.End+1 < lines {
				line = r.End + 1
			}
			head = d.Buffer.Clamp(buffer.Pos{Line: line, Col: head.Col})
		}
		if c.anchor == c.head {
			anchor = head
		} else if r, ok := d.hidingIn(ranges, anchor.Line); ok {
			anchor = d.Buffer.Clamp(buffer.Pos{Line: r.Start, Col: anchor.Col})
		}
		if head != c.head || anchor != c.anchor {
			c.head, c.anchor = head, anchor
			changed = true
		}
	}
	if !changed {
		return
	}
	// A reveal is not a vertical move, so the wanted column and the block
	// selection are kept.
	want, block := d.want, d.block
	d.setCarets(cs)
	d.want, d.block = want, block
}

// toggleFold collapses the fold that starts on the caret line, or expands it when
// it is collapsed already. A collapsed start that no longer begins a fold, because
// the text above it changed, can still be expanded.
func toggleFold(e *Editor, d *Document, _ map[string]any) error {
	return e.toggleFoldAt(d, d.Cursor.Line)
}

// toggleFoldAt is toggleFold for the fold that starts at line.
func (e *Editor) toggleFoldAt(d *Document, line int) error {
	if d.large != nil {
		// A large document has no folds, so "no fold starts on this line" would be
		// untrue of a line that looks like it starts one.
		return &largeError{msg: "folds are not available in large file mode", errs: []error{ErrLargeFile}}
	}
	if d.folded[line] {
		delete(d.folded, line)
		return nil
	}
	for _, r := range d.Folds(e.tabSizeOf(d)) {
		if r.Start == line {
			if d.folded == nil {
				d.folded = map[int]bool{}
			}
			d.folded[line] = true
			return nil
		}
	}
	return ErrNoFold
}
