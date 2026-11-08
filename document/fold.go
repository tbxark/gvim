package document

import (
	"cmp"
	"errors"
	"slices"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
)

// ErrNoFold is returned by toggle_fold when no fold starts on the caret line.
var ErrNoFold = errors.New("no fold starts on this line")

// Folds returns the fold ranges of the text: the indentation folds, merged with
// the ranges an external source (a language server) and an outline source (the
// Contents section) gave, ordered by start line, with tabWidth as the width of a
// tab. The ranges always match the text: they are found again when the text,
// tabWidth or a source's ranges are not the ones of the last call, and the same
// slice is returned until then, so the caller must not change it. A fold nested in
// another comes after it. A large document has no folds: finding them reads every
// line.
func (d *Document) Folds(tabWidth int) []fold.Range {
	if d.large != nil {
		return nil
	}
	key := foldKey{Gen: d.Gen(), Ver: d.Buffer.Version(), Tab: tabWidth, Rev: d.externalRev, OutlineRev: d.outlineRev}
	m := &d.FoldMemo
	if m.Ok && m.Key == key {
		return m.Ranges
	}
	b := d.Buffer
	indentOf := func(i int) int32 {
		if w, blank := b.LineIndent(i, tabWidth); !blank {
			return int32(w)
		}
		return -1
	}
	if m.Ok && m.Buf == b && m.Key.Gen == key.Gen && m.Key.Tab == tabWidth && len(m.Indents) == b.Lines() {
		// Typing does not change the indentation of a line, nor the folds. The lines that
		// edits touched since the last call are read again, and when each is as it was, so
		// are the ranges.
		if lo, hi, ok := b.EditedSince(m.Serial); ok && hi < len(m.Indents) {
			same := true
			for i := lo; i <= hi; i++ {
				if v := indentOf(i); v != m.Indents[i] {
					m.Indents[i], same = v, false
				}
			}
			// A new text version can invalidate the outline source's ranges even
			// when the edit did not change any indentation, so the merged set is
			// found again whenever the version moved and an outline source exists.
			outlineMoved := d.outlineSet && m.Key.Ver != key.Ver
			m.Key, m.Serial = key, b.EditSerial()
			if !same || outlineMoved {
				m.Ranges = mergeFoldRanges(m.findRanges(), d.externalFolds, d.currentOutlineFolds())
			}
			return m.Ranges
		}
	}
	m.Indents = make([]int32, b.Lines())
	for i := range m.Indents {
		m.Indents[i] = indentOf(i)
	}
	m.Buf, m.Key, m.Serial, m.Ok = b, key, b.EditSerial(), true
	m.Ranges = mergeFoldRanges(m.findRanges(), d.externalFolds, d.currentOutlineFolds())
	return m.Ranges
}

// SetExternalFolds replaces the fold ranges an external source gave for the
// current text: a language server's foldingRange, in zero-based inclusive lines.
// rev is the source's revision: the same revision with the same ranges does
// nothing, and a new one invalidates the fold memo, so Folds finds the merged set
// again. The merged ranges are what the pane's fold gutter and the fold actions
// see, so an LSP range folds exactly like an indentation fold.
func (d *Document) SetExternalFolds(ranges []fold.Range, rev uint64) {
	if d.externalRev == rev && slices.Equal(d.externalFolds, ranges) {
		return
	}
	d.externalFolds = slices.Clone(ranges)
	d.externalRev = rev
	d.FoldMemo.Ok = false
}

// ExternalFolds returns a copy of the fold ranges the external source gave last,
// for a caller that merges its own view.
func (d *Document) ExternalFolds() []fold.Range { return slices.Clone(d.externalFolds) }

