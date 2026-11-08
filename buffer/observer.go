package buffer

import "unicode/utf8"

// MaxRemoved is the longest removed text a Change carries. A deletion of more
// bytes reports Removed as "".
const MaxRemoved = 64 << 10

// Change is one primitive edit of a Buffer, as an observer sees it.
//
// Changes are sequential. From and To are in the coordinates of the document
// immediately BEFORE this change, so a consumer that applies the changes it
// receives first to last keeps a mirror of the text in step with the buffer.
// A Replace is two changes, a delete and then an insert at its From. Undo and
// Redo report one change per primitive step they undo or redo, newest step
// first for Undo, with Undo set.
type Change struct {
	// From and To are the replaced range in rune coordinates: To equals From
	// for an insert, and an insert has no removed text. A delete has Text "".
	From, To Pos
	// FromByte and ToByte are the byte columns of From and To inside their
	// lines, in the text as the buffer holds it (UTF-8 of the runes).
	FromByte, ToByte int
	// Text is the inserted text. Removed is the deleted text, and "" when it is
	// longer than MaxRemoved bytes.
	Text, Removed string
	// Undo reports that Undo or Redo produced the change.
	Undo bool
	// Coarse reports a change whose range and texts are not reported: an edit
	// of a large buffer, its Undo or Redo, a restored overlay, or a document
	// whose buffer was replaced. A consumer must drop what it derived from the
	// text. Only Version and Undo are set.
	Coarse bool
	// Version is Buffer.Version() after the change. The changes of one Undo or
	// Redo all carry the version after the whole step.
	Version uint64
}

// SetObserver installs fn to be called synchronously, once per Change, after
// every Insert, Delete, Undo and Redo has changed the buffer. The edit call
// has not returned yet, so fn sees the buffer in the state after the change
// and must not change it. A nil fn turns observation off. With no observer an
// edit does no extra work and allocates nothing for it.
func (b *Buffer) SetObserver(fn func(Change)) {
	b.obs = fn
}

// changeOf describes an edit about to be made, before the buffer changes. It
// is only called when an observer is installed.
func (b *Buffer) changeOf(from, to Pos, text, removed string) Change {
	// The buffer stores runes, so each invalid byte it was given is held as
	// U+FFFD. A consumer that applies Text to a mirror must get those bytes.
	// Undo hands back the text of an insert as removed text.
	text, removed = asStored(text), asStored(removed)
	if len(removed) > MaxRemoved {
		removed = ""
	}
	return Change{
		From: from, To: to,
		FromByte: b.ByteCol(from), ToByte: b.ByteCol(to),
		Text: text, Removed: removed,
	}
}

// asStored returns s as the buffer holds it: UTF-8 of its runes.
func asStored(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return string([]rune(s))
}

// notify reports c, with the current version, to the observer.
func (b *Buffer) notify(c Change) {
	if b.obs != nil {
		c.Version = b.ver
		b.obs(c)
	}
}

// notifyCoarse reports a change of unknown extent.
func (b *Buffer) notifyCoarse(undo bool) {
	if b.obs != nil {
		b.obs(Change{Coarse: true, Undo: undo, Version: b.ver})
	}
}

// notifyStep reports the changes of one Undo or Redo, which have all been made
// by now, or one coarse change when the buffer is large (seen is then empty).
func (b *Buffer) notifyStep(seen []Change) {
	if b.obs == nil {
		return
	}
	if b.big != nil {
		b.notifyCoarse(true)
		return
	}
	for _, c := range seen {
		c.Undo = true
		b.notify(c)
	}
}
