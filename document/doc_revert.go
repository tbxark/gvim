package document

import (
	"errors"
)

// ErrStaleRevert is returned by ApplyRevert when the text changed after PlanRevert. The file
// that RunRevert read is then not the one the document was planned against, so the reload is
// refused and the edit is kept.
var ErrStaleRevert = errors.New("text changed since the revert was planned")

// adoptFile records the file that a read found as the saved state of d: the saved text, and
// the line ending, encoding and disk state of the read. A result with no read behind it (fresh
// is nil) changes only the saved text.
func (d *Document) AdoptFile(text string, fresh *Document) {
	d.Buffer.MarkSaved()
	d.MetaDirty = false
	d.SetSaved(text)
	if fresh == nil {
		return
	}
	d.Eol = fresh.Eol
	d.Ends = fresh.Ends
	d.Disk = fresh.Disk
	d.Codec = fresh.Codec
	d.encoding = fresh.encoding
	// A binary tab whose file became text is text now. It stays read-only, since it was
	// opened that way, and toggle_read_only can make it editable.
	d.binary = fresh.binary
}

// revertedStatus is the status after a reload from disk: "Reverted a.txt" for a revert the
// user asked for, and "Reloaded a.txt from disk" for one made because the file changed.
func (d *Document) RevertedStatus() string {
	if d.RevertAuto {
		return "Reloaded " + d.Name() + " from disk"
	}
	return "Reverted " + d.Name()
}
