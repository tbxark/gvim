package document

import (
	"errors"
	"github.com/tbxark/gvim/buffer"
	"slices"
	"strings"
)

// CursorMode says what ApplyEdits does with the carets.
type CursorMode uint8

const (
	// CursorMap moves every caret and selection through the edits. A position
	// before an edit, or at the start of a replaced range, stays; one inside a
	// replaced range goes after the new text; one at or after the end of an edit
	// (or at an insert point) moves with the text. Carets that meet merge.
	CursorMap CursorMode = iota
	// CursorKeep leaves every caret and selection where it was, clamped into the
	// new text.
	CursorKeep
	// CursorEnd puts the one primary caret at the end of the last edit.
	CursorEnd
)

// EditOptions are the options of ApplyEdits.
type EditOptions struct {
	Cursor CursorMode
	// Expect, when set, must equal the document's Version or the edits are
	// refused with ErrStale.
	Expect *Version
}

// ErrStale is returned by ApplyEdits when EditOptions.Expect differs from the
// version of the document.
var ErrStale = errors.New("document changed since the expected version")

// ErrOverlappingEdits is returned by ApplyEdits for edits whose ranges overlap.
var ErrOverlappingEdits = errors.New("overlapping edits")

// bulkEditMin is the number of edits above which ApplyEdits replaces the touched
// span of lines in one step, instead of making each edit through the buffer.
const bulkEditMin = 64

// applyEditList is ApplyEdits on a document. lenient applies overlapping edits one
// by one, as ApplyLSPEdits does, instead of refusing them.
func (d *Document) ApplyEditList(edits []LSPEdit, o EditOptions, lenient bool) error {
	if d.ReadOnly() {
		return ErrReadOnly
	}
	if err := d.RefuseEditWhileRestoring("apply edits"); err != nil {
		return err
	}
	if o.Expect != nil && *o.Expect != d.Version() {
		return ErrStale
	}
	if len(edits) == 0 {
		return nil
	}
	sorted := d.sortedEdits(edits)
	overlap := overlaps(sorted)
	if overlap && !lenient {
		return ErrOverlappingEdits
	}
	if d.large != nil && !d.Index().Complete {
		last := d.Buffer.Lines() - 1
		for _, ed := range sorted {
			if ed.To.Line >= last || ed.From.Line >= last {
				return ErrIndexing
			}
		}
	}
	// The carets are mapped from the positions the document has now.
	var carets []caret
	if o.Cursor == CursorMap || o.Cursor == CursorKeep {
		carets = d.AllCarets()
	}
	if d.large != nil {
		_ = d.TakeRefusal()
	}
	if !overlap && len(sorted) > bulkEditMin && d.large == nil {
		from, to, text, err := spanReplacement(d.Buffer, sorted)
		if err != nil {
			return err
		}
		d.Buffer.Group(func() {
			d.Buffer.Delete(from, to)
			d.Buffer.Insert(from, text)
		})
	} else {
		d.Buffer.Group(func() {
			for _, ed := range slices.Backward(sorted) {
				if ed.From != ed.To {
					d.Buffer.Delete(ed.From, ed.To)
				}
				d.Buffer.Insert(ed.From, ed.Text)
			}
		})
	}
	if d.large != nil {
		if err := d.TakeRefusal(); err != nil {
			return err
		}
	}
	d.afterEdits(sorted, carets, o.Cursor)
	return nil
}

// sortedEdits returns edits with both positions clamped and in order, sorted by
// start. Edits with the same start keep their list order.
func (d *Document) sortedEdits(edits []LSPEdit) []LSPEdit {
	out := slices.Clone(edits)
	for i, ed := range out {
		ed.From, ed.To = d.Buffer.Clamp(ed.From), d.Buffer.Clamp(ed.To)
		if ed.To.Less(ed.From) {
			ed.From, ed.To = ed.To, ed.From
		}
		out[i] = ed
	}
	slices.SortStableFunc(out, func(a, b LSPEdit) int {
		if c := comparePos(a.From, b.From); c != 0 {
			return c
		}
		return comparePos(a.To, b.To)
	})
	return out
}

