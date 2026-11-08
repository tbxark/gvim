package document

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/tbxark/gvim/buffer"
)

// errFirstLine and errLastLine are returned when a caret cannot be added above
// the first line or below the last one.
var (
	errFirstLine = errors.New("already at first line")
	errLastLine  = errors.New("already at last line")
)

// Caret is a caret other than the primary one: where it sits and the selection
// it carries. Sel is nil when the caret has no selection, as Document.Sel is.
type Caret struct {
	Cursor buffer.Pos
	Sel    *buffer.Selection
	// want is the display column vertical moves aim at, plus one. Zero is unset.
	Want int
}

// Cursors returns the extra carets in document order. The primary caret is
// Document.Cursor and Document.Sel and is not in the list. The result is a
// copy, and its positions are clamped into the document. It returns nil when
// there is one caret. SetCursor, SelectRange and the other primary-only calls
// leave these carets in place; a UI that wants a click to clear them sends
// remove_secondary_cursors first.
func (d *Document) Cursors() []Caret {
	if len(d.Extra) == 0 {
		return nil
	}
	out := make([]Caret, len(d.Extra))
	for i, x := range d.Extra {
		out[i].Cursor = d.Buffer.Clamp(x.Cursor)
		if x.Sel != nil {
			out[i].Sel = &buffer.Selection{Anchor: d.Buffer.Clamp(x.Sel.Anchor), Head: out[i].Cursor}
		}
	}
	return out
}

// caret is one caret as the multi-caret operations see it: the selection it
// covers, with the anchor and head of buffer.Selection (equal when it has
// none), and whether it is the primary caret.
type caret struct {
	Anchor, Head buffer.Pos
	Primary      bool
	// want is the wanted column of vertical moves, plus one. Zero is unset.
	Want int
	// Pad is how many spaces the caret's line needs before an edit at the caret,
	// for the lines of a virtual block selection whose left edge is past their
	// end. Edits that insert text materialize it (see replaceEdit); deletions
	// ignore it.
	Pad int
}

// bounds returns the caret's selection in document order. An empty caret has
// lo equal to hi.
func (c caret) Bounds() (lo, hi buffer.Pos) {
	if c.Head.Less(c.Anchor) {
		return c.Head, c.Anchor
	}
	return c.Anchor, c.Head
}

// selection returns the caret's selection, or nil when it has none.
func (c caret) Selection() *buffer.Selection {
	if c.Anchor == c.Head {
		return nil
	}
	return &buffer.Selection{Anchor: c.Anchor, Head: c.Head}
}

// meets reports whether c and o become one caret: their selections overlap, or
// they touch and one of them is empty. Two selections that only touch, such as
// [0,2) and [2,4), stay apart.
func (c caret) meets(o caret) bool {
	lo1, hi1 := c.Bounds()
	lo2, hi2 := o.Bounds()
	if lo2.Less(lo1) {
		lo1, hi1, lo2, hi2 = lo2, hi2, lo1, hi1
	}
	if lo2.Less(hi1) {
		return true
	}
	return lo2 == hi1 && (lo1 == hi1 || lo2 == hi2)
}

// normalize sorts cs into document order and merges the carets that meet. See
// mergeCarets. The slice is reordered in place.
func normalize(cs []caret) []caret {
	slices.SortStableFunc(cs, func(a, b caret) int {
		alo, _ := a.Bounds()
		blo, _ := b.Bounds()
		return comparePos(alo, blo)
	})
	out := cs[:0]
	for _, c := range cs {
		if n := len(out); n > 0 && out[n-1].meets(c) {
			out[n-1] = mergeCarets(out[n-1], c)
			continue
		}
		out = append(out, c)
	}
	return out
}

// mergeCarets returns one caret covering a and b, where a comes first in
// document order. The merged caret is the primary one if either was. Its
// direction is the primary's, or a's when neither is primary.
func mergeCarets(a, b caret) caret {
	lo, hi := a.Bounds()
	if _, h := b.Bounds(); hi.Less(h) {
		hi = h
	}
	w := a
	if b.Primary {
		w = b
	}
	m := caret{Primary: a.Primary || b.Primary, Want: w.Want}
	if w.Head.Less(w.Anchor) {
		m.Anchor, m.Head = hi, lo
	} else {
		m.Anchor, m.Head = lo, hi
	}
	return m
}

