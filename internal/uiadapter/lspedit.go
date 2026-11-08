package uiadapter

import "github.com/tbxark/gvim/editor"

// ApplyLSPEdits applies the edits of a language server to the open document at
// path, as one undo step. Like any action that may change the text, it marks the
// syntax spans stale first. The root sees it through its lspEditor port.
func (a *Editor) ApplyLSPEdits(path string, edits []editor.LSPEdit) error {
	a.edited()
	return a.ed.ApplyLSPEdits(path, edits)
}
