package editor

import "github.com/tbxark/gvim/document"

// blockHandler returns the handler of a block selection step in direction
// (dLine, dCol).
func blockHandler(dLine, dCol int) handler {
	return docAction(func(e *Editor, d *document.Document, _ map[string]any) error {
		d.BlockStep(dLine, dCol, e.virtualBlock(d))
		return nil
	})
}