// comparePos orders p and q in document order, as cmp.Compare orders numbers.
func comparePos(p, q buffer.Pos) int {
	return cmp.Or(cmp.Compare(p.Line, q.Line), cmp.Compare(p.Col, q.Col))
}

// allCarets returns the carets of d in document order, the primary one marked.
// The list is normalized, so no two carets meet.
//
// The positions are clamped into the text. An extra caret can be stale after an
// action that changed the text, and an edit must not use a position past the end.
func (d *Document) AllCarets() []caret {
	p := caret{Anchor: d.Cursor, Head: d.Cursor, Primary: true, Want: d.Want + 1}
	if d.Sel != nil {
		p.Anchor = d.Sel.Anchor
	}
	cs := make([]caret, 0, 1+len(d.Extra))
	cs = append(cs, d.clampCaret(p))
	for _, x := range d.Extra {
		c := caret{Anchor: x.Cursor, Head: x.Cursor, Want: x.Want}
		if x.Sel != nil {
			c.Anchor = x.Sel.Anchor
		}
		cs = append(cs, d.clampCaret(c))
	}
	cs = normalize(cs)
	// A virtual block selection edits every line of the rectangle: a line whose
	// end is left of the rectangle's left edge gets the gap as spaces first.
	if rect, ok := d.BlockSelection(); ok && rect.Virtual {
		for i := range cs {
			cs[i].Pad = d.blockPad(rect, cs[i].Head.Line)
		}
	}
	return cs
}

// clampCaret returns c with its anchor and head clamped into the text.
func (d *Document) clampCaret(c caret) caret {
	c.Anchor = d.Buffer.Clamp(c.Anchor)
	c.Head = d.Buffer.Clamp(c.Head)
	return c
}

// setCarets makes cs the carets of d. The list is normalized first. The
// primary caret becomes Cursor and Sel, and the others become the extra carets.
// Each caret keeps its wanted column, and the block selection is reset, so a
// block step sets its block after it calls setCarets.
func (d *Document) SetCarets(cs []caret) {
	cs = normalize(cs)
	d.Want = -1
	d.virtual = 0
	d.virtualLines = 0
	d.Block = nil
	d.Extra = nil
	for _, c := range cs {
		if c.Primary {
			d.Cursor, d.Sel, d.Want = c.Head, c.Selection(), c.Want-1
			continue
		}
		d.Extra = append(d.Extra, Caret{Cursor: c.Head, Sel: c.Selection(), Want: c.Want})
	}
}

// hasAnySelection reports whether the primary caret or an extra caret has a
// selection.
func (d *Document) HasAnySelection() bool {
	if d.HasSelection() {
		return true
	}
	for _, x := range d.Extra {
		if x.Sel != nil {
			return true
		}
	}
	return false
}

// clearCarets drops the extra carets and the block selection. The primary
// caret and its selection stay.
func (d *Document) ClearCarets() {
	d.Extra = nil
	d.Block = nil
}

// edit replaces the text from from to to with text. An edit with from equal
// to to only inserts.
type edit struct {
	From, To buffer.Pos
	Text     string
}

// applyEdits applies edits to d.Buffer as one undo step. It returns, for each
// edit, the position just after the text the edit inserted, in the final text.
// The edits must be in document order and must not overlap. They are applied
// from the last to the first, so the positions of the earlier edits stay valid,
// and each recorded end moves past the edits applied after it.
func (d *Document) applyEdits(edits []edit) []buffer.Pos {
	ends := make([]buffer.Pos, len(edits))
	if d.PlanErr != nil {
		// The planning failed (see failPlan): no edit is made.
		return ends
	}
	d.Buffer.Group(func() {
		for i := len(edits) - 1; i >= 0; i-- {
			e := edits[i]
			if e.From != e.To {
				d.Buffer.Delete(e.From, e.To)
			}
			ends[i] = e.From
			if e.Text != "" {
				ends[i] = d.Buffer.Insert(e.From, e.Text)
			}
			for j := i + 1; j < len(edits); j++ {
				ends[j] = movePast(ends[j], e.To, ends[i])
			}
		}
	})
	return ends
}

