package editor

// ToggleFoldAt collapses the fold that starts at line, or expands it when it is
// collapsed, for a click on the fold marker in the gutter. The caret stays where it
// is, unless the fold hides it, and then it moves to the fold start. It reports
// whether a fold changed.
func (e *Editor) ToggleFoldAt(line int) bool {
	d := e.Active()
	if d == nil || e.toggleFoldAt(d, line) != nil {
		return false
	}
	d.revealCarets(d.Cursor.Line, e.tabSizeOf(d))
	return true
}
