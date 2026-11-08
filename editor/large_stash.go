package editor

import (
	"errors"
	"fmt"
	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/document"
)

// RestoreOverlay puts the unsaved edits of st back into d, which was just opened. The
// stash must fit the file d reads (ErrStashStale otherwise) and d must be an
// unedited large document. The edits refer to lines of the file, so while the line
// index has not reached them the restore waits: it returns nil, the document refuses
// changes to its text meanwhile (ErrRestoring), and ApplyIndex finishes it. An error
// means nothing was restored and the caller keeps the stash.
func (e *Editor) RestoreOverlay(d *Document, st document.OverlayStash) error {
	l := d.LargeState()
	switch {
	case l == nil:
		return errNotLargeNow
	case st.Size != l.Size || st.MTimeUnixNano != l.Mtime || st.Skip != l.Skip:
		return ErrStashStale
	case d.Buffer.Modified() || l.Restore != nil:
		return errors.New("the document has changes already")
	}
	l.Restore, l.RestoreErr = &st, nil
	if err := e.tryRestore(d); err != nil {
		l.Restore = nil
		return err
	}
	return nil
}

// ReplaceOverlay puts the unsaved edits of st into d as RestoreOverlay does, but d may
// hold edits or a stash already: they are dropped for st, which is the newer copy (the
// crash-recovery copy over a session that was restored first). The document is read from
// its file again for that, so st must fit the file as it is now. When it does not, or
// the file cannot be read, d is left as it was and the error says why.
func (e *Editor) ReplaceOverlay(d *Document, st document.OverlayStash) error {
	l := d.LargeState()
	if l == nil {
		return errNotLargeNow
	}
	if !d.Buffer.Modified() && l.Restore == nil {
		return e.RestoreOverlay(d, st)
	}
	fresh, err := readDocument(d.Path, e.loadSpec())
	switch {
	case err != nil:
		return err
	case fresh.LargeState() == nil:
		e.releaseDoc(fresh)
		return errNotLargeNow
	case st.Size != fresh.LargeState().Size || st.MTimeUnixNano != fresh.LargeState().Mtime || st.Skip != fresh.LargeState().Skip:
		e.releaseDoc(fresh)
		return ErrStashStale
	}
	e.swapFile(d, fresh)
	return e.RestoreOverlay(d, st)
}

// tryRestore puts the waiting stash of d into its buffer. It returns nil when that is
// done, and when it must wait for the index. The caller owns an error: nothing was
// restored.
func (e *Editor) tryRestore(d *Document) error {
	l := d.LargeState()
	err := d.Buffer.RestoreOverlay(l.Restore.Overlay)
	switch {
	case err == nil:
		l.Restore = nil
		// A jump asked for while the stash waited (the session puts the caret back)
		// still waits for the index; the rest of the state is stale.
		jump := l.Pending
		d.ResetHistoryState()
		l.Pending = jump
		d.Status = "Restored unsaved changes"
	case errors.Is(err, buffer.ErrIndexPending):
		d.Status = "Restoring unsaved changes: " + scanningStatus(d.Index())
		return nil
	}
	return err
}

// continueRestore is called after each step of the index. A stash that cannot be put
// back at all ends the wait: the document is usable again, the stash stays with it so
// that a session saved later keeps it, and the status says so. It returns the sentence
// for the status when this call ended the wait, and "" otherwise.
func (e *Editor) continueRestore(d *Document) (note string) {
	l := d.LargeState()
	if l == nil || l.Restore == nil || l.RestoreErr != nil {
		return ""
	}
	if err := e.tryRestore(d); err != nil {
		l.RestoreErr = err
		d.Status = fmt.Sprintf("The unsaved changes could not be restored and are kept in the session: %v", err)
		d.StatusErr = d.Status
		return d.Status
	}
	if l.Restore == nil {
		return d.Status
	}
	return ""
}