// movePast maps p through an edit that replaced the text up to oldEnd with
// text ending at end. p must be at or after oldEnd, which holds for every end
// recorded after the edit.
func movePast(p, oldEnd, end buffer.Pos) buffer.Pos {
	if p.Line == oldEnd.Line {
		return buffer.Pos{Line: end.Line, Col: end.Col + p.Col - oldEnd.Col}
	}
	return buffer.Pos{Line: p.Line + end.Line - oldEnd.Line, Col: p.Col}
}

// multiEditRecord records the last multi-caret edit, so that undo and redo can put
// the primary caret back. The buffer's history keeps only the first and the last
// edit of an undo group, and those belong to the carets furthest down the text, so
// it cannot tell where the primary caret was. The record is valid while the
// buffer's newest undo group is the edit. Any other text change ends it: an action
// in textChanging (guardDocument), a replacement (writableActive), a shell replace
// (applyShell) or trimming on save (tidyForSave). Undo and redo keep it, and set
// undone while the edit is undone.
type multiEditRecord struct {
	Before, After buffer.Pos // the primary caret before the edit and after it
	Undone        bool
}

// noteMultiEdit records a multi-caret edit whose edits were applied, with before
// as the primary caret before them. Edits that change nothing record nothing.
// Call it after setCarets, so that d.Cursor is the caret after the edit.
func (d *Document) noteMultiEdit(before buffer.Pos, edits []edit) {
	for _, x := range edits {
		if x.From != x.To || x.Text != "" {
			d.LastMulti = &multiEditRecord{Before: before, After: d.Cursor}
			return
		}
	}
}

// placePrimary puts the primary caret at p with no selection. Undo and redo use
// it, as they always have.
func (d *Document) PlacePrimary(p buffer.Pos) {
	d.Cursor = d.Buffer.Clamp(p)
	d.Sel = nil
	d.Want = -1
}

// editAtCarets makes one edit per caret, as one undo step. plan returns the
// edit for a caret. It is called for every caret, in document order, before any
// edit is applied, so it sees the text as it was. Afterwards each caret sits just
// after the text its edit inserted, and carets that meet are merged.
func (d *Document) EditAtCarets(plan func(c caret) edit) {
	cs := d.AllCarets()
	before := d.Cursor
	edits := make([]edit, len(cs))
	for i, c := range cs {
		edits[i] = plan(c)
	}
	ends := d.applyEdits(edits)
	if d.editRefused() {
		return
	}
	out := make([]caret, len(cs))
	for i, c := range cs {
		out[i] = caret{Anchor: ends[i], Head: ends[i], Primary: c.Primary}
	}
	d.SetCarets(out)
	d.noteMultiEdit(before, edits)
}

// replaceEdit returns the edit that replaces a caret's selection with text. An
// empty caret inserts text at its position. A caret of a virtual block selection
// with a pad inserts the pad as spaces before the text, so the rectangle's left
// edge materializes in the same undo step.
func replaceEdit(c caret, text string) edit {
	if c.Pad > 0 {
		return edit{From: c.Head, To: c.Head, Text: strings.Repeat(" ", c.Pad) + text}
	}
	lo, hi := c.Bounds()
	return edit{From: lo, To: hi, Text: text}
}

// DeleteAtCarets removes each caret's selection, and inserts nothing, as one undo
// step. Unlike replaceEdit it never materializes a virtual block's padding: the
// gap holds no text to delete.
func (d *Document) DeleteAtCarets() {
	d.EditAtCarets(func(c caret) edit {
		lo, hi := c.Bounds()
		return edit{From: lo, To: hi}
	})
}

// caretLines returns the first and last line a caret covers: its own line, or
// the lines of its selection. A selection that ends at column 0 leaves its last
// line out, as selectedLines does for the primary caret.
func caretLines(c caret) (first, last int) {
	lo, hi := c.Bounds()
	last = hi.Line
	if hi.Col == 0 && hi.Line > lo.Line {
		last--
	}
	return lo.Line, last
}

