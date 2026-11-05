package editor

import (
	"errors"
	"fmt"
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
)

// Unit is the granularity of a selection drag: one character, one word, or
// one whole line.
type Unit int

const (
	// UnitChar selects the characters between the anchor and the pointer.
	UnitChar Unit = iota
	// UnitWord selects whole words, so the selection grows one word at a time.
	UnitWord
	// UnitLine selects whole lines, including their newlines.
	UnitLine
)

// ErrNoTab is returned by CloseTab when the index does not name an open tab.
var ErrNoTab = errors.New("no such tab")

// previewSuffix is appended to the tab label of a preview tab.
const previewSuffix = " (preview)"

// SetCursor moves the caret to p, which is clamped into the document. With
// extend set, the selection grows from its existing anchor, or from the old
// caret when there is no selection; otherwise any selection is cleared. The
// wanted column for the next vertical move becomes the new column.
//
// SetCursor does not scroll the viewport, so a caret placed off-screen stays
// off-screen until an action that moves the viewport runs.
func (e *Editor) SetCursor(p buffer.Pos, extend bool) {
	d := e.Active()
	if d == nil {
		return
	}
	d.want = -1
	d.move(d.Buffer.Clamp(p), extend)
}

// SelectRange selects the text between anchor and head, both clamped into the
// document, and puts the caret at head. When the two positions are equal the
// selection is cleared and only the caret moves. It does not scroll.
func (e *Editor) SelectRange(anchor, head buffer.Pos) {
	d := e.Active()
	if d == nil {
		return
	}
	anchor, head = d.Buffer.Clamp(anchor), d.Buffer.Clamp(head)
	d.want = -1
	d.Buffer.Break()
	d.Cursor = head
	d.Sel = nil
	if anchor != head {
		d.Sel = &buffer.Selection{Anchor: anchor, Head: head}
	}
}

// ScrollBy moves the viewport of the active document by rows lines. Positive
// values scroll down, negative values scroll up. Only Document.Top changes:
// the caret and selection stay where they are. The result is clamped to
// [0, max(lines-1, 0)].
func (e *Editor) ScrollBy(rows int) {
	if d := e.Active(); d != nil {
		d.Top = clampTop(d, d.Top+rows)
	}
}

// ScrollTo sets the first visible line of the active document to top. The
// value is clamped the same way as ScrollBy. The caret does not move.
func (e *Editor) ScrollTo(top int) {
	if d := e.Active(); d != nil {
		d.Top = clampTop(d, top)
	}
}

// clampTop keeps a viewport top inside the document.
func clampTop(d *Document, top int) int {
	return min(max(top, 0), max(d.Buffer.Lines()-1, 0))
}

// ActivateTab makes tab i the active tab. An index outside the open tabs is
// ignored. Unlike Do, it does not change the status message.
func (e *Editor) ActivateTab(i int) {
	if i < 0 || i >= len(e.docs) {
		return
	}
	e.switchTo(i)
}

// CloseTab closes tab i. A modified document is kept, and ErrUnsaved is
// returned, unless force is true. Closing the last tab leaves a new empty
// buffer in its place. The error is also written to the status message, as Do
// does for its own failures. An index outside the open tabs returns an error
// that wraps ErrNoTab.
func (e *Editor) CloseTab(i int, force bool) error {
	err := e.closeAt(i, force)
	if err != nil {
		e.setStatus(err.Error())
	}
	return err
}

// CloseNeedsConfirm reports whether CloseTab(i, false) would be refused because
// tab i has unsaved edits. A caller can ask the user before it calls CloseTab
// with force set. It changes no state. An index outside the open tabs returns
// an error wrapping ErrNoTab.
func (e *Editor) CloseNeedsConfirm(i int) (bool, error) {
	if i < 0 || i >= len(e.docs) {
		return false, fmt.Errorf("%w: %d", ErrNoTab, i)
	}
	return e.docs[i].Modified(), nil
}

// DocumentIndexByPath returns the index of the first open tab whose path is
// path, and true when there is one. The path is matched the same way SaveAs and
// RenamePath check for duplicates. An empty path never matches.
func (e *Editor) DocumentIndexByPath(path string) (int, bool) {
	i := e.pathIndex(path)
	return i, i >= 0
}

// closeAt removes tab i and keeps the active tab on the same document where it
// can. Closing the active tab activates the neighbouring tab and resets the
// typing state, as switching tabs does. Closing the last tab leaves the editor
// Empty.
func (e *Editor) closeAt(i int, force bool) error {
	if i < 0 || i >= len(e.docs) {
		return fmt.Errorf("%w: %d", ErrNoTab, i)
	}
	d := e.docs[i]
	if d.Modified() && !force {
		return fmt.Errorf("close %s: %w", d.Name(), ErrUnsaved)
	}
	e.docs = slices.Delete(e.docs, i, i+1)
	if len(e.docs) == 0 {
		e.active = 0
		e.lastKind = ""
		e.emptyStatus = ""
		return nil
	}
	switch {
	case i < e.active:
		// The active document is unchanged; only its index moves.
		e.active--
	case i == e.active:
		e.switchTo(min(e.active, len(e.docs)-1))
	}
	return nil
}

// previewIndex returns the index of the preview tab, or -1 when there is none.
// A preview tab that has been edited is made permanent first, so a preview
// that OpenPreview can replace never holds unsaved text.
func (e *Editor) previewIndex() int {
	for _, d := range e.docs {
		if d.preview && d.Modified() {
			d.preview = false
		}
	}
	return slices.IndexFunc(e.docs, func(d *Document) bool { return d.preview })
}

// pathIndex returns the index of the first tab opened from path, or -1. The
// empty path never matches, so untitled buffers are not confused with each other.
// Paths match when they name the same file (see sameFile).
func (e *Editor) pathIndex(path string) int {
	if path == "" {
		return -1
	}
	return slices.IndexFunc(e.docs, func(d *Document) bool { return sameFile(d.Path, path) })
}