// overlaps reports whether a sorted edit starts inside the range of the one
// before it.
func overlaps(sorted []LSPEdit) bool {
	for i := 1; i < len(sorted); i++ {
		if sorted[i].From.Less(sorted[i-1].To) {
			return true
		}
	}
	return false
}

// spanReplacement returns the first and last position of the lines the sorted,
// non-overlapping edits touch, and the text that replaces them.
func spanReplacement(b *buffer.Buffer, sorted []LSPEdit) (from, to buffer.Pos, text string, err error) {
	from = buffer.Pos{Line: sorted[0].From.Line}
	lastLine := sorted[len(sorted)-1].To.Line
	to = buffer.Pos{Line: lastLine, Col: b.LineLen(lastLine)}
	var sb strings.Builder
	cur := from
	for _, ed := range sorted {
		part, err := b.RangeChecked(cur, ed.From)
		if err != nil {
			return from, to, "", err
		}
		sb.WriteString(part)
		sb.WriteString(ed.Text)
		cur = ed.To
	}
	part, err := b.RangeChecked(cur, to)
	if err != nil {
		return from, to, "", err
	}
	sb.WriteString(part)
	return from, to, sb.String(), nil
}

// afterEdits sets the carets and the bookkeeping of a document whose text was
// just changed by the sorted edits. carets are those of the document before.
func (d *Document) afterEdits(sorted []LSPEdit, carets []caret, mode CursorMode) {
	switch mode {
	case CursorMap:
		for i, c := range carets {
			carets[i].Anchor, carets[i].Head = mapThrough(sorted, c.Anchor), mapThrough(sorted, c.Head)
		}
		d.SetCarets(carets)
		d.Cursor = d.Buffer.Clamp(d.Cursor)
	case CursorKeep:
		for i, c := range carets {
			carets[i] = d.clampCaret(c)
		}
		d.SetCarets(carets)
	case CursorEnd:
		d.ClearCarets()
		d.PlacePrimary(mapThrough(sorted, sorted[len(sorted)-1].To))
	default:
		d.Cursor = d.Buffer.Clamp(d.Cursor)
		d.Sel = nil
		d.ClearCarets()
	}
	d.Want = -1
	d.Block = nil
	d.LastMulti = nil
	d.FindRange = nil
	if d.preview && d.Modified() {
		d.preview = false
	}
}

// mapThrough returns where p goes when the sorted, non-overlapping edits are
// made. They are taken from the last to the first, as they are applied, so each
// edit sees p in a text where only the edits after it have been made.
func mapThrough(sorted []LSPEdit, p buffer.Pos) buffer.Pos {
	for _, ed := range slices.Backward(sorted) {
		end := advancePos(ed.From, ed.Text)
		switch {
		case !p.Less(ed.To):
			p = movePast(p, ed.To, end)
		case ed.From.Less(p):
			p = end
		}
	}
	return p
}

// advancePos returns the position after typing text at p.
func advancePos(p buffer.Pos, text string) buffer.Pos {
	n := strings.Count(text, "\n")
	if n == 0 {
		return buffer.Pos{Line: p.Line, Col: p.Col + len([]rune(text))}
	}
	last := text[strings.LastIndexByte(text, '\n')+1:]
	return buffer.Pos{Line: p.Line + n, Col: len([]rune(last))}
}

// placeCursor moves the caret to p, as SetCursor does without the jump record.
func (d *Document) PlaceCursor(p buffer.Pos, extend bool) {
	d.Want = -1
	d.virtual = 0
	p = d.Buffer.Clamp(p)
	snapped := snapToCluster(d.Buffer, p)
	if extend && snapped != p {
		anchor := d.Cursor
		if d.Sel != nil {
			anchor = d.Sel.Anchor
		}
		if anchor.Less(p) {
			snapped.Col = nextBoundary(d.Buffer, p.Line, snapped.Col)
		}
	}
	d.Move(snapped, extend)
	d.Seen = d.Cursor
}

// selectBetween selects anchor..head, as SelectRange does without the jump record.
func (d *Document) SelectBetween(anchor, head buffer.Pos) {
	d.SetRange(d.Buffer.Clamp(anchor), d.Buffer.Clamp(head))
	d.Seen = d.Cursor
}
