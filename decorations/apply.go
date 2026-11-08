package decorations

import (
	"slices"
	"strings"
)

// pos is a position in byte columns.
type pos struct{ line, col int }

func (p pos) less(q pos) bool { return p.line < q.line || (p.line == q.line && p.col < q.col) }

// shape is an edit with the numbers the anchor arithmetic needs.
type shape struct {
	from, to pos
	text     string
	nl       int // line breaks in text
	lastLen  int // bytes after the last line break of text (all of text when nl == 0)
}

func shapeOf(e Edit) shape {
	s := shape{
		from: pos{max(e.FromLine, 0), max(e.FromCol, 0)},
		to:   pos{max(e.ToLine, 0), max(e.ToCol, 0)},
		text: e.Text,
	}
	if s.to.less(s.from) {
		s.to = s.from
	}
	s.nl = strings.Count(e.Text, "\n")
	s.lastLen = len(e.Text)
	if s.nl > 0 {
		s.lastLen = len(e.Text) - strings.LastIndexByte(e.Text, '\n') - 1
	}
	return s
}

// deletes reports whether the edit removes text.
func (s shape) deletes() bool { return s.from.less(s.to) }

// lineDelta is how many lines the edit adds to the lines after it.
func (s shape) lineDelta() int { return s.nl - (s.to.line - s.from.line) }

// mapPoint moves p the way the text under it moves. A point inside the removed text
// collapses to its start. A point at the place where text is inserted stays before the
// new text, or goes after it when right is set.
func (s shape) mapPoint(p pos, right bool) pos {
	if s.deletes() && s.from.less(p) {
		switch {
		case p.less(s.to):
			p = s.from
		case p.line == s.to.line:
			p = pos{s.from.line, s.from.col + p.col - s.to.col}
		default:
			p.line -= s.to.line - s.from.line
		}
	}
	if s.text == "" || p.less(s.from) || (p == s.from && !right) {
		return p
	}
	if p.line != s.from.line {
		p.line += s.nl
		return p
	}
	if s.nl == 0 {
		p.col += len(s.text)
		return p
	}
	return pos{s.from.line + s.nl, s.lastLen + p.col - s.from.col}
}

// Apply moves the anchors of the document through edits, which are applied first to last.
// With coarse set the extent of the change is unknown (a large file, a replaced buffer) and
// every decoration of the document is dropped; the plugins that own them set them again.
//
// An item is removed when the text it was anchored to is gone: a range that becomes empty,
// the character a Replace sits on, a line item whose line (with its line break) is deleted.
func (s *Store) Apply(doc uint64, edits []Edit, coarse bool) {
	d := s.docs[doc]
	if d == nil {
		return
	}
	if coarse {
		s.DropDoc(doc)
		return
	}
	if len(edits) == 0 {
		return
	}
	for _, e := range edits {
		s.queue(d, shapeOf(e))
	}
	s.changed(d)
	s.flushDoc(doc)
}

// Queue records one edit of the document, in the coordinates of the document just before it,
// to be applied together with the edits that follow: every reader of the store applies the
// queued edits first. A bulk operation (Replace All, a multi-caret edit) reports one change
// per replacement, and the anchors below an edit that adds or removes a line all move, so
// applying them one at a time costs the edits times the items. Queued edits that go down the
// document, as those of a replacement in document order do, are applied in one pass.
func (s *Store) Queue(doc uint64, e Edit) {
	d := s.docs[doc]
	if d == nil {
		return
	}
	s.queue(d, shapeOf(e))
	if len(d.pend) >= maxQueued {
		s.flushDoc(doc)
	}
}

// maxQueued is the most edits a document keeps queued; the next one applies them.
const maxQueued = 8192

func (s *Store) queue(d *docDecor, sh shape) {
	if !sh.deletes() && sh.text == "" {
		return
	}
	if len(d.pend) == 0 {
		s.queued++
	}
	d.pend = append(d.pend, sh)
	s.changed(d)
}

// flushDoc applies the edits queued on the document.
func (s *Store) flushDoc(doc uint64) {
	d := s.docs[doc]
	if d == nil || len(d.pend) == 0 {
		return
	}
	shapes := d.pend
	d.pend = nil
	s.queued--
	for _, g := range d.groups {
		if n := g.applyAll(shapes); n > 0 {
			s.perOwner[g.owner] -= n
		}
	}
	d.windows = nil
	s.tidy(doc)
}

