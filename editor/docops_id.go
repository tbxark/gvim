package editor

// More *ID methods for the plugin host (see edit_api.go): undo and redo, and scrolling to
// the caret, for any open document and not only the active one.

// Preview reports whether the document is in the preview tab, which the next preview
// replaces.
func (d *Document) Preview() bool { return d.preview }

// UndoID reverts the newest undo group of document id and reports whether there was one.
// The caret goes where Undo puts it. A read-only document returns ErrReadOnly, and one
// whose stashed edits are still being restored an error that wraps ErrRestoring.
func (e *Editor) UndoID(id DocID) (bool, error) {
	return e.historyID(id, "undo", undoAction)
}

// RedoID is UndoID for the newest undone group.
func (e *Editor) RedoID(id DocID) (bool, error) {
	return e.historyID(id, "redo", redoAction)
}

func (e *Editor) historyID(id DocID, what string, act func(*Editor, *Document, map[string]any) error) (bool, error) {
	d, _, err := e.docByID(id)
	if err != nil {
		return false, err
	}
	if d.ReadOnly() {
		return false, ErrReadOnly
	}
	if err := d.refuseEditWhileRestoring(what); err != nil {
		return false, err
	}
	before := d.Buffer.Version()
	if err := act(e, d, nil); err != nil {
		return false, err
	}
	return d.Buffer.Version() != before, nil
}

// RevealID scrolls document id so that its caret is in view, as a jump does: when the
// view has to move, the caret line goes to the middle of the pane. With center the caret
// line is put in the middle whether or not it was visible. The viewport size is the one
// SetViewport gave, so the result is meant for the active document.
func (e *Editor) RevealID(id DocID, center bool) error {
	d, _, err := e.docByID(id)
	if err != nil {
		return err
	}
	if center {
		e.recenter(d)
		d.seen = d.Cursor
		return nil
	}
	e.ensureVisibleForJump(d)
	return nil
}
