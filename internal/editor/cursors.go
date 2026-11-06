package editor

import (
	"cmp"
	"errors"
	"maps"
	"slices"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/buffer"
)

// Multi-caret editing. A Document has one primary caret, Cursor and Sel, and
// any number of extra carets, which Cursors reports.
//
// Typing, Enter, Tab, Backspace, Delete, cut and paste apply at every caret, each
// as one undo step, and copy takes the text of every caret (see clip_ops.go). The
// add-caret actions, add_cursors_to_line_ends and the block selection actions create
// the carets. remove_secondary_cursors drops the extra carets. The motions,
// select_word and the paragraph jumps run at every caret (motion_carets.go). Every
// other action works on the primary caret alone and leaves the extra carets where
// they are, so their positions can go stale after a text change. Undo and redo drop
// the extra carets, because they cannot restore them.
// Carets that meet are merged, so no two carets overlap or sit at one point.

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
	want int
}

// Cursors returns the extra carets in document order. The primary caret is
// Document.Cursor and Document.Sel and is not in the list. The result is a
// copy, and its positions are clamped into the document. It returns nil when
// there is one caret. SetCursor, SelectRange and the other primary-only calls
// leave these carets in place; a UI that wants a click to clear them sends
// remove_secondary_cursors first.
func (d *Document) Cursors() []Caret {
	if len(d.extra) == 0 {
		return nil
	}
	out := make([]Caret, len(d.extra))
	for i, x := range d.extra {
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
	anchor, head buffer.Pos
	primary      bool
	// want is the wanted column of vertical moves, plus one. Zero is unset.
	want int
}

// bounds returns the caret's selection in document order. An empty caret has
// lo equal to hi.
func (c caret) bounds() (lo, hi buffer.Pos) {
	if c.head.Less(c.anchor) {
		return c.head, c.anchor
	}
	return c.anchor, c.head
}

// selection returns the caret's selection, or nil when it has none.
func (c caret) selection() *buffer.Selection {
	if c.anchor == c.head {
		return nil
	}
	return &buffer.Selection{Anchor: c.anchor, Head: c.head}
}

// meets reports whether c and o become one caret: their selections overlap, or
// they touch and one of them is empty. Two selections that only touch, such as
// [0,2) and [2,4), stay apart.
func (c caret) meets(o caret) bool {
	lo1, hi1 := c.bounds()
	lo2, hi2 := o.bounds()
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
		alo, _ := a.bounds()
		blo, _ := b.bounds()
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
	lo, hi := a.bounds()
	if _, h := b.bounds(); hi.Less(h) {
		hi = h
	}
	w := a
	if b.primary {
		w = b
	}
	m := caret{primary: a.primary || b.primary, want: w.want}
	if w.head.Less(w.anchor) {
		m.anchor, m.head = hi, lo
	} else {
		m.anchor, m.head = lo, hi
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
func (d *Document) allCarets() []caret {
	p := caret{anchor: d.Cursor, head: d.Cursor, primary: true, want: d.want + 1}
	if d.Sel != nil {
		p.anchor = d.Sel.Anchor
	}
	cs := make([]caret, 0, 1+len(d.extra))
	cs = append(cs, d.clampCaret(p))
	for _, x := range d.extra {
		c := caret{anchor: x.Cursor, head: x.Cursor, want: x.want}
		if x.Sel != nil {
			c.anchor = x.Sel.Anchor
		}
		cs = append(cs, d.clampCaret(c))
	}
	return normalize(cs)
}

// clampCaret returns c with its anchor and head clamped into the text.
func (d *Document) clampCaret(c caret) caret {
	c.anchor = d.Buffer.Clamp(c.anchor)
	c.head = d.Buffer.Clamp(c.head)
	return c
}

// setCarets makes cs the carets of d. The list is normalized first. The
// primary caret becomes Cursor and Sel, and the others become the extra carets.
// Each caret keeps its wanted column, and the block selection is reset, so a
// block step sets its block after it calls setCarets.
func (d *Document) setCarets(cs []caret) {
	cs = normalize(cs)
	d.want = -1
	d.block = nil
	d.extra = nil
	for _, c := range cs {
		if c.primary {
			d.Cursor, d.Sel, d.want = c.head, c.selection(), c.want-1
			continue
		}
		d.extra = append(d.extra, Caret{Cursor: c.head, Sel: c.selection(), want: c.want})
	}
}

// hasAnySelection reports whether the primary caret or an extra caret has a
// selection.
func (d *Document) hasAnySelection() bool {
	if d.HasSelection() {
		return true
	}
	for _, x := range d.extra {
		if x.Sel != nil {
			return true
		}
	}
	return false
}

// clearCarets drops the extra carets and the block selection. The primary
// caret and its selection stay.
func (d *Document) clearCarets() {
	d.extra = nil
	d.block = nil
}

// edit replaces the text from from to to with text. An edit with from equal
// to to only inserts.
type edit struct {
	from, to buffer.Pos
	text     string
}

// applyEdits applies edits to d.Buffer as one undo step. It returns, for each
// edit, the position just after the text the edit inserted, in the final text.
// The edits must be in document order and must not overlap. They are applied
// from the last to the first, so the positions of the earlier edits stay valid,
// and each recorded end moves past the edits applied after it.
func (d *Document) applyEdits(edits []edit) []buffer.Pos {
	ends := make([]buffer.Pos, len(edits))
	if d.planErr != nil {
		// The planning failed (see failPlan): no edit is made.
		return ends
	}
	d.Buffer.Group(func() {
		for i := len(edits) - 1; i >= 0; i-- {
			e := edits[i]
			if e.from != e.to {
				d.Buffer.Delete(e.from, e.to)
			}
			ends[i] = e.from
			if e.text != "" {
				ends[i] = d.Buffer.Insert(e.from, e.text)
			}
			for j := i + 1; j < len(edits); j++ {
				ends[j] = movePast(ends[j], e.to, ends[i])
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
	before, after buffer.Pos // the primary caret before the edit and after it
	undone        bool
}

// noteMultiEdit records a multi-caret edit whose edits were applied, with before
// as the primary caret before them. Edits that change nothing record nothing.
// Call it after setCarets, so that d.Cursor is the caret after the edit.
func (d *Document) noteMultiEdit(before buffer.Pos, edits []edit) {
	for _, x := range edits {
		if x.from != x.to || x.text != "" {
			d.lastMulti = &multiEditRecord{before: before, after: d.Cursor}
			return
		}
	}
}

// placePrimary puts the primary caret at p with no selection. Undo and redo use
// it, as they always have.
func (d *Document) placePrimary(p buffer.Pos) {
	d.Cursor = d.Buffer.Clamp(p)
	d.Sel = nil
	d.want = -1
}

// undoAction reverts the newest undo group. When that group is the last
// multi-caret edit, the primary caret goes back to where it was before the edit.
// Otherwise the caret goes to the first edit of the group, as it always has.
func undoAction(_ *Editor, d *Document, _ map[string]any) error {
	m := d.lastMulti
	if m == nil || m.undone {
		d.lastMulti = nil
		if pos, ok := d.Buffer.Undo(); ok {
			d.placePrimary(pos)
		}
		return nil
	}
	if _, ok := d.Buffer.Undo(); ok {
		m.undone = true
		d.placePrimary(m.before)
	}
	return nil
}

// redoAction reapplies the newest undone group. When that group is the last
// multi-caret edit, the primary caret goes to where it was after the edit.
// Otherwise the caret goes to the end of the group, as it always has.
func redoAction(_ *Editor, d *Document, _ map[string]any) error {
	m := d.lastMulti
	if m == nil || !m.undone {
		d.lastMulti = nil
		if pos, ok := d.Buffer.Redo(); ok {
			d.placePrimary(pos)
		}
		return nil
	}
	if _, ok := d.Buffer.Redo(); ok {
		m.undone = false
		d.placePrimary(m.after)
	}
	return nil
}

// editAtCarets makes one edit per caret, as one undo step. plan returns the
// edit for a caret. It is called for every caret, in document order, before any
// edit is applied, so it sees the text as it was. Afterwards each caret sits just
// after the text its edit inserted, and carets that meet are merged.
func (d *Document) editAtCarets(plan func(c caret) edit) {
	cs := d.allCarets()
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
		out[i] = caret{anchor: ends[i], head: ends[i], primary: c.primary}
	}
	d.setCarets(out)
	d.noteMultiEdit(before, edits)
}

// replaceEdit returns the edit that replaces a caret's selection with text. An
// empty caret inserts text at its position.
func replaceEdit(c caret, text string) edit {
	lo, hi := c.bounds()
	return edit{from: lo, to: hi, text: text}
}

// typeAtCarets types the text at every caret, replacing each selection. It is
// the multi-caret form of insertText, with the smart rules of typeText.
func typeAtCarets(e *Editor, d *Document, args map[string]any) error {
	text, ok := args["text"].(string)
	if !ok {
		return errors.New("insert_char: missing string argument \"text\"")
	}
	e.typeText(d, text)
	return nil
}

// newlineAtCarets breaks the line at every caret, replacing each selection, with
// the indent rules of newline.
func newlineAtCarets(e *Editor, d *Document, _ map[string]any) error {
	e.newline(d)
	return nil
}

// deleteBackwardAtCarets removes each selection, or the character before each
// empty caret, with the smart rules of backspace.
func deleteBackwardAtCarets(e *Editor, d *Document, _ map[string]any) error {
	e.backspace(d)
	return nil
}

// deleteForwardAtCarets removes each selection, or the character after each
// empty caret, which may be a line break.
func deleteForwardAtCarets(_ *Editor, d *Document, _ map[string]any) error {
	d.editAtCarets(func(c caret) edit {
		if lo, hi := c.bounds(); lo != hi {
			return edit{from: lo, to: hi}
		}
		if next, ok := nextPos(d.Buffer, c.head); ok {
			return edit{from: c.head, to: next}
		}
		return edit{from: c.head, to: c.head}
	})
	return nil
}

// tabAtCarets handles Tab at every caret. If any caret has a selection, every
// line that a selection covers is indented, as Tab does for the primary caret.
// Otherwise each empty caret gets one indent unit: tab_size spaces, or a tab
// character when UseTabs is set.
func tabAtCarets(e *Editor, d *Document, _ map[string]any) error {
	if d.hasAnySelection() {
		d.indentSelectedLines(e.indentUnit(d))
		return nil
	}
	unit := e.indentUnit(d)
	d.editAtCarets(func(c caret) edit { return replaceEdit(c, unit) })
	return nil
}

// caretLines returns the first and last line a caret covers: its own line, or
// the lines of its selection. A selection that ends at column 0 leaves its last
// line out, as selectedLines does for the primary caret.
func caretLines(c caret) (first, last int) {
	lo, hi := c.bounds()
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
func (d *Document) indentSelectedLines(unit string) {
	cs := d.allCarets()
	lines := make(map[int]bool)
	for _, c := range cs {
		if c.anchor == c.head {
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
		edits = append(edits, edit{from: p, to: p, text: unit})
	}
	for i := range cs {
		c := &cs[i]
		selected := c.anchor != c.head
		c.anchor = indentedPoint(c.anchor, lines, width, selected)
		c.want = 0
		c.head = indentedPoint(c.head, lines, width, false)
	}
	before := d.Cursor
	d.applyEdits(edits)
	d.setCarets(cs)
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
func (d *Document) addCaretVertical(dir, tabSize int) error {
	line := d.Cursor.Line + dir
	if line < 0 || line >= d.Buffer.Lines() {
		if dir < 0 {
			return errFirstLine
		}
		return errLastLine
	}
	col := displayColumn([]rune(d.Buffer.Line(d.Cursor.Line)), d.Cursor.Col, tabSize)
	p := buffer.Pos{Line: line, Col: columnAtDisplay([]rune(d.Buffer.Line(line)), col, tabSize)}
	cs := d.allCarets()
	for i := range cs {
		cs[i].primary = false
	}
	d.setCarets(append(cs, caret{anchor: p, head: p, primary: true}))
	return nil
}

// cursorsToLineEnds puts a caret at the end of every line that a caret covers:
// the line of a caret without a selection, or each line of a selection. The
// selections are cleared. The primary caret stays on the first line it covers.
// Carets that land on the same line end merge.
func (d *Document) cursorsToLineEnds() {
	if d.large != nil {
		n := 0
		for _, c := range d.allCarets() {
			first, last := caretLines(c)
			n += last - first + 1
		}
		if n > maxLargeCarets {
			d.failPlan(errTooManyCarets())
			return
		}
	}
	var out []caret
	for _, c := range d.allCarets() {
		first, last := caretLines(c)
		for line := first; line <= last; line++ {
			p := buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)}
			out = append(out, caret{anchor: p, head: p, primary: c.primary && line == first})
		}
	}
	d.setCarets(out)
}

// withCarets returns a handler that runs multi while the document has extra
// carets, and single otherwise. A single caret therefore behaves exactly as it
// did before the extra carets existed.
func withCarets(single handler, multi func(e *Editor, d *Document, args map[string]any) error) handler {
	return func(e *Editor, args map[string]any) error {
		if d := e.Active(); d != nil && len(d.extra) > 0 {
			return multi(e, d, args)
		}
		return single(e, args)
	}
}

// collapseCarets returns a handler that drops the extra carets, then runs next.
// Undo and redo use it, because they cannot put the extra carets back.
func collapseCarets(next handler) handler {
	return func(e *Editor, args map[string]any) error {
		if d := e.Active(); d != nil {
			d.clearCarets()
		}
		return next(e, args)
	}
}

// registerCursorHandlers binds the multi-caret actions. It runs last in
// buildHandlers, so it replaces the entries it names. The single-caret handlers
// it replaces for typing and deletion are kept inside withCarets, and run when
// there is one caret.
func registerCursorHandlers(h map[Action]handler) {
	h["insert_char"] = withCarets(h["insert_char"], typeAtCarets)
	h["insert_newline"] = withCarets(h["insert_newline"], newlineAtCarets)
	h["insert_tab"] = withCarets(h["insert_tab"], tabAtCarets)
	h["delete_backward"] = withCarets(h["delete_backward"], deleteBackwardAtCarets)
	h["delete_forward"] = withCarets(h["delete_forward"], deleteForwardAtCarets)
	registerMotionCaretHandlers(h)
	h["undo"] = collapseCarets(h["undo"])
	h["redo"] = collapseCarets(h["redo"])
	h["add_cursor_above"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		return d.addCaretVertical(-1, e.tabSizeOf(d))
	})
	h["add_cursor_below"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		return d.addCaretVertical(1, e.tabSizeOf(d))
	})
	h["add_cursor_next_match"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		return d.addNextMatch()
	})
	h["add_cursors_to_line_ends"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.cursorsToLineEnds()
		return nil
	})
	// Escape sends this action. It drops the extra carets, the selection and mark
	// mode, as Fresh does, and reports nothing.
	h["remove_secondary_cursors"] = func(e *Editor, _ map[string]any) error {
		if d := e.Active(); d != nil {
			d.clearCarets()
			d.Sel = nil
			delete(e.extras.marked, d)
		}
		return nil
	}
	h["block_select_up"] = blockHandler(-1, 0)
	h["block_select_down"] = blockHandler(1, 0)
	h["block_select_left"] = blockHandler(0, -1)
	h["block_select_right"] = blockHandler(0, 1)
}
