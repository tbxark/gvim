package editor

import "github.com/tbxark/gvim/internal/buffer"

// ExtendSelection updates the selection during a mouse drag, with the pointer
// now at p. The unit says what the selection grows by:
//
//   - UnitChar: the selection runs from its anchor to p, as SetCursor(p, true)
//     does.
//   - UnitWord: the selection covers whole words. Dragging forward keeps the
//     anchor at the start of the word the drag began on and moves the head to
//     the end of the word under p. Dragging backward keeps the anchor at the
//     end of that word and moves the head to the start of the word under p.
//   - UnitLine: as UnitWord, with whole lines in place of words.
//
// The drag's starting range is the current selection, so a double-click
// (SelectRange on the word) followed by ExtendSelection with UnitWord grows
// the selection a word at a time. With no selection, the caret is the start.
// The caret always follows the head, and the viewport does not scroll.
func (e *Editor) ExtendSelection(p buffer.Pos, u Unit) {
	d := e.Active()
	if d == nil {
		return
	}
	p = d.Buffer.Clamp(p)
	d.want = -1
	defer func() { d.seen = d.Cursor }()
	if u == UnitChar {
		d.move(p, true)
		return
	}
	d.Buffer.Break()

	from, to := d.Cursor, d.Cursor
	if d.Sel != nil {
		from, to = d.Sel.Ordered()
	}
	lo, hi := unitBounds(d.Buffer, p, u)
	anchor, head := from, hi
	if p.Less(from) {
		anchor, head = to, lo
	}
	d.Cursor = head
	d.Sel = nil
	if anchor != head {
		d.Sel = &buffer.Selection{Anchor: anchor, Head: head}
	}
}

// unitBounds returns the range of the unit that contains p. UnitLine uses the
// line of p, and every other value uses the word under p.
func unitBounds(b *buffer.Buffer, p buffer.Pos, u Unit) (start, end buffer.Pos) {
	if u == UnitLine {
		return buffer.LineBounds(b, p.Line)
	}
	return buffer.WordBounds(b, p)
}

// OpenPreview opens path in a preview tab, the single-click form of OpenFile.
// There is at most one preview tab: opening another file as a preview replaces
// it in the same tab position. Its label in Tabs carries the " (preview)"
// suffix. The first edit to the tab, or OpenFile on the same path, makes it a
// permanent tab.
//
// If path is already open in a tab, that tab is activated and nothing is
// loaded. A permanent tab is never turned into a preview. A file that opens in large
// file mode is not previewed: nothing is loaded, and the status says to open it.
//
// Like OpenFile, it returns an error wrapping ErrNotDecodable or the file error,
// and the same message is written to the status.
func (e *Editor) OpenPreview(path string) error {
	path, err := absPath(path)
	if err != nil {
		e.setError(err)
		return err
	}
	if i := e.pathIndex(path); i >= 0 {
		e.switchTo(i)
		e.setStatus("Opened " + e.docs[i].Name())
		return nil
	}
	if e.isLargePath(path) {
		// A preview loads on the update loop. A large file opens through LoadFunc
		// or OpenFile, which a click on it does not do.
		e.setStatus("Large file: open it to load")
		return nil
	}
	d, err := loadDocument(path, e.loadSpec())
	if err != nil {
		e.setError(err)
		return err
	}
	d.preview = true
	switch i := e.previewIndex(); {
	case i >= 0:
		e.releaseDoc(e.docs[i])
		e.setDoc(i, d)
		e.switchTo(i)
	case e.pristineUntitled():
		// Like OpenFile, the untouched startup buffer is replaced, not kept.
		e.releaseDoc(e.docs[0])
		e.setDoc(0, d)
		e.switchTo(0)
	default:
		e.addDoc(d)
		e.switchTo(len(e.docs) - 1)
	}
	e.setStatus(d.openStatus())
	return nil
}
