package editor

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/session"
)

// The unsaved edits of a large document are not held as one text: they are an overlay
// of edited lines over the file (see buffer.Overlay). A session keeps them as that
// delta, which is small however large the file is, and puts them back onto the same
// file when it opens again. Because the lines the overlay leaves alone are the file's
// own, the delta fits only the bytes it was taken from: its identity is the size and
// modification time of the file the document reads, and a file that differs refuses it.

// ErrRestoring is returned by an action that would change the text of a large document
// while its stashed edits are being put back. The edits wait for the line index to
// reach the lines they refer to, and a change made before they are back would be lost
// or would clash with them.
var ErrRestoring = errors.New("unsaved changes are still being restored")

// ErrStashStale is returned when a stash of unsaved edits does not fit the file that
// was opened: the file is not the one the edits were made on.
var ErrStashStale = errors.New("the file changed since the unsaved changes were stashed")

// errNotLargeNow is the refusal of a stash of a large document when the file opens as
// a small one now, for instance because the size threshold changed.
var errNotLargeNow = errors.New("the file is not opened in large file mode now")

// mtimeNano is the modification time of the file ds describes in nanoseconds, or 0 when
// there is no such file.
func mtimeNano(ds diskState) int64 {
	if !ds.exists {
		return 0
	}
	return ds.mtime.UnixNano()
}

// restoring reports whether stashed edits are waiting to be put back into d.
func (d *Document) restoring() bool {
	return d.large != nil && d.large.restore != nil && d.large.restoreErr == nil
}

// holdsStash reports whether d keeps a stash of edits that are not in its text: one
// that waits for the line index, or one that could not be put back.
func (d *Document) holdsStash() bool {
	return d.large != nil && d.large.restore != nil
}

// OverlayStash returns the unsaved edits of a large document in the form a session
// keeps them. ok is false for a document that is not large, has no unsaved edits and
// is not holding a stash. A document that is waiting to put its stash back returns that
// stash, so a session saved meanwhile keeps it. One whose stash could not be put back
// returns it only while the text has no edits of its own; with edits, those are the
// stash, and FailedStash is the one that failed.
func (d *Document) OverlayStash() (st session.OverlayStash, ok bool) {
	later, ok := d.OverlayStashLater()
	if !ok {
		return session.OverlayStash{}, false
	}
	return later(), true
}

// OverlayStashLater is OverlayStash with the work of building the stash moved into the
// function it returns. The call itself is cheap whatever the size of the edits, so the
// caller can take it on the UI goroutine and build and encode the stash in a command. The
// function gives the stash as it was when OverlayStashLater ran.
func (d *Document) OverlayStashLater() (later func() session.OverlayStash, ok bool) {
	l := d.large
	if l == nil {
		return nil, false
	}
	if l.restore != nil && (d.restoring() || !d.Buffer.Modified()) {
		st := *l.restore
		return func() session.OverlayStash { return st }, true
	}
	if !d.Buffer.Modified() {
		return nil, false
	}
	ov, ok := d.Buffer.OverlayLater()
	if !ok {
		return nil, false
	}
	st := session.OverlayStash{
		Path: d.Path, Size: l.size, MTimeUnixNano: l.mtime, Skip: l.skip, EOL: d.eol,
	}
	if w := l.written; w != nil && w.buf == d.Buffer {
		// The buffer reads a file that was replaced by a save. The stash must fit the
		// file at the path, so the edits are described over the file that was written. If
		// they cannot be, the stash keeps the identity of the file the buffer reads, which
		// the file at the path no longer has, and a restore refuses it.
		if over, err := d.Buffer.OverlayOver(w.state); err == nil {
			st.Size, st.MTimeUnixNano, st.Skip, st.Overlay = w.size, w.mtime, w.skip, over
			return func() session.OverlayStash { return st }, true
		}
	}
	return func() session.OverlayStash {
		out := st
		out.Overlay = ov()
		return out
	}, true
}

// FailedStash returns the stash that could not be put back into d, when the text has
// edits of its own and OverlayStash therefore returns those. A session keeps both.
func (d *Document) FailedStash() (st session.OverlayStash, ok bool) {
	if l := d.large; l != nil && l.restore != nil && !d.restoring() && d.Buffer.Modified() {
		return *l.restore, true
	}
	return session.OverlayStash{}, false
}

