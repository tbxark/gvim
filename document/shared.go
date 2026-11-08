package document

import (
	"errors"
	"fmt"

	"github.com/tbxark/gvim/buffer"
)

// Cross-workspace shared buffers (daemon #19b bounded slice). A document is either the
// owner of a buffer or a read-only mirror of another workspace's document. Mirrors are
// independent Documents with their own cursor/view, kept in step with the owner through
// an explicit change path: the owner editor forwards each buffer.Change, the mirror
// applies it when its recorded owner version is exactly the version before the change,
// and reloads the whole text when it cannot (coarse edits, undo steps, a stale version).
// Text, saved state and unsaved state follow the owner; a mirror never writes the file.
// Large files, virtual buffers and remote workspaces are kept independent.

// MaxSharedDocumentBytes bounds the text a shared mirror may hold. Sharing is meant for
// ordinary source files; a larger document opens independently as before.
const MaxSharedDocumentBytes = 8 << 20

var errSharedUnsupported = errors.New("document cannot be shared across workspaces")

// NewMirrorDocument returns a read-only mirror of owner for another workspace's editor.
// The text and the saved/disk/encoding state are copied; the cursor starts at the top.
func NewMirrorDocument(owner *Document) (*Document, error) {
	if owner == nil || owner.Buffer == nil {
		return nil, errSharedUnsupported
	}
	if owner.large != nil || owner.Virtual() {
		return nil, fmt.Errorf("%w: large or virtual buffers stay independent", errSharedUnsupported)
	}
	text, err := owner.Buffer.TextChecked()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSharedUnsupported, err)
	}
	if len(text) > MaxSharedDocumentBytes {
		return nil, fmt.Errorf("%w: the text is over %d bytes", errSharedUnsupported, MaxSharedDocumentBytes)
	}
	d := newDocument(owner.Path, text)
	d.sharedMirror = true
	d.sharedVersion = owner.Buffer.Version()
	d.sharedSelfVersion = d.Buffer.Version()
	d.readOnly = true
	d.Codec = owner.Codec
	d.encoding = owner.encoding
	d.Eol = owner.Eol
	if len(owner.Ends) > 0 {
		d.Ends = append([]string(nil), owner.Ends...)
	}
	d.Disk = owner.Disk
	d.SetSaved(owner.Saved)
	d.MetaDirty = owner.MetaDirty
	d.Status = "Mirror of another workspace: read-only until focused"
	return d, nil
}

// SharedMirror reports whether d is a read-only cross-workspace mirror.
func (d *Document) SharedMirror() bool { return d.sharedMirror }

// SetSharedMirrorStatus sets the mirror's status row text.
func (d *Document) SetSharedMirrorStatus(text string) {
	if d.sharedMirror {
		d.Status = text
	}
}

// SyncMirrorFrom replaces the mirror's text and saved state with owner's. It is the
// fallback of the stale-version guard and the sync a focus performs; owner is never
// changed. It returns an error while owner is large/virtual or over the size bound.
func (d *Document) SyncMirrorFrom(owner *Document) error {
	if d == nil || owner == nil || owner.Buffer == nil || !d.sharedMirror {
		return errSharedUnsupported
	}
	if owner.large != nil || owner.Virtual() {
		return fmt.Errorf("%w: the owner became a large or virtual buffer", errSharedUnsupported)
	}
	text, err := owner.Buffer.TextChecked()
	if err != nil {
		return fmt.Errorf("%w: %v", errSharedUnsupported, err)
	}
	if len(text) > MaxSharedDocumentBytes {
		return fmt.Errorf("%w: the text is over %d bytes", errSharedUnsupported, MaxSharedDocumentBytes)
	}
	d.Buffer.Delete(buffer.Pos{}, d.Buffer.End())
	if text != "" {
		d.Buffer.Insert(buffer.Pos{}, text)
	}
	d.sharedVersion = owner.Buffer.Version()
	d.sharedSelfVersion = d.Buffer.Version()
	d.Codec = owner.Codec
	d.encoding = owner.encoding
	d.Eol = owner.Eol
	if len(owner.Ends) > 0 {
		d.Ends = append([]string(nil), owner.Ends...)
	} else {
		d.Ends = nil
	}
	d.Disk = owner.Disk
	d.SetSaved(owner.Saved)
	d.MetaDirty = owner.MetaDirty
	d.readOnly = true
	return nil
}

// ApplyOwnerChange applies one owner buffer change to the mirror. The stale-version
// guard: when the change is coarse, an undo/redo report, or its version is not exactly
// the version after the mirror's recorded one, the mirror reloads the whole text from
// the owner instead of applying a possibly wrong range.
func (d *Document) ApplyOwnerChange(owner *Document, c buffer.Change) error {
	if d == nil || owner == nil || !d.sharedMirror {
		return errSharedUnsupported
	}
	if c.Coarse || c.Undo || c.Version != d.sharedVersion+1 || d.Buffer.Version() != d.sharedSelfVersion {
		if err := d.SyncMirrorFrom(owner); err != nil {
			return err
		}
		d.SetSharedMirrorStatus("Mirror of another workspace: reloaded after an out-of-step edit")
		return nil
	}
	if c.From != c.To {
		d.Buffer.Delete(c.From, c.To)
	}
	if c.Text != "" {
		d.Buffer.Insert(c.From, c.Text)
	}
	d.sharedVersion = c.Version
	d.sharedSelfVersion = d.Buffer.Version()
	d.Disk = owner.Disk
	d.SetSaved(owner.Saved)
	return nil
}

// MakeSharedOwner clears the mirror state of a document that has become the owner.
func (d *Document) MakeSharedOwner() {
	d.sharedMirror = false
	d.readOnly = false
	d.SetSharedMirrorStatus("")
}