// flush applies the queued edits of one document (doc 0: of every document). Every method that
// reads or changes the items starts with it.
func (s *Store) flush(doc uint64) {
	switch {
	case s.queued == 0:
	case doc != 0:
		s.flushDoc(doc)
	default:
		for _, id := range s.docIDs() {
			s.flushDoc(id)
		}
	}
}

// applyAll moves the items of the group through the edits and returns how many it removed.
func (g *group) applyAll(shapes []shape) (removed int) {
	g.sort()
	if c, ok := compose(shapes); ok {
		return g.applyComposed(c)
	}
	for _, sh := range shapes {
		g.sort()
		removed += g.apply(sh)
	}
	return removed
}

// apply moves the items of the group through one edit and returns how many it removed.
func (g *group) apply(sh shape) (removed int) {
	items := g.items
	// Items that end before the edit begins are not touched: they start more than maxSpan
	// lines above it.
	lo, _ := slices.BinarySearchFunc(items, sh.from.line-g.maxSpan, func(e entry, line int) int {
		return e.FromLine - line
	})
	delta := sh.lineDelta()
	touched := max(sh.to.line, sh.from.line+sh.nl)
	drop := false
	end := lo
	for i := lo; i < len(items); i++ {
		e := &items[i]
		if delta == 0 && e.FromLine > touched {
			// Sorted by start line, and nothing below the edit moves.
			break
		}
		end = i + 1
		if !e.move(sh) {
			e.id = 0 // marks the item for removal
			drop = true
			removed++
		} else if e.Kind == Highlight || e.Kind == Scroll || e.Kind == Conceal {
			g.maxSpan = max(g.maxSpan, e.ToLine-e.FromLine) // a line break inside a range widens it
		}
	}
	// Two items that start on the same line can move to different lines (a line break
	// inserted between them), so the order by start line may be broken now.
	for i := max(lo, 1); i < min(end+1, len(items)); i++ {
		if items[i].FromLine < items[i-1].FromLine {
			g.sorted = false
			break
		}
	}
	if drop {
		g.items = slices.DeleteFunc(items, func(e entry) bool { return e.id == 0 })
		g.index = nil
	}
	return removed
}

// move moves the item through the edit and reports whether it survives.
func (e *entry) move(sh shape) bool {
	switch e.Kind {
	case Highlight, Conceal:
		return e.moveRange(sh)
	case CursorLine:
		return true // the overlay is the caret's row, not a position on the text
	case Scroll:
		if e.ToLine != e.FromLine || e.ToCol != e.FromCol {
			return e.moveRange(sh)
		}
		return e.moveLine(sh)
	case Replace:
		p, ok := stepReplace(sh, pos{e.FromLine, e.FromCol})
		e.FromLine, e.FromCol, e.ToLine, e.ToCol = p.line, p.col, p.line, p.col
		return ok
	}
	return e.moveLine(sh)
}

// moveRange moves both ends of a range: the start stays before text inserted at it and
// the end stays before text inserted at it, so typing at an edge does not grow the range.
func (e *entry) moveRange(sh shape) bool {
	from := sh.mapPoint(pos{e.FromLine, e.FromCol}, true)
	to := sh.mapPoint(pos{e.ToLine, e.ToCol}, false)
	if !from.less(to) {
		return false
	}
	e.FromLine, e.FromCol, e.ToLine, e.ToCol = from.line, from.col, to.line, to.col
	return true
}

// moveLine moves an item that sits on a line. It follows the start of the line: text
// inserted at the start pushes it down with the line's own text, and it is removed when
// the line goes, that is when the edit removes the start of the line and its break.
func (e *entry) moveLine(sh shape) bool {
	p, ok := stepLine(sh, pos{e.FromLine, 0})
	e.FromLine, e.FromCol, e.ToLine, e.ToCol = p.line, 0, p.line, 0
	return ok
}

// stepReplace moves the position of a Replace through one edit; it reports false when the
// character at it is gone.
func stepReplace(sh shape, p pos) (pos, bool) {
	if sh.deletes() && !p.less(sh.from) && p.less(sh.to) {
		return p, false
	}
	return sh.mapPoint(p, true), true
}

// stepLine moves the start of a line through one edit; it reports false when the line is gone.
// The result is the start of the line again, whatever text was put at it.
func stepLine(sh shape, p pos) (pos, bool) {
	if sh.deletes() && !p.less(sh.from) && sh.to.line > p.line {
		return p, false
	}
	p = sh.mapPoint(p, true)
	return pos{p.line, 0}, true
}
