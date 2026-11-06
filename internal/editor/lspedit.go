package editor

import (
	"errors"
	"fmt"
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
)

// LSPEdit replaces the text from From up to To with Text. Both positions are
// rune positions, 0-based, as buffer.Pos counts them. From equal to To inserts.
type LSPEdit struct {
	From, To buffer.Pos
	Text     string
}

// ErrNotOpen is returned by ApplyLSPEdits for a path that no open document has.
var ErrNotOpen = errors.New("document is not open")

// ApplyLSPEdits applies edits to the open document at path. The edits run from
// the end of the text to the start, so each one keeps the positions of the ones
// before it. All of them are one undo step. Positions past the text are clamped.
// The caret is clamped into the new text, and any selection or extra caret is
// dropped. It returns ErrNotOpen when no open document has path, and ErrReadOnly
// for a read-only document, which is then left unchanged.
func (e *Editor) ApplyLSPEdits(path string, edits []LSPEdit) error {
	d := e.lspDocAt(path)
	if d == nil {
		return fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	if d.ReadOnly() {
		return ErrReadOnly
	}
	if len(edits) == 0 {
		return nil
	}
	ordered := slices.Clone(edits)
	slices.SortStableFunc(ordered, func(a, b LSPEdit) int {
		switch {
		case a.From.Less(b.From):
			return 1
		case b.From.Less(a.From):
			return -1
		}
		return 0
	})
	d.Buffer.Group(func() {
		for _, ed := range ordered {
			from, to := d.Buffer.Clamp(ed.From), d.Buffer.Clamp(ed.To)
			if to.Less(from) {
				from, to = to, from
			}
			d.Buffer.Delete(from, to)
			d.Buffer.Insert(from, ed.Text)
		}
	})
	d.Cursor = d.Buffer.Clamp(d.Cursor)
	d.Sel = nil
	d.extra = nil
	d.block = nil
	d.want = -1
	return nil
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
