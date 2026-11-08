package editor

import (
	"fmt"
)

// ApplyLSPEditsExpect is ApplyLSPEdits with a version check: when expect is
// non-nil the document must still be at that version, or the edits are refused
// with ErrStale and nothing changes. The language-server port uses it for a
// workspace edit that was computed against an older text.
func (e *Editor) ApplyLSPEditsExpect(path string, edits []LSPEdit, expect *Version) error {
	d := e.lspDocAt(path)
	if d == nil {
		return fmt.Errorf("%w: %s", ErrNotOpen, path)
	}
	return d.ApplyEditList(edits, EditOptions{Cursor: CursorMap, Expect: expect}, true)
}

// OpenDocumentVersion returns the version of the open document at path, and
// whether an open document has that path. The language-server port uses it to
// capture the version a workspace edit must still see.
func (e *Editor) OpenDocumentVersion(path string) (Version, bool) {
	d := e.lspDocAt(path)
	if d == nil {
		return Version{}, false
	}
	return d.Version(), true
}