// SetOutlineFolds replaces the structural fold ranges an outline source gave, tied
// to the text version they were computed from. The ranges merge into Folds only
// while the text still has that version: an edit drops them until the source
// publishes the ranges of the new text, so a stale range never folds a line it was
// not computed for. An empty list clears them, and a document that has no outline
// source keeps none. The fold memo is invalidated, so the merged set is found again.
func (d *Document) SetOutlineFolds(ranges []fold.Range, textVersion uint64) {
	d.outlineFolds = slices.Clone(ranges)
	d.outlineVer = textVersion
	d.outlineSet = len(ranges) > 0
	d.outlineRev++
	d.FoldMemo.Ok = false
}

// OutlineFolds returns a copy of the outline ranges last given, whether or not the
// text still has the version they were computed from.
func (d *Document) OutlineFolds() []fold.Range { return slices.Clone(d.outlineFolds) }

// currentOutlineFolds is the outline source's ranges as Folds uses them: nothing
// when the source gave none, or when the text changed since they were computed.
func (d *Document) currentOutlineFolds() []fold.Range {
	if !d.outlineSet || d.outlineVer != d.Buffer.Version() {
		return nil
	}
	return d.outlineFolds
}

// mergeFoldRanges merges the external and outline ranges into the indentation
// ranges, ordered by start line and with exact duplicates removed. A range nested
// in another keeps its place after it, as fold.Ranges returns nesting: the pane and
// the fold actions read the same ordering from every source.
func mergeFoldRanges(indent, external, outline []fold.Range) []fold.Range {
	if len(external) == 0 && len(outline) == 0 {
		return indent
	}
	out := make([]fold.Range, 0, len(indent)+len(external)+len(outline))
	out = append(out, indent...)
	out = append(out, external...)
	out = append(out, outline...)
	slices.SortStableFunc(out, func(a, b fold.Range) int {
		if a.Start != b.Start {
			return cmp.Compare(a.Start, b.Start)
		}
		return cmp.Compare(a.End, b.End)
	})
	return slices.CompactFunc(out, func(a, b fold.Range) bool { return a == b })
}

// foldKey identifies the input of fold.Ranges: the buffer, its content version, the
// tab width and the revision of the external (language server) ranges.
type foldKey struct {
	Gen, Ver   uint64
	Tab        int
	Rev        uint64
	OutlineRev uint64
}

// foldMemo is the ranges Folds found last, and what they were found from: the indentation of
// every line (or -1 for a blank one) in the buffer at edit serial serial.
type foldMemo struct {
	Ok      bool
	Key     foldKey
	Buf     *buffer.Buffer
	Serial  uint64
	Indents []int32
	Ranges  []fold.Range
}

// findRanges finds the ranges of the remembered indentation.
func (m *foldMemo) findRanges() []fold.Range {
	return fold.RangesFunc(len(m.Indents), func(i int) (int, bool) {
		return int(m.Indents[i]), m.Indents[i] < 0
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
func (d *Document) RevealCarets(from, tabWidth int) {
	if len(d.folded) == 0 {
		return
	}
	ranges := d.Folds(tabWidth)
	lines := d.Buffer.Lines()
	cs := d.AllCarets()
	changed := false
	for i := range cs {
		c := &cs[i]
		head, anchor := c.Head, c.Anchor
		if r, ok := d.hidingIn(ranges, head.Line); ok {
			line := r.Start
			if c.Primary && head.Line > from && r.End+1 < lines {
				line = r.End + 1
			}
			head = d.Buffer.Clamp(buffer.Pos{Line: line, Col: head.Col})
		}
		if c.Anchor == c.Head {
			anchor = head
		} else if r, ok := d.hidingIn(ranges, anchor.Line); ok {
			anchor = d.Buffer.Clamp(buffer.Pos{Line: r.Start, Col: anchor.Col})
		}
		if head != c.Head || anchor != c.Anchor {
			c.Head, c.Anchor = head, anchor
			changed = true
		}
	}
	if !changed {
		return
	}
	// A reveal is not a vertical move, so the wanted column and the block
	// selection are kept.
	want, block := d.Want, d.Block
	d.SetCarets(cs)
	d.Want, d.Block = want, block
}
