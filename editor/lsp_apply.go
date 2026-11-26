package editor

import (
	"fmt"
	"github.com/tbxark/gvim/document"
)

// ApplyLSPEditsExpect is ApplyLSPEdits with a version check: when expect is
// non-nil the document must still be at that version, or the edits are refused
// with ErrStale and nothing changes. The language-server port uses it for a
// workspace edit that was computed against an older text.
func (e *Editor) ApplyLSPEditsExpect(path string, edits []document.LSPEdit, expect *document.Version) error {
	d := e.docWithPath(path)
	if d == nil {
		return fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	return d.ApplyEditList(edits, document.EditOptions{Cursor: document.CursorMap, Expect: expect}, true)
}

// OpenDocumentVersion returns the version of the open document at path, and
// whether an open document has that path. The language-server port uses it to
// capture the version a workspace edit must still see.
func (e *Editor) OpenDocumentVersion(path string) (document.Version, bool) {
	d := e.docWithPath(path)
	if d == nil {
		return document.Version{}, false
	}
	return d.Version(), true
}