// stashUnsaved writes the unsaved edits of d beside the session as an overlay stash and
// returns the sentence that tells the user where they are, or why they are not kept.
// It writes nothing, and returns "", for a document without edits and one that has no
// stash directory. A document whose edits were stashed already at this version gets
// the same sentence again and no second file. This is
// the copy that is made at once when a file changes under a document or its index
// fails: the edits cannot be saved over that file, and they must not depend on the
// session being saved later.
func (d *Document) stashUnsaved() string {
	l := d.large
	if l == nil || l.stashDir == "" {
		return ""
	}
	st, ok := d.OverlayStash()
	if !ok {
		return ""
	}
	if l.stashedOK && l.stashedVer == d.Buffer.Version() {
		return l.stashNote
	}
	name, err := session.StashOverlay(l.stashDir, st)
	if err != nil {
		return fmt.Sprintf("The unsaved changes could not be stashed: %v", err)
	}
	l.stashedVer, l.stashedOK = d.Buffer.Version(), true
	l.stashNote = "Unsaved changes stashed in " + filepath.Join(l.stashDir, name)
	return l.stashNote
}

// RestoreOverlay puts the unsaved edits of st back into d, which was just opened. The
// stash must fit the file d reads (ErrStashStale otherwise) and d must be an
// unedited large document. The edits refer to lines of the file, so while the line
// index has not reached them the restore waits: it returns nil, the document refuses
// changes to its text meanwhile (ErrRestoring), and ApplyIndex finishes it. An error
// means nothing was restored and the caller keeps the stash.
func (e *Editor) RestoreOverlay(d *Document, st session.OverlayStash) error {
	l := d.large
	switch {
	case l == nil:
		return errNotLargeNow
	case st.Size != l.size || st.MTimeUnixNano != l.mtime || st.Skip != l.skip:
		return ErrStashStale
	case d.Buffer.Modified() || l.restore != nil:
		return errors.New("the document has changes already")
	}
	l.restore, l.restoreErr = &st, nil
	if err := e.tryRestore(d); err != nil {
		l.restore = nil
		return err
	}
	return nil
}

// ReplaceOverlay puts the unsaved edits of st into d as RestoreOverlay does, but d may
// hold edits or a stash already: they are dropped for st, which is the newer copy (the
// crash-recovery copy over a session that was restored first). The document is read from
// its file again for that, so st must fit the file as it is now. When it does not, or
// the file cannot be read, d is left as it was and the error says why.
func (e *Editor) ReplaceOverlay(d *Document, st session.OverlayStash) error {
	l := d.large
	if l == nil {
		return errNotLargeNow
	}
	if !d.Buffer.Modified() && l.restore == nil {
		return e.RestoreOverlay(d, st)
	}
	fresh, err := readDocument(d.Path, e.loadSpec())
	switch {
	case err != nil:
		return err
	case fresh.large == nil:
		e.releaseDoc(fresh)
		return errNotLargeNow
	case st.Size != fresh.large.size || st.MTimeUnixNano != fresh.large.mtime || st.Skip != fresh.large.skip:
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
	l := d.large
	err := d.Buffer.RestoreOverlay(l.restore.Overlay)
	switch {
	case err == nil:
		l.restore = nil
		// A jump asked for while the stash waited (the session puts the caret back)
		// still waits for the index; the rest of the state is stale.
		jump := l.pending
		d.resetHistoryState()
		l.pending = jump
		d.status = "Restored unsaved changes"
	case errors.Is(err, buffer.ErrIndexPending):
		d.status = "Restoring unsaved changes: " + scanningStatus(d.Index())
		return nil
	}
	return err
}

// continueRestore is called after each step of the index. A stash that cannot be put
// back at all ends the wait: the document is usable again, the stash stays with it so
// that a session saved later keeps it, and the status says so. It returns the sentence
// for the status when this call ended the wait, and "" otherwise.
func (e *Editor) continueRestore(d *Document) (note string) {
	l := d.large
	if l == nil || l.restore == nil || l.restoreErr != nil {
		return ""
	}
	if err := e.tryRestore(d); err != nil {
		l.restoreErr = err
		d.status = fmt.Sprintf("The unsaved changes could not be restored and are kept in the session: %v", err)
		d.statusErr = d.status
		return d.status
	}
	if l.restore == nil {
		return d.status
	}
	return ""
}

// refuseWhileRestoring returns ErrRestoring for an action that changes the text of d
// while its stash waits for the index.
func (d *Document) refuseWhileRestoring(a Action) error {
	if d.restoring() && textChanging[a] {
		return fmt.Errorf("%s: %w", a, ErrRestoring)
	}
	return nil
}

// refuseEditWhileRestoring returns ErrRestoring for a change to the text of d that does
// not go through Do (a replacement, a shell filter, an edit from the language server)
// while its stash waits for the index. what names the change in the message.
func (d *Document) refuseEditWhileRestoring(what string) error {
	if d.restoring() {
		return fmt.Errorf("%s: %w", what, ErrRestoring)
	}
	return nil
}
