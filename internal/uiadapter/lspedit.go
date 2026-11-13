package uiadapter

import "github.com/tbxark/gvim/editor"

// ApplyLSPEdits applies the edits of a language server to the open document at
// path, as one undo step. The root sees it through its lspEditor port.
func (a *Editor) ApplyLSPEdits(path string, edits []editor.LSPEdit) error {
	return a.ed.ApplyLSPEdits(path, edits)
}
