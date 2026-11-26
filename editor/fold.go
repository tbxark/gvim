package editor

import (
	"cmp"
	"github.com/tbxark/gvim/document"
	"slices"

	"github.com/tbxark/gvim/fold"
)

// toggleFold collapses the fold that starts on the caret line, or expands it when
// it is collapsed already. A collapsed start that no longer begins a fold, because
// the text above it changed, can still be expanded.
func toggleFold(e *Editor, d *document.Document, _ map[string]any) error {
	return e.toggleFoldAt(d, d.Cursor.Line)
}

// toggleFoldAt is toggleFold for the fold that starts at line.
func (e *Editor) toggleFoldAt(d *document.Document, line int) error {
	if d.LargeState() != nil {
		// A large document has no folds, so "no fold starts on this line" would be
		// untrue of a line that looks like it starts one.
		return &document.LargeError{Msg: "folds are not available in large file mode", Errs: []error{document.ErrLargeFile}}
	}
	if d.FoldedLines()[line] {
		delete(d.FoldedLines(), line)
		return nil
	}
	ranges := d.Folds(e.tabSizeOf(d))
	if _, ok := slices.BinarySearchFunc(ranges, line, func(r fold.Range, target int) int {
		return cmp.Compare(r.Start, target)
	}); ok {
		if d.FoldedLines() == nil {
			d.SetFoldedLines(map[int]bool{})
		}
		d.FoldedLines()[line] = true
		return nil
	}
	return document.ErrNoFold
}

// foldAll collapses every fold in the active document.
func foldAll(e *Editor, d *document.Document, _ map[string]any) error {
	if d.LargeState() != nil {
		return &document.LargeError{Msg: "folds are not available in large file mode", Errs: []error{document.ErrLargeFile}}
	}
	folds := d.Folds(e.tabSizeOf(d))
	if len(folds) == 0 {
		return nil
	}
	m := make(map[int]bool, len(folds))
	for _, r := range folds {
		m[r.Start] = true
	}
	d.SetFoldedLines(m)
	return nil
}

// unfoldAll expands every fold in the active document.
func unfoldAll(e *Editor, d *document.Document, _ map[string]any) error {
	if d.LargeState() != nil {
		return &document.LargeError{Msg: "folds are not available in large file mode", Errs: []error{document.ErrLargeFile}}
	}
	d.SetFoldedLines(map[int]bool{})
	return nil
}
