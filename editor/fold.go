package editor

// toggleFold collapses the fold that starts on the caret line, or expands it when
// it is collapsed already. A collapsed start that no longer begins a fold, because
// the text above it changed, can still be expanded.
func toggleFold(e *Editor, d *Document, _ map[string]any) error {
	return e.toggleFoldAt(d, d.Cursor.Line)
}

// toggleFoldAt is toggleFold for the fold that starts at line.
func (e *Editor) toggleFoldAt(d *Document, line int) error {
	if d.LargeState() != nil {
		// A large document has no folds, so "no fold starts on this line" would be
		// untrue of a line that looks like it starts one.
		return &largeError{Msg: "folds are not available in large file mode", Errs: []error{ErrLargeFile}}
	}
	if d.FoldedLines()[line] {
		delete(d.FoldedLines(), line)
		return nil
	}
	for _, r := range d.Folds(e.tabSizeOf(d)) {
		if r.Start == line {
			if d.FoldedLines() == nil {
				d.SetFoldedLines(map[int]bool{})
			}
			d.FoldedLines()[line] = true
			return nil
		}
	}
	return ErrNoFold
}

// foldAll collapses every fold in the active document.
func foldAll(e *Editor, d *Document, _ map[string]any) error {
	if d.LargeState() != nil {
		return &largeError{Msg: "folds are not available in large file mode", Errs: []error{ErrLargeFile}}
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
func unfoldAll(e *Editor, d *Document, _ map[string]any) error {
	if d.LargeState() != nil {
		return &largeError{Msg: "folds are not available in large file mode", Errs: []error{ErrLargeFile}}
	}
	d.SetFoldedLines(map[int]bool{})
	return nil
}
