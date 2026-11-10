package document

import (
	"github.com/tbxark/gvim/buffer"
	"slices"
	"strings"
)

// sortLines sorts the lines of the selection, or of the whole document when nothing
// is selected. A selection covers whole lines (see selectedLines). The order is the
// byte order of the text, so uppercase letters sort before lowercase. The empty
// line after a final break is not a line of the text, so it stays at the end. The
// sort is one undo step. The selection then covers the sorted lines. Text with
// fewer than two lines, or already in order, is left alone.
func (d *Document) SortLines() {
	if len(d.Extra) > 0 && d.HasAnySelection() {
		d.sortLinesAtCarets()
		return
	}
	first, last := 0, d.Buffer.Lines()-1
	if d.HasSelection() {
		first, last = d.selectedLines()
	} else if last > 0 && d.Buffer.LineLen(last) == 0 {
		last--
	}
	if last <= first {
		return
	}
	if err := d.checkLines(first, last, "sorted"); err != nil {
		d.failPlan(err)
		return
	}
	lines := make([]string, 0, last-first+1)
	for line := first; line <= last; line++ {
		lines = append(lines, d.Buffer.Line(line))
	}
	sorted := slices.Clone(lines)
	slices.Sort(sorted)
	if slices.Equal(lines, sorted) {
		return
	}
	start := buffer.Pos{Line: first}
	end := buffer.Pos{Line: last, Col: d.Buffer.LineLen(last)}
	var after buffer.Pos
	d.Buffer.Group(func() {
		d.Buffer.Delete(start, end)
		after = d.Buffer.Insert(start, strings.Join(sorted, "\n"))
	})
	d.Want = -1
	if d.HasSelection() {
		d.SetRange(start, after)
		return
	}
	d.ClampToText()
}

// expandSelection grows the selection one level per call. The levels are the word
// at the caret, the lines the selection touches, and the whole buffer. A level is
// taken only when it is larger than the selection, so a press that changes nothing
// means the buffer is already selected.
// enclosingBrackets finds enclosing pairs (), [], {}, "", '', `` on line.
func enclosingBrackets(line string, col int) (inFrom, inTo, outFrom, outTo int, ok bool) {
	pairs := [][2]byte{
		{'\'', '\''},
		{'"', '"'},
		{'`', '`'},
		{'(', ')'},
		{'[', ']'},
		{'{', '}'},
	}
	n := len(line)
	bestDist := n + 1
	var bInFrom, bInTo, bOutFrom, bOutTo int
	found := false

	for _, p := range pairs {
		open, close := p[0], p[1]
		start := -1
		for i := col - 1; i >= 0; i-- {
			if line[i] == open {
				start = i
				break
			}
		}
		if start < 0 {
			continue
		}
		end := -1
		for i := col; i < n; i++ {
			if line[i] == close {
				end = i
				break
			}
		}
		if end < 0 || end <= start {
			continue
		}
		dist := end - start
		if dist < bestDist {
			bestDist = dist
			bInFrom = start + 1
			bInTo = end
			bOutFrom = start
			bOutTo = end + 1
			found = true
		}
	}
	return bInFrom, bInTo, bOutFrom, bOutTo, found
}

func (d *Document) ExpandSelection() {
	cur := buffer.Selection{Anchor: d.Cursor, Head: d.Cursor}
	if d.Sel != nil {
		cur = *d.Sel
	}
	from, to := cur.Ordered()
	var levels []buffer.Selection
	if wFrom, wTo, ok := d.wordAt(d.Cursor); ok {
		levels = append(levels, buffer.Selection{Anchor: wFrom, Head: wTo})
	}
	lineText := d.Buffer.Line(from.Line)
	if inFrom, inTo, outFrom, outTo, ok := enclosingBrackets(lineText, from.Col); ok {
		if inFrom < inTo {
			levels = append(levels, buffer.Selection{
				Anchor: buffer.Pos{Line: from.Line, Col: inFrom},
				Head:   buffer.Pos{Line: from.Line, Col: inTo},
			})
		}
		levels = append(levels, buffer.Selection{
			Anchor: buffer.Pos{Line: from.Line, Col: outFrom},
			Head:   buffer.Pos{Line: from.Line, Col: outTo},
		})
	}
	lineFrom, _ := buffer.LineBounds(d.Buffer, from.Line)
	_, lineTo := buffer.LineBounds(d.Buffer, to.Line)
	levels = append(levels, buffer.Selection{Anchor: lineFrom, Head: lineTo})
	if d.large == nil || d.Index().Complete {
		levels = append(levels, buffer.Selection{Anchor: buffer.Pos{}, Head: d.Buffer.End()})
	}
	for _, lv := range levels {
		lvFrom, lvTo := lv.Ordered()
		if from.Less(lvFrom) || lvTo.Less(to) || (lvFrom == from && lvTo == to) {
			continue
		}
		if d.Sel != nil {
			d.selHistory = append(d.selHistory, *d.Sel)
		} else {
			d.selHistory = append(d.selHistory, buffer.Selection{Anchor: d.Cursor, Head: d.Cursor})
		}
		d.SetRange(lvFrom, lvTo)
		return
	}
}

// ShrinkSelection restores the narrower selection from before the last ExpandSelection.
func (d *Document) ShrinkSelection() {
	if len(d.selHistory) == 0 {
		return
	}
	prev := d.selHistory[len(d.selHistory)-1]
	d.selHistory = d.selHistory[:len(d.selHistory)-1]
	if prev.Anchor == prev.Head {
		d.Cursor = prev.Head
		d.Sel = nil
	} else {
		from, to := prev.Ordered()
		d.SetRange(from, to)
	}
}

// wordAt returns the word that p is in, or the word that ends just before p. ok is
// false when there is no word at either place.
func (d *Document) wordAt(p buffer.Pos) (from, to buffer.Pos, ok bool) {
	if d.Buffer.ClassAt(p) != buffer.ClassWord {
		prev, has := prevPos(d.Buffer, p)
		if !has || d.Buffer.ClassAt(prev) != buffer.ClassWord {
			return from, to, false
		}
		p = prev
	}
	from, to = buffer.WordBounds(d.Buffer, p)
	return from, to, true
}

// trimTrailing removes the trailing blanks of every line, as one undo step. It
// reports whether the text changed. The caret and selection are kept inside the
// text afterwards.
func (d *Document) TrimTrailing() bool {
	before := d.ContentMark()
	d.Buffer.Group(d.trimTrailingWhitespace)
	if d.ContentMark() == before {
		return false
	}
	d.ClampToText()
	return true
}

// ensureFinalLineBreak adds a final line break when the text lacks one, as one undo
// step. It reports whether the text changed.
func (d *Document) EnsureFinalLineBreak() bool {
	before := d.ContentMark()
	d.Buffer.Group(d.ensureFinalNewline)
	return d.ContentMark() != before
}

// clampToText moves the caret and the selection back into the text after an edit
// that may have shortened it. A selection that becomes empty is cleared.
func (d *Document) ClampToText() {
	if caret := d.Buffer.Clamp(d.Cursor); caret != d.Cursor {
		d.Cursor = caret
		d.Want = -1
	}
	if d.Sel != nil {
		d.Sel.Anchor = d.Buffer.Clamp(d.Sel.Anchor)
		d.Sel.Head = d.Cursor
		if d.Sel.Anchor == d.Sel.Head {
			d.Sel = nil
		}
	}
}
