package editor

import (
	"cmp"
	"maps"
	"slices"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// Line and text operations that act at every caret. Each one is a single undo step,
// and with one caret it behaves as it always did. The caret list comes from
// allCarets, so carets that meet are merged first.

// caretPlan is the change one caret makes in a multi-caret action.
type caretPlan struct {
	// edit is the change, or nil when the caret changes nothing and only moves with
	// the text around it.
	edit *edit
	// place gives the head from the end of the edit's inserted text, in the final
	// text. Nil puts the head at that end. anchor does the same for the anchor and
	// defaults to the head, which leaves no selection.
	place, anchor placeFn
}

// applyPlans makes the plans of cs as one undo step. cs and plans are parallel. A
// caret without an edit keeps its selection, moved with the edits before it. A caret
// whose edit or position falls inside another caret's edit is merged into that
// caret, which then is the primary caret if either was.
func (d *Document) applyPlans(cs []caret, plans []caretPlan) {
	type entry struct {
		e     edit
		caret int
		part  int // 0 for the edit, 1 and 2 for the anchor and the head of an unchanged caret
	}
	var entries []entry
	for i, p := range plans {
		if p.edit != nil {
			entries = append(entries, entry{*p.edit, i, 0})
			continue
		}
		entries = append(entries,
			entry{edit{from: cs[i].anchor, to: cs[i].anchor}, i, 1},
			entry{edit{from: cs[i].head, to: cs[i].head}, i, 2})
	}
	weight := func(x entry) int { return min(x.part, 1) ^ 1 } // marks first at one position
	slices.SortStableFunc(entries, func(a, b entry) int {
		return cmp.Or(comparePos(a.e.from, b.e.from), cmp.Compare(weight(a), weight(b)))
	})
	absorbed := make(map[int]int) // dropped caret -> the caret whose edit took it in
	var last *entry
	for i := range entries {
		x := &entries[i]
		if _, gone := absorbed[x.caret]; gone {
			continue
		}
		if last != nil && x.e.from.Less(last.e.to) {
			absorbed[x.caret] = last.caret
			continue
		}
		if x.part == 0 {
			last = x
		}
	}
	var live []entry
	var edits, real []edit
	for _, x := range entries {
		if _, gone := absorbed[x.caret]; gone {
			continue
		}
		live = append(live, x)
		edits = append(edits, x.e)
		if x.part == 0 {
			real = append(real, x.e)
		}
	}
	before := d.Cursor
	ends := d.applyEdits(edits)
	if d.editRefused() {
		return
	}
	at := make([]caret, len(cs))
	for i, x := range live {
		c := &at[x.caret]
		switch x.part {
		case 1:
			c.anchor = ends[i]
		case 2:
			c.head = ends[i]
		default:
			c.head = ends[i]
			if f := plans[x.caret].place; f != nil {
				c.head = f(ends[i])
			}
			c.anchor = c.head
			if f := plans[x.caret].anchor; f != nil {
				c.anchor = f(ends[i])
			}
		}
	}
	primary := make([]bool, len(cs))
	for i, c := range cs {
		primary[i] = primary[i] || c.primary
		if to, gone := absorbed[i]; gone {
			primary[to] = primary[to] || c.primary
		}
	}
	out := make([]caret, 0, len(cs))
	for i := range cs {
		if _, gone := absorbed[i]; !gone {
			at[i].primary = primary[i]
			out = append(out, at[i])
		}
	}
	d.setCarets(out)
	if len(cs) > 1 {
		d.noteMultiEdit(before, real)
	}
}

// lineBlock is a run of whole lines, first to last.
type lineBlock struct{ first, last int }

// caretBlocks returns the lines the carets cover as blocks in document order.
// Blocks that share a line are merged, and with touching set so are blocks that
// are next to each other.
func caretBlocks(cs []caret, touching bool) []lineBlock {
	var blocks []lineBlock
	for _, c := range cs {
		first, last := caretLines(c)
		blocks = append(blocks, lineBlock{first, last})
	}
	slices.SortFunc(blocks, func(a, b lineBlock) int { return cmp.Compare(a.first, b.first) })
	gap := 0
	if touching {
		gap = 1
	}
	out := blocks[:0]
	for _, b := range blocks {
		if n := len(out); n > 0 && b.first <= out[n-1].last+gap {
			out[n-1].last = max(out[n-1].last, b.last)
			continue
		}
		out = append(out, b)
	}
	return out
}

// blockIndex returns the index in blocks of the block that holds line, or -1.
func blockIndex(blocks []lineBlock, line int) int {
	return slices.IndexFunc(blocks, func(b lineBlock) bool { return b.first <= line && line <= b.last })
}

// blockText returns the lines of b joined with line breaks. In a large document a
// block that is over the size it hands out, or holds a line that is cut short, has no
// text: the edit being planned fails (see failPlan).
func (d *Document) blockText(b lineBlock) string {
	if err := d.checkLines(b.first, b.last, "copied"); err != nil {
		d.failPlan(err)
		return ""
	}
	lines := make([]string, 0, b.last-b.first+1)
	for line := b.first; line <= b.last; line++ {
		lines = append(lines, d.Buffer.Line(line))
	}
	return strings.Join(lines, "\n")
}

// deleteLine removes the line of every caret with its line break. On the last line
// the text goes but the break before it stays, so the text ends with an empty line.
// Selections are dropped, and their text is not deleted. Each caret moves to column
// 0 of the line that takes its line's place.
func (d *Document) deleteLine() {
	cs := d.allCarets()
	var lines []int
	primary := -1
	for _, c := range cs {
		lines = append(lines, c.head.Line)
		if c.primary {
			primary = c.head.Line
		}
	}
	slices.Sort(lines)
	lines = slices.Compact(lines)
	edits := make([]edit, len(lines))
	for i, line := range lines {
		edits[i] = edit{from: buffer.Pos{Line: line}, to: buffer.Pos{Line: line + 1}}
		if line+1 >= d.Buffer.Lines() {
			edits[i].to = buffer.Pos{Line: line, Col: d.Buffer.LineLen(line)}
		}
	}
	before := d.Cursor
	d.applyEdits(edits)
	out := make([]caret, len(lines))
	for i, line := range lines {
		p := d.Buffer.Clamp(buffer.Pos{Line: line - i})
		out[i] = caret{anchor: p, head: p, primary: line == primary}
	}
	d.setCarets(out)
	if len(cs) > 1 {
		d.noteMultiEdit(before, edits)
	}
}

// duplicateLines copies the lines each caret covers (see caretLines) onto the line
// below them, or above them when above is set. The carets and selections move with
// the copy below, and stay on the line numbers they had above, which now hold the
// copy. On the last line, the copy below is joined with a new line break. Carets on
// shared lines duplicate their lines once.
func (d *Document) duplicateLines(above bool) {
	cs := d.allCarets()
	blocks := caretBlocks(cs, false)
	edits := make([]edit, len(blocks))
	before := make([]int, len(blocks)) // lines the earlier blocks inserted
	added := 0
	for i, b := range blocks {
		text := d.blockText(b)
		switch {
		case above:
			edits[i] = edit{from: buffer.Pos{Line: b.first}, to: buffer.Pos{Line: b.first}, text: text + "\n"}
		case b.last+1 < d.Buffer.Lines():
			edits[i] = edit{from: buffer.Pos{Line: b.last + 1}, to: buffer.Pos{Line: b.last + 1}, text: text + "\n"}
		default:
			end := buffer.Pos{Line: b.last, Col: d.Buffer.LineLen(b.last)}
			edits[i] = edit{from: end, to: end, text: "\n" + text}
		}
		before[i] = added
		added += b.last - b.first + 1
	}
	first := d.Cursor
	d.applyEdits(edits)
	out := make([]caret, len(cs))
	for i, c := range cs {
		k := blockIndex(blocks, min(c.anchor.Line, c.head.Line))
		shift := before[k]
		if !above {
			shift += blocks[k].last - blocks[k].first + 1
		}
		c.anchor.Line += shift
		c.head.Line += shift
		out[i] = c
	}
	d.setCarets(out)
	if len(cs) > 1 {
		d.noteMultiEdit(first, edits)
	}
}

// moveLines moves the lines each caret covers one line up (dir -1) or down (dir 1),
// past the neighbouring line, as one undo step. Blocks of lines that touch move as
// one, and a block that has no neighbour to swap with stays. Moving down never
// passes the empty line after a final line break. Every caret and selection
// moves with its text.
func (d *Document) moveLines(dir int) {
	cs := d.allCarets()
	lines := d.Buffer.Lines()
	blocks := caretBlocks(cs, true)
	phantom := lines > 1 && d.Buffer.LineLen(lines-1) == 0
	moves := make([]bool, len(blocks))
	var edits []edit
	for i, b := range blocks {
		if dir < 0 && b.first == 0 || dir > 0 && (b.last+1 >= lines || b.last+1 == lines-1 && phantom) {
			continue
		}
		moves[i] = true
		block := d.blockText(b)
		if dir < 0 {
			end := buffer.Pos{Line: b.last, Col: d.Buffer.LineLen(b.last)}
			edits = append(edits, edit{from: buffer.Pos{Line: b.first - 1}, to: end, text: block + "\n" + d.blockText(lineBlock{b.first - 1, b.first - 1})})
		} else {
			end := buffer.Pos{Line: b.last + 1, Col: d.Buffer.LineLen(b.last + 1)}
			edits = append(edits, edit{from: buffer.Pos{Line: b.first}, to: end, text: d.blockText(lineBlock{b.last + 1, b.last + 1}) + "\n" + block})
		}
	}
	if len(edits) == 0 {
		return
	}
	// shifted gives where a position goes. exclusive marks the end of a selection
	// that stops at column 0 of the line after its block: it follows its block.
	shifted := func(p buffer.Pos, own int, exclusive bool) buffer.Pos {
		if exclusive {
			if own >= 0 && moves[own] {
				p.Line += dir
			}
			return p
		}
		for i, b := range blocks {
			switch {
			case !moves[i]:
			case b.first <= p.Line && p.Line <= b.last:
				p.Line += dir
				return p
			case dir < 0 && p.Line == b.first-1:
				p.Line = b.last
				return p
			case dir > 0 && p.Line == b.last+1:
				p.Line = b.first
				return p
			}
		}
		return p
	}
	before := d.Cursor
	d.applyEdits(edits)
	out := make([]caret, len(cs))
	for i, c := range cs {
		lo, hi := c.bounds()
		own := blockIndex(blocks, lo.Line)
		exclusive := lo != hi && hi.Col == 0 && hi.Line > lo.Line
		anchor, head := c.anchor, c.head
		anchorHi, headHi := exclusive && anchor == hi, exclusive && head == hi
		c.anchor = d.Buffer.Clamp(shifted(anchor, own, anchorHi))
		c.head = d.Buffer.Clamp(shifted(head, own, headHi))
		out[i] = c
	}
	d.setCarets(out)
	if len(cs) > 1 {
		d.noteMultiEdit(before, edits)
	}
}

// dedent removes one indent unit from the start of each selected line, and of the
// line of every caret without a selection. A unit is one leading tab, or up to
// tabSize leading spaces. A position moves back by what was removed before it on
// its line.
func (d *Document) dedent(tabSize int) {
	cs := d.allCarets()
	widths := make(map[int]int)
	for _, c := range cs {
		first, last := caretLines(c)
		for line := first; line <= last; line++ {
			if w := indentWidth(d.Buffer.Line(line), tabSize); w > 0 {
				widths[line] = w
			}
		}
	}
	if len(widths) == 0 {
		return
	}
	edits := make([]edit, 0, len(widths))
	for _, line := range slices.Sorted(maps.Keys(widths)) {
		edits = append(edits, edit{from: buffer.Pos{Line: line}, to: buffer.Pos{Line: line, Col: widths[line]}})
	}
	back := func(p buffer.Pos) buffer.Pos {
		p.Col = max(p.Col-widths[p.Line], 0)
		return p
	}
	before := d.Cursor
	d.applyEdits(edits)
	for i := range cs {
		cs[i].anchor, cs[i].head = back(cs[i].anchor), back(cs[i].head)
	}
	d.setCarets(cs)
	if len(cs) > 1 {
		d.noteMultiEdit(before, edits)
	}
}

// deleteByRange deletes, at every caret, its selection, or else the range that
// target gives for the caret. A caret whose range is empty changes nothing.
func (d *Document) deleteByRange(target func(c caret) (from, to buffer.Pos)) {
	cs := d.allCarets()
	plans := make([]caretPlan, len(cs))
	for i, c := range cs {
		from, to := c.bounds()
		if from == to {
			from, to = target(c)
		}
		from, to = d.Buffer.Clamp(from), d.Buffer.Clamp(to)
		if from != to {
			plans[i].edit = &edit{from: from, to: to}
		}
	}
	d.applyPlans(cs, plans)
}

// deleteWord deletes the selection, or the word before (back) or after each caret.
func (d *Document) deleteWord(back bool) {
	d.deleteByRange(func(c caret) (buffer.Pos, buffer.Pos) {
		if back {
			return wordLeft(d.Buffer, c.head), c.head
		}
		return c.head, wordRight(d.Buffer, c.head)
	})
}

// deleteToLineStart deletes the selection, or the text from the start of the line to
// each caret. A caret at column 0 deletes the line break before it.
func (d *Document) deleteToLineStart() {
	d.deleteByRange(func(c caret) (buffer.Pos, buffer.Pos) {
		if c.head.Col == 0 {
			prev, _ := prevPos(d.Buffer, c.head)
			return prev, c.head
		}
		return buffer.Pos{Line: c.head.Line}, c.head
	})
}

// deleteToLineEnd deletes the selection, or the text from each caret to the end of
// its line. A caret at the end of the line deletes the line break after it.
func (d *Document) deleteToLineEnd() {
	d.deleteByRange(func(c caret) (buffer.Pos, buffer.Pos) {
		end := buffer.Pos{Line: c.head.Line, Col: d.Buffer.LineLen(c.head.Line)}
		if c.head == end {
			next, _ := nextPos(d.Buffer, c.head)
			return c.head, next
		}
		return c.head, end
	})
}

// transposeChars swaps the character before each caret with the one at it, and
// moves the caret past the pair. At the start or end of a line there is no
// character on both sides, so nothing changes. Line breaks are never swapped.
// A selection is dropped when its caret swaps.
func (d *Document) transposeChars() {
	cs := d.allCarets()
	plans := make([]caretPlan, len(cs))
	for i, c := range cs {
		p := c.head
		if p.Col == 0 || p.Col >= d.Buffer.LineLen(p.Line) {
			continue
		}
		from := buffer.Pos{Line: p.Line, Col: p.Col - 1}
		to := buffer.Pos{Line: p.Line, Col: p.Col + 1}
		pair := []rune(d.Buffer.Range(from, to))
		plans[i].edit = &edit{from: from, to: to, text: string(pair[1]) + string(pair[0])}
	}
	d.applyPlans(cs, plans)
}

// transformCase converts the selection of each caret, or else the word at or just
// before it, with fn. The caret ends after the converted text and its selection is
// dropped. Text that does not change leaves the caret and selection alone.
func (d *Document) transformCase(fn func(string) string) {
	cs := d.allCarets()
	plans := make([]caretPlan, len(cs))
	for i, c := range cs {
		from, to, ok := d.caseRange(c)
		if !ok {
			continue
		}
		old := d.planRange(from, to)
		if converted := fn(old); converted != old {
			plans[i].edit = &edit{from: from, to: to, text: converted}
		}
	}
	d.applyPlans(cs, plans)
}

// caseRange returns the range that a case conversion changes for c: its selection,
// or the word it is in or directly after. ok is false when there is neither.
func (d *Document) caseRange(c caret) (from, to buffer.Pos, ok bool) {
	if lo, hi := c.bounds(); lo != hi {
		return lo, hi, true
	}
	return d.wordAt(c.head)
}

// sortLinesAtCarets sorts the lines of every selection, in byte order. Selections
// that share lines sort together. Each sorted selection then covers its sorted lines.
func (d *Document) sortLinesAtCarets() {
	cs := d.allCarets()
	var withSel []caret
	for _, c := range cs {
		if c.anchor != c.head {
			withSel = append(withSel, c)
		}
	}
	blocks := caretBlocks(withSel, false)
	var edits []edit
	var sorted []lineBlock
	for _, b := range blocks {
		if b.last <= b.first {
			continue
		}
		lines := strings.Split(d.blockText(b), "\n")
		order := slices.Clone(lines)
		slices.Sort(order)
		if slices.Equal(lines, order) {
			continue
		}
		edits = append(edits, edit{
			from: buffer.Pos{Line: b.first},
			to:   buffer.Pos{Line: b.last, Col: d.Buffer.LineLen(b.last)},
			text: strings.Join(order, "\n"),
		})
		sorted = append(sorted, b)
	}
	if len(edits) == 0 {
		return
	}
	before := d.Cursor
	d.applyEdits(edits)
	for i, c := range cs {
		if c.anchor == c.head {
			continue
		}
		first, _ := caretLines(c)
		if k := blockIndex(sorted, first); k >= 0 {
			b := sorted[k]
			cs[i].anchor = buffer.Pos{Line: b.first}
			cs[i].head = buffer.Pos{Line: b.last, Col: d.Buffer.LineLen(b.last)}
		}
	}
	d.setCarets(cs)
	d.noteMultiEdit(before, edits)
}
