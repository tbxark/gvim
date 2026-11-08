package document

import (
	"errors"
	"fmt"
	"path/filepath"
)

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
	if !ds.Exists {
		return 0
	}
	return ds.Mtime.UnixNano()
}

// restoring reports whether stashed edits are waiting to be put back into d.
func (d *Document) Restoring() bool {
	return d.large != nil && d.large.Restore != nil && d.large.RestoreErr == nil
}

// holdsStash reports whether d keeps a stash of edits that are not in its text: one
// that waits for the line index, or one that could not be put back.
func (d *Document) holdsStash() bool {
	return d.large != nil && d.large.Restore != nil
}

// OverlayStash returns the unsaved edits of a large document in the form a session
// keeps them. ok is false for a document that is not large, has no unsaved edits and
// is not holding a stash. A document that is waiting to put its stash back returns that
// stash, so a session saved meanwhile keeps it. One whose stash could not be put back
// returns it only while the text has no edits of its own; with edits, those are the
// stash, and FailedStash is the one that failed.
func (d *Document) OverlayStash() (st OverlayStash, ok bool) {
	later, ok := d.OverlayStashLater()
	if !ok {
		return OverlayStash{}, false
	}
	return later(), true
}

// OverlayStashLater is OverlayStash with the work of building the stash moved into the
// function it returns. The call itself is cheap whatever the size of the edits, so the
// caller can take it on the UI goroutine and build and encode the stash in a command. The
// function gives the stash as it was when OverlayStashLater ran.
func (d *Document) OverlayStashLater() (later func() OverlayStash, ok bool) {
	l := d.large
	if l == nil {
		return nil, false
	}
	if l.Restore != nil && (d.Restoring() || !d.Buffer.Modified()) {
		st := *l.Restore
		return func() OverlayStash { return st }, true
	}
	if !d.Buffer.Modified() {
		return nil, false
	}
	ov, ok := d.Buffer.OverlayLater()
	if !ok {
		return nil, false
	}
	st := OverlayStash{
		Path: d.Path, Size: l.Size, MTimeUnixNano: l.Mtime, Skip: l.Skip, EOL: d.Eol,
	}
	if w := l.Written; w != nil && w.Buf == d.Buffer {
		// The buffer reads a file that was replaced by a save. The stash must fit the
		// file at the path, so the edits are described over the file that was written. If
		// they cannot be, the stash keeps the identity of the file the buffer reads, which
		// the file at the path no longer has, and a restore refuses it.
		if over, err := d.Buffer.OverlayOver(w.State); err == nil {
			st.Size, st.MTimeUnixNano, st.Skip, st.Overlay = w.Size, w.Mtime, w.Skip, over
			return func() OverlayStash { return st }, true
		}
	}
	return func() OverlayStash {
		out := st
		out.Overlay = ov()
		return out
	}, true
}

// FailedStash returns the stash that could not be put back into d, when the text has
// edits of its own and OverlayStash therefore returns those. A session keeps both.
func (d *Document) FailedStash() (st OverlayStash, ok bool) {
	if l := d.large; l != nil && l.Restore != nil && !d.Restoring() && d.Buffer.Modified() {
		return *l.Restore, true
	}
	return OverlayStash{}, false
}

// stashUnsaved writes the unsaved edits of d beside the session as an overlay stash and
// returns the sentence that tells the user where they are, or why they are not kept.
// It writes nothing, and returns "", for a document without edits and one that has no
// stash directory. A document whose edits were stashed already at this version gets
// the same sentence again and no second file. This is
// the copy that is made at once when a file changes under a document or its index
// fails: the edits cannot be saved over that file, and they must not depend on the
// session being saved later.
func (d *Document) StashUnsaved() string {
	l := d.large
	if l == nil || l.StashDir == "" {
		return ""
	}
	st, ok := d.OverlayStash()
	if !ok {
		return ""
	}
	if l.StashedOK && l.StashedVer == d.Buffer.Version() {
		return l.StashNote
	}
	name, err := StashOverlay(l.StashDir, st)
	if err != nil {
		return fmt.Sprintf("The unsaved changes could not be stashed: %v", err)
	}
	l.StashedVer, l.StashedOK = d.Buffer.Version(), true
	l.StashNote = "Unsaved changes stashed in " + filepath.Join(l.StashDir, name)
	return l.StashNote
}

// refuseWhileRestoring returns ErrRestoring for an action that changes the text of d
// while its stash waits for the index.
func (d *Document) refuseWhileRestoring(a Action) error {
	if d.Restoring() && textChanging[a] {
		return fmt.Errorf("%s: %w", a, ErrRestoring)
	}
	return nil
}

// refuseEditWhileRestoring returns ErrRestoring for a change to the text of d that does
// not go through Do (a replacement, a shell filter, an edit from the language server)
// while its stash waits for the index. what names the change in the message.
func (d *Document) RefuseEditWhileRestoring(what string) error {
	if d.Restoring() {
		return fmt.Errorf("%s: %w", what, ErrRestoring)
	}
	return nil
}
