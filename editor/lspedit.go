package editor

import (
	"errors"
	"fmt"

	"github.com/tbxark/gvim/buffer"
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

// ApplyCompletionAtCarets replaces the identifier prefix before EVERY caret of the open
// document at path with text, in one atomic edit (one undo step). It is the accept path
// for a completion with multiple cursors, matching Fresh's insert_completion_text: each
// caret keeps its own prefix length, so the cursors stay in lock-step, the same candidate
// is inserted at all of them, and CursorMap puts every caret after its own inserted text.
// expect, when non-nil, must equal the document version or nothing is written (ErrStale).
// It returns the number of carets whose prefix was replaced (which is also the number of
// edits), and ErrNotOpen for a path no open document has. An extra caret with an empty
// prefix gets the text inserted, as Fresh does.
func (e *Editor) ApplyCompletionAtCarets(path, text, wordChars string, expect *Version) (int, error) {
	d := e.lspDocAt(path)
	if d == nil {
		return 0, fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	positions := make([]buffer.Pos, 0, 1+len(d.Cursors()))
	positions = append(positions, d.Cursor)
	for _, c := range d.Cursors() {
		positions = append(positions, c.Cursor)
	}
	edits := make([]LSPEdit, 0, len(positions))
	for _, p := range positions {
		p = d.Buffer.Clamp(p)
		line := []rune(d.Buffer.Line(p.Line))
		col := min(p.Col, len(line))
		start := col
		for start > 0 && IsWordRune(line[start-1], wordChars) {
			start--
		}
		edits = append(edits, LSPEdit{
			From: buffer.Pos{Line: p.Line, Col: start},
			To:   p,
			Text: text,
		})
	}
	if err := d.ApplyEditList(edits, EditOptions{Cursor: CursorMap, Expect: expect}, false); err != nil {
		return 0, err
	}
	return len(edits), nil
}