// indentSelectedLines indents each line that a selection covers, once per line.
// Carets without a selection contribute no lines, but they move with their
// text. An anchor at column 0 of an indented line moves past the indent, as
// indentLines does, and a caret moves when it is past column 0.
func (d *Document) IndentSelectedLines(unit string) {
	cs := d.AllCarets()
	lines := make(map[int]bool)
	for _, c := range cs {
		if c.Anchor == c.Head {
			continue
		}
		first, last := caretLines(c)
		for line := first; line <= last; line++ {
			lines[line] = true
		}
	}
	edits := make([]edit, 0, len(lines))
	width := utf8.RuneCountInString(unit)
	for _, line := range slices.Sorted(maps.Keys(lines)) {
		p := buffer.Pos{Line: line}
		edits = append(edits, edit{From: p, To: p, Text: unit})
	}
	for i := range cs {
		c := &cs[i]
		selected := c.Anchor != c.Head
		c.Anchor = indentedPoint(c.Anchor, lines, width, selected)
		c.Want = 0
		c.Head = indentedPoint(c.Head, lines, width, false)
	}
	before := d.Cursor
	d.applyEdits(edits)
	d.SetCarets(cs)
	d.noteMultiEdit(before, edits)
}

// indentedPoint moves p past the indent that indentSelectedLines inserted at
// the start of each line in lines. A position at column 0 moves only when
// atStart is set.
func indentedPoint(p buffer.Pos, lines map[int]bool, width int, atStart bool) buffer.Pos {
	if lines[p.Line] && (p.Col > 0 || atStart) {
		p.Col += width
	}
	return p
}

// displayColumn returns the display column of rune column col in line, where a
// tab advances to the next multiple of tabSize and every other rune takes one
// column.
func displayColumn(line []rune, col, tabSize int) int {
	dc := 0
	for _, r := range line[:col] {
		if r == '\t' {
			dc += tabSize - dc%tabSize
		} else {
			dc++
		}
	}
	return dc
}

// columnAtDisplay returns the largest rune column of line whose display column
// is at most target. A target past the end of the line gives the line length.
// A target inside a tab gives the column before that tab.
func columnAtDisplay(line []rune, target, tabSize int) int {
	dc, col := 0, 0
	for col < len(line) {
		next := dc + 1
		if line[col] == '\t' {
			next = dc + tabSize - dc%tabSize
		}
		if next > target {
			break
		}
		dc, col = next, col+1
	}
	return col
}

// addCaretVertical adds a caret on the line above (dir -1) or below (dir +1) the
// primary caret, at the same display column, clamped to the line length. The new
// caret becomes the primary caret and the old primary becomes an extra caret, so
// repeated presses move outwards, as Fresh's add_cursor_above and
// add_cursor_below do.
func (d *Document) AddCaretVertical(dir, tabSize int) error {
	line := d.Cursor.Line + dir
	if line < 0 || line >= d.Buffer.Lines() {
		if dir < 0 {
			return errFirstLine
		}
		return errLastLine
	}
	col := displayColumn([]rune(d.Buffer.Line(d.Cursor.Line)), d.Cursor.Col, tabSize)
	p := buffer.Pos{Line: line, Col: columnAtDisplay([]rune(d.Buffer.Line(line)), col, tabSize)}
	cs := d.AllCarets()
	for i := range cs {
		cs[i].Primary = false
	}
	d.SetCarets(append(cs, caret{Anchor: p, Head: p, Primary: true}))
	return nil
}

// cursorsToLineEnds puts a caret at the end of every line that a caret covers:
// the line of a caret without a selection, or each line of a selection. The
// selections are cleared. The primary caret stays on the first line it covers.
// Carets that land on the same line end merge.
func (d *Document) CursorsToLineEnds() {
	if d.large != nil {
		n := 0
		for _, c := range d.AllCarets() {
			first, last := caretLines(c)
			n += last - first + 1
		}
		if n > maxLargeCarets {
			d.failPlan(errTooManyCarets())
			return
		}
	}
	var out []caret
	for _, c := range d.AllCarets() {
		first, last := caretLines(c)
		for line := first; line <= last; line++ {
			p := buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)}
			out = append(out, caret{Anchor: p, Head: p, Primary: c.Primary && line == first})
		}
	}
	d.SetCarets(out)
}
