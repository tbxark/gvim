package editor

import (
	"sync/atomic"

	"github.com/tbxark/gvim/fold"
)

// externalFoldRev orders the external fold sets pushed into documents. A document
// compares it to decide whether Folds must merge the ranges again. It is shared by
// all editors of the process, because it only ever grows.
var externalFoldRev atomic.Uint64

// SetExternalFolds sets the fold ranges a language server gave for the open
// document at path. They merge with the document's indentation folds, so the fold
// gutter, the fold actions and the hidden-line logic see one set. An empty list
// clears them. A path that is not open is ignored: the ranges belong to a text the
// server has, and opening the document asks for them again.
func (e *Editor) SetExternalFolds(path string, ranges []fold.Range) {
	d := e.lspDocAt(path)
	if d == nil {
		return
	}
	d.SetExternalFolds(ranges, externalFoldRev.Add(1))
}
