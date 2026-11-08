package uiadapter

import "github.com/tbxark/gvim/fold"

// SetExternalFolds sets the language server's folding ranges for the open document
// at path, merged with the indentation folds by the editor. The root's LSP port
// calls it after a foldingRange reply.
func (a *Editor) SetExternalFolds(path string, ranges []fold.Range) {
	a.ed.SetExternalFolds(path, ranges)
}
