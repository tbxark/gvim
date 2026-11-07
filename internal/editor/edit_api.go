package editor

import (
	"errors"
	"slices"
	"strings"

	"github.com/tbxark/gvim/internal/buffer"
)

// The *ID methods act on any open document by its DocID, active or not, where
// the older methods act on the active one. They are what a plugin host calls.

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

	// cursorDrop keeps the primary caret, clamped, and drops the selection and
	// the other carets, as ApplyLSPEdits always did.
	cursorDrop CursorMode = 100
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

// ApplyEdits applies edits to the open document id as one undo step. Every
// range is in the coordinates of the text before any edit, so the edits may
// come in any order; edits that start at the same position insert in list
// order. Positions past the text are clamped. Edits that overlap are refused
// with ErrOverlappingEdits, and read-only documents with ErrReadOnly. The
// document is left unchanged in both cases. It works on any open tab, with or
// without a file. A large document refuses an edit its buffer cannot make, with
// an error that matches ErrLargeFile or one of the buffer's.
//
// Up to 64 edits are made one by one, from the end of the text to the start.
// More are made by cutting the lines from the first touched one to the last,
// applying the edits to that text in memory, and putting it back, so the cost
// does not grow with the number of edits times the size of the text.
func (e *Editor) ApplyEdits(id DocID, edits []LSPEdit, o EditOptions) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	return d.applyEditList(edits, o, false)
}

// applyEditList is ApplyEdits on a document. lenient applies overlapping edits one
// by one, as ApplyLSPEdits does, instead of refusing them.
func (d *Document) applyEditList(edits []LSPEdit, o EditOptions, lenient bool) error {
	if d.ReadOnly() {
		return ErrReadOnly
	}
	if err := d.refuseEditWhileRestoring("apply edits"); err != nil {
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
		carets = d.allCarets()
	}
	if d.large != nil {
		_ = d.takeRefusal()
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
		if err := d.takeRefusal(); err != nil {
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
			carets[i].anchor, carets[i].head = mapThrough(sorted, c.anchor), mapThrough(sorted, c.head)
		}
		d.setCarets(carets)
		d.Cursor = d.Buffer.Clamp(d.Cursor)
	case CursorKeep:
		for i, c := range carets {
			carets[i] = d.clampCaret(c)
		}
		d.setCarets(carets)
	case CursorEnd:
		d.clearCarets()
		d.placePrimary(mapThrough(sorted, sorted[len(sorted)-1].To))
	default:
		d.Cursor = d.Buffer.Clamp(d.Cursor)
		d.Sel = nil
		d.clearCarets()
	}
	d.want = -1
	d.block = nil
	d.lastMulti = nil
	d.findRange = nil
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

// Group runs fn so that every edit it makes to the document is one undo step.
// It nests, and does not merge with typing before or after it. It returns the
// error of fn, or the id's error when no document has it.
func (e *Editor) Group(id DocID, fn func() error) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.Buffer.Group(func() { err = fn() })
	return err
}

// SetCursorID is SetCursor for document id. It does not record a jump.
func (e *Editor) SetCursorID(id DocID, p buffer.Pos, extend bool) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.placeCursor(p, extend)
	return nil
}

// SelectID is SelectRange for document id. It does not record a jump.
func (e *Editor) SelectID(id DocID, anchor, head buffer.Pos) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.selectBetween(anchor, head)
	return nil
}

// SetLanguageID is SetLanguage for document id.
func (e *Editor) SetLanguageID(id DocID, name string) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.language = name
	return nil
}

// CloseID is CloseTab for the tab of document id. A modified document is kept,
// and ErrUnsaved returned, unless force is set.
func (e *Editor) CloseID(id DocID, force bool) error {
	_, i, err := e.docByID(id)
	if err != nil {
		return err
	}
	return e.CloseTab(i, force)
}

// ActivateID makes the tab of document id the active one.
func (e *Editor) ActivateID(id DocID) error {
	_, i, err := e.docByID(id)
	if err != nil {
		return err
	}
	e.ActivateTab(i)
	return nil
}

// placeCursor moves the caret to p, as SetCursor does without the jump record.
func (d *Document) placeCursor(p buffer.Pos, extend bool) {
	d.want = -1
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
	d.move(snapped, extend)
	d.seen = d.Cursor
}

// selectBetween selects anchor..head, as SelectRange does without the jump record.
func (d *Document) selectBetween(anchor, head buffer.Pos) {
	d.setRange(d.Buffer.Clamp(anchor), d.Buffer.Clamp(head))
	d.seen = d.Cursor
}
