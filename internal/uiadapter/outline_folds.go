package uiadapter

import "github.com/tbxark/gvim/fold"

// SetOutlineFolds sets the Contents section's structural fold ranges for the open
// document at path, tied to the text version they were computed from. The editor
// merges them with the indentation and language-server folds; they apply only
// while the document still has that text version.
func (a *Editor) SetOutlineFolds(path string, ranges []fold.Range, textVersion uint64) {
	a.ed.SetOutlineFolds(path, ranges, textVersion)
}

// FoldedAt reports whether the fold that starts on line is collapsed in the active
// document.
func (a *Editor) FoldedAt(line int) bool { return a.ed.FoldedAt(line) }
