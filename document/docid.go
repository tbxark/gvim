package document

import (
	"github.com/tbxark/gvim/buffer"
	"sync/atomic"
)

// DocID names one open document for as long as it is open. Tab moves, closes of
// other tabs, a revert or a reload of its file never change it. The zero DocID
// names nothing.
type DocID uint64

// Version identifies one content state of one document. Compare Versions for
// equality only. Gen changes whenever the document's Buffer object is replaced
// (revert, reload, encoding change), and starts at 1, so the zero Version never
// names a real state. Ver is Buffer.Version(), which is not monotonic: Undo
// returns an earlier value together with the earlier text, so equal Versions
// mean equal text.
type Version struct{ Gen, Ver uint64 }

// IsZero reports whether v is the zero Version, which names no state.
func (v Version) IsZero() bool { return v == Version{} }

// docSeam is the per-document state of the plugin seams: its id, its buffer
// generation and the change tracking. It is a field of Document.
type docSeam struct {
	Id  DocID
	Gen uint64
	// obs is the buffer observer while changes are tracked, else nil.
	Obs func(buffer.Change)
	// ring holds the recent changes for TakeChanges while they are tracked.
	Ring *changeRing
}

// genCounter hands out buffer generations. It is shared by all editors so a
// Version never repeats in a process.
var genCounter atomic.Uint64

// ID returns the id of the document. It is 0 until an Editor has adopted the
// document, which happens when it enters a tab.
func (d *Document) ID() DocID { return d.seam.Id }

// Gen returns the generation of the document's buffer: a number from a
// process-wide counter, starting at 1, that changes whenever the Buffer is
// replaced.
func (d *Document) Gen() uint64 {
	if d.seam.Gen == 0 {
		d.seam.Gen = genCounter.Add(1)
	}
	return d.seam.Gen
}

// Version returns the token of the current content, to detect that the text
// changed between a read and a later edit (EditOptions.Expect).
func (d *Document) Version() Version {
	return Version{Gen: d.Gen(), Ver: d.Buffer.Version()}
}

// setBuffer makes b the document's buffer. It is the only place that replaces
// the Buffer object: it starts a new generation, moves the change observer to b
// and reports one coarse change, so whoever tracks the text drops its state.
func (d *Document) SetBuffer(b *buffer.Buffer) {
	if d.Buffer != nil && d.Buffer != b {
		d.Buffer.SetObserver(nil)
	}
	d.Buffer = b
	d.seam.Gen = genCounter.Add(1)
	if d.seam.Obs != nil {
		b.SetObserver(d.seam.Obs)
		d.seam.Obs(buffer.Change{Coarse: true, Version: b.Version()})
	}
}
