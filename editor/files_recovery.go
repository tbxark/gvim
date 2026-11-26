package editor

import "github.com/tbxark/gvim/document"

// UnsavedDocs returns the open documents whose text differs from what was last saved
// or read, in tab order. A document with only a new encoding or line ending is not
// one, and neither is a binary one. The crash-recovery copy is made from them.
func (e *Editor) UnsavedDocs() []*document.Document {
	var out []*document.Document
	for _, d := range e.docs {
		if !d.Ext().Virtual && (d.Buffer.Modified() || d.Restoring()) {
			out = append(out, d)
		}
	}
	return out
}
