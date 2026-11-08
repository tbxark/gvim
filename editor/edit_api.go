package editor

import (
	"github.com/tbxark/gvim/buffer"
)

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
	return d.ApplyEditList(edits, o, false)
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
	d.PlaceCursor(p, extend)
	return nil
}

// SelectID is SelectRange for document id. It does not record a jump.
func (e *Editor) SelectID(id DocID, anchor, head buffer.Pos) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.SelectBetween(anchor, head)
	return nil
}

// SetLanguageID is SetLanguage for document id.
func (e *Editor) SetLanguageID(id DocID, name string) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	d.SetLanguage(name)
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
