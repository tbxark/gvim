package editor

import "github.com/tbxark/gvim/fold"

// SetOutlineFolds sets the structural fold ranges an outline source (the Contents
// sidebar section) gave for the open document at path, together with the text
// version they were computed from. They merge with the indentation and
// language-server folds, so the gutter, the fold actions and the hidden-line logic
// see one set. The ranges apply only while the document still has that text
// version: an edit drops them until the outline publishes the ranges of the new
// text, so a stale heading range can never fold lines it was not computed for. An
// empty list clears them. A path that is not open is ignored, as for
// SetExternalFolds.
func (e *Editor) SetOutlineFolds(path string, ranges []fold.Range, textVersion uint64) {
	d := e.lspDocAt(path)
	if d == nil {
		return
	}
	d.SetOutlineFolds(ranges, textVersion)
}

// FoldedAt reports whether the fold that starts on line is collapsed in the active
// document. The root asks before it unfolds a heading, so a fold the reader made
// by hand is not toggled back and forth.
func (e *Editor) FoldedAt(line int) bool {
	d := e.Active()
	return d != nil && d.Folded(line)
}
