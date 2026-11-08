package editor

import (
	"errors"
	"fmt"
)

// ErrNotOpen is returned by ApplyLSPEdits for a path that no open document has.
var ErrNotOpen = errors.New("document is not open")

// ApplyLSPEdits applies edits to the open document at path. The edits run from
// the end of the text to the start, so each one keeps the positions of the ones
// before it. All of them are one undo step. Positions past the text are clamped.
// The carets and selections go through the edits (CursorMap), so a format that adds
// or removes lines above the caret leaves it on its text. It returns ErrNotOpen when no open document has path, and ErrReadOnly
// for a read-only document, which is then left unchanged.
func (e *Editor) ApplyLSPEdits(path string, edits []LSPEdit) error {
	d := e.lspDocAt(path)
	if d == nil {
		return fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	return d.ApplyEditList(edits, EditOptions{Cursor: CursorMap}, true)
}

// lspDocAt returns the open document whose path is path, or nil.
func (e *Editor) lspDocAt(path string) *Document {
	for _, d := range e.docs {
		if d.Path == path {
			return d
		}
	}
	return nil
}
