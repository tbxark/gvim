package editor

import (
	"slices"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// registerTextCmdHandlers registers the text commands: sort, select by level,
// duplicate above, trim, final newline, and copy as plain text.
func registerTextCmdHandlers(h map[Action]handler) {
	h["sort_lines"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.sortLines()
		return nil
	})
	h["expand_selection"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.expandSelection()
		return nil
	})
	h["duplicate_line_above"] = docAction(func(_ *Editor, d *Document, _ map[string]any) error {
		d.duplicateLinesAbove()
		return nil
	})
	h["trim_trailing_whitespace"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if d.trimTrailing() {
			e.setStatus("Trimmed trailing whitespace")
		} else {
			e.setStatus("No trailing whitespace to remove")
		}
		return nil
	})
	h["ensure_final_newline"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		if d.ensureFinalLineBreak() {
			e.setStatus("Added final newline")
		} else {
			e.setStatus("File already ends with newline")
		}
		return nil
	})
	// copy_with_theme copies the plain text only. Fresh also copies the syntax
	// colours as rich text, and gvim has no rich-text clipboard, so that part is
	// not done.
	h["copy_with_theme"] = docAction(func(e *Editor, d *Document, _ map[string]any) error {
		text, err := d.copySelection()
		if err != nil {
			return err
		}
		e.clipboard = text
		e.setStatus("Copied as plain text")
		return nil
	})
}

// sortLines sorts the lines of the selection, or of the whole document when nothing
// is selected. A selection covers whole lines (see selectedLines). The order is the
// byte order of the text, so uppercase letters sort before lowercase. The empty
// line after a final break is not a line of the text, so it stays at the end. The
// sort is one undo step. The selection then covers the sorted lines. Text with
// fewer than two lines, or already in order, is left alone.
func (d *Document) sortLines() {
	first, last := 0, d.Buffer.Lines()-1
	if d.HasSelection() {
		first, last = d.selectedLines()
	} else if last > 0 && d.Buffer.LineLen(last) == 0 {
		last--
	}
	if last <= first {
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
	d.want = -1
	if d.HasSelection() {
		d.setRange(start, after)
		return
	}
	d.clampToText()
}

// expandSelection grows the selection one level per call. The levels are the word
// at the caret, the lines the selection touches, and the whole buffer. A level is
// taken only when it is larger than the selection, so a press that changes nothing
// means the buffer is already selected.
func (d *Document) expandSelection() {
	cur := buffer.Selection{Anchor: d.Cursor, Head: d.Cursor}
	if d.Sel != nil {
		cur = *d.Sel
	}
	from, to := cur.Ordered()
	var levels []buffer.Selection
	if wFrom, wTo, ok := d.wordAt(d.Cursor); ok {
		levels = append(levels, buffer.Selection{Anchor: wFrom, Head: wTo})
	}
	lineFrom, _ := buffer.LineBounds(d.Buffer, from.Line)
	_, lineTo := buffer.LineBounds(d.Buffer, to.Line)
	levels = append(levels,
		buffer.Selection{Anchor: lineFrom, Head: lineTo},
		buffer.Selection{Anchor: buffer.Pos{}, Head: d.Buffer.End()},
	)
	for _, lv := range levels {
		lvFrom, lvTo := lv.Ordered()
		if from.Less(lvFrom) || lvTo.Less(to) || (lvFrom == from && lvTo == to) {
			continue
		}
		d.setRange(lvFrom, lvTo)
		return
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

// duplicateLinesAbove copies the lines that selectedLines covers, as one block, above
// the first of them. The copy takes the place of the original: the caret and the
// selection keep their line numbers, so they now sit on the copy.
func (d *Document) duplicateLinesAbove() {
	first, last := d.selectedLines()
	block := make([]string, 0, last-first+1)
	for line := first; line <= last; line++ {
		block = append(block, d.Buffer.Line(line))
	}
	d.Buffer.Group(func() {
		d.Buffer.Insert(buffer.Pos{Line: first}, strings.Join(block, "\n")+"\n")
	})
	d.want = -1
}

// trimTrailing removes the trailing blanks of every line, as one undo step. It
// reports whether the text changed. The caret and selection are kept inside the
// text afterwards.
func (d *Document) trimTrailing() bool {
	before := d.Buffer.Text()
	d.Buffer.Group(d.trimTrailingWhitespace)
	if d.Buffer.Text() == before {
		return false
	}
	d.clampToText()
	return true
}

// ensureFinalLineBreak adds a final line break when the text lacks one, as one undo
// step. It reports whether the text changed.
func (d *Document) ensureFinalLineBreak() bool {
	before := d.Buffer.Text()
	d.Buffer.Group(d.ensureFinalNewline)
	return d.Buffer.Text() != before
}

// clampToText moves the caret and the selection back into the text after an edit
// that may have shortened it. A selection that becomes empty is cleared.
func (d *Document) clampToText() {
	if caret := d.Buffer.Clamp(d.Cursor); caret != d.Cursor {
		d.Cursor = caret
		d.want = -1
	}
	if d.Sel != nil {
		d.Sel.Anchor = d.Buffer.Clamp(d.Sel.Anchor)
		d.Sel.Head = d.Cursor
		if d.Sel.Anchor == d.Sel.Head {
			d.Sel = nil
		}
	}
}
