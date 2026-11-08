package editor

import (
	"fmt"
	"sync/atomic"

	"github.com/tbxark/gvim/buffer"
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
	id  DocID
	gen uint64
	// obs is the buffer observer while changes are tracked, else nil.
	obs func(buffer.Change)
	// ring holds the recent changes for TakeChanges while they are tracked.
	ring *changeRing
}

// genCounter hands out buffer generations. It is shared by all editors so a
// Version never repeats in a process.
var genCounter atomic.Uint64

// ID returns the id of the document. It is 0 until an Editor has adopted the
// document, which happens when it enters a tab.
func (d *Document) ID() DocID { return d.seam.id }

// Gen returns the generation of the document's buffer: a number from a
// process-wide counter, starting at 1, that changes whenever the Buffer is
// replaced.
func (d *Document) Gen() uint64 {
	if d.seam.gen == 0 {
		d.seam.gen = genCounter.Add(1)
	}
	return d.seam.gen
}

// Version returns the token of the current content, to detect that the text
// changed between a read and a later edit (EditOptions.Expect).
func (d *Document) Version() Version {
	return Version{Gen: d.Gen(), Ver: d.Buffer.Version()}
}

// setBuffer makes b the document's buffer. It is the only place that replaces
// the Buffer object: it starts a new generation, moves the change observer to b
// and reports one coarse change, so whoever tracks the text drops its state.
func (d *Document) setBuffer(b *buffer.Buffer) {
	if d.Buffer != nil && d.Buffer != b {
		d.Buffer.SetObserver(nil)
	}
	d.Buffer = b
	d.seam.gen = genCounter.Add(1)
	if d.seam.obs != nil {
		b.SetObserver(d.seam.obs)
		d.seam.obs(buffer.Change{Coarse: true, Version: b.Version()})
	}
}

// adopt gives d an id, if it has none, and starts observing its buffer when
// changes are tracked. It is the one place that assigns ids.
func (e *Editor) adopt(d *Document) {
	if d.seam.id == 0 {
		e.seam.nextID++
		d.seam.id = DocID(e.seam.nextID)
	}
	if e.seam.tracked > 0 && d.seam.obs == nil {
		e.observe(d)
	}
}

// addDoc appends d as the last tab. Every place that adds a tab calls it.
func (e *Editor) addDoc(d *Document) {
	e.adopt(d)
	e.seedRulers(d)
	e.docs = append(e.docs, d)
}

// setDoc puts d in place of tab i, whose document the caller has released.
func (e *Editor) setDoc(i int, d *Document) {
	e.adopt(d)
	e.seedRulers(d)
	e.docs[i] = d
}

// adoptAll adopts every document, for the callers that look documents up.
// Documents enter through addDoc and setDoc, so this only finds the ones a test
// put in the tab list directly.
func (e *Editor) adoptAll() {
	for _, d := range e.docs {
		if d.seam.id == 0 || (e.seam.tracked > 0 && d.seam.obs == nil) {
			e.adopt(d)
		}
	}
}

// DocByID returns the document with the given id and the index of its tab.
func (e *Editor) DocByID(id DocID) (*Document, int, bool) {
	e.adoptAll()
	for i, d := range e.docs {
		if d.seam.id == id && id != 0 {
			return d, i, true
		}
	}
	return nil, -1, false
}

// ActiveID returns the id of the active document, or 0 when no tab is open.
func (e *Editor) ActiveID() DocID {
	e.adoptAll()
	if d := e.Active(); d != nil {
		return d.seam.id
	}
	return 0
}

// DocIDs returns the ids of the open documents in tab order.
func (e *Editor) DocIDs() []DocID {
	e.adoptAll()
	ids := make([]DocID, len(e.docs))
	for i, d := range e.docs {
		ids[i] = d.seam.id
	}
	return ids
}

// docByID is DocByID for the methods that return an error.
func (e *Editor) docByID(id DocID) (*Document, int, error) {
	d, i, ok := e.DocByID(id)
	if !ok {
		return nil, -1, fmt.Errorf("%w: id %d", ErrNotOpen, id)
	}
	return d, i, nil
}
