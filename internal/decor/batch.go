package decor

import (
	"slices"
	"sort"
)

// composed is a sequence of edits that go down the document: every edit starts at or after
// the end of the text the one before inserted. Such a sequence moves a position in one
// step, whatever its length, instead of one step for each edit.
//
// The edits are in sequence coordinates (each in the text after the ones before it). The
// text removed by edit k is [from, oe[k]) in the coordinates of the text before the whole
// sequence, and the text it inserted ends at ne[k] in the coordinates after edit k. Nothing
// before an edit's start moves because of the edits after it, so a position that lies after
// edit k's removed text, in either coordinates, differs by the same shift from the other.
type composed struct {
	sh     []shape
	oe, ne []pos
}

// compose reports whether the edits form a sequence that goes down the document and whose
// edits change the number of lines; only then is a pass over the items cheaper than one
// short pass for each edit.
func compose(shapes []shape) (*composed, bool) {
	if len(shapes) < 2 {
		return nil, false
	}
	c := &composed{sh: shapes, oe: make([]pos, len(shapes)), ne: make([]pos, len(shapes))}
	var oe, ne pos // the end of the last removed and inserted text; both start at the origin
	lines := false
	for k, sh := range shapes {
		if sh.from.less(ne) {
			return nil, false
		}
		oe = shift(ne, oe, sh.to) // where the removed text ends before the whole sequence
		if sh.nl == 0 {
			ne = pos{sh.from.line, sh.from.col + len(sh.text)}
		} else {
			ne = pos{sh.from.line + sh.nl, sh.lastLen}
		}
		c.oe[k], c.ne[k] = oe, ne
		lines = lines || sh.lineDelta() != 0
	}
	return c, lines
}

// shift moves p, which is at or after the point from, to where it is when the point from has
// moved to the point to: positions on the line of from keep their distance from it, the lines
// below move as a whole.
func shift(from, to, p pos) pos {
	if p.line == from.line {
		return pos{to.line, to.col + p.col - from.col}
	}
	return pos{p.line - from.line + to.line, p.col}
}

// walk moves p through the sequence. step moves it through one edit and reports false when
// what p anchors is gone. The edits that end before p are one shift; only the few edits
// that reach p (or an insertion right at it) are applied one by one, with step.
func (c *composed) walk(p pos, step func(shape, pos) (pos, bool)) (pos, bool) {
	a := sort.Search(len(c.sh), func(i int) bool { return !c.oe[i].less(p) })
	q := p
	if a > 0 {
		q = shift(c.oe[a-1], c.ne[a-1], p)
	}
	for k := a; k < len(c.sh); k++ {
		// This and the edits after it start at or after q: they do not move it.
		if q.less(c.sh[k].from) {
			break
		}
		var ok bool
		if q, ok = step(c.sh[k], q); !ok {
			return q, false
		}
	}
	return q, true
}

// applyComposed moves the items of the group through the sequence in one pass and returns how
// many it removed.
func (g *group) applyComposed(c *composed) (removed int) {
	items := g.items
	// Items that end before the first edit are not touched.
	lo, _ := slices.BinarySearchFunc(items, c.sh[0].from.line-g.maxSpan, func(e entry, line int) int {
		return e.FromLine - line
	})
	for i := lo; i < len(items); i++ {
		e := &items[i]
		if !e.moveComposed(c) {
			e.id = 0 // marks the item for removal
			removed++
		} else if e.Kind == Highlight || e.Kind == Scroll {
			g.maxSpan = max(g.maxSpan, e.ToLine-e.FromLine)
		}
	}
	for i := max(lo, 1); i < len(items); i++ {
		if items[i].FromLine < items[i-1].FromLine {
			g.sorted = false
			break
		}
	}
	if removed > 0 {
		g.items = slices.DeleteFunc(items, func(e entry) bool { return e.id == 0 })
		g.index = nil
	}
	return removed
}

// moveComposed is move for a whole sequence of edits.
func (e *entry) moveComposed(c *composed) bool {
	switch e.Kind {
	case Highlight:
		return e.moveRangeComposed(c)
	case Scroll:
		if e.ToLine != e.FromLine || e.ToCol != e.FromCol {
			return e.moveRangeComposed(c)
		}
	case Replace:
		p, ok := c.walk(pos{e.FromLine, e.FromCol}, stepReplace)
		e.FromLine, e.FromCol, e.ToLine, e.ToCol = p.line, p.col, p.line, p.col
		return ok
	}
	p, ok := c.walk(pos{e.FromLine, 0}, stepLine)
	e.FromLine, e.FromCol, e.ToLine, e.ToCol = p.line, 0, p.line, 0
	return ok
}

// moveRangeComposed is moveRange for a whole sequence. An edit that empties the range leaves
// its end no later than its start, and the later edits keep it so, so checking at the end
// removes exactly the ranges that one edit at a time would.
func (e *entry) moveRangeComposed(c *composed) bool {
	from, _ := c.walk(pos{e.FromLine, e.FromCol}, func(sh shape, p pos) (pos, bool) { return sh.mapPoint(p, true), true })
	to, _ := c.walk(pos{e.ToLine, e.ToCol}, func(sh shape, p pos) (pos, bool) { return sh.mapPoint(p, false), true })
	if !from.less(to) {
		return false
	}
	e.FromLine, e.FromCol, e.ToLine, e.ToCol = from.line, from.col, to.line, to.col
	return true
}
