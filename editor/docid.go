package editor

import (
	"fmt"
)

// adopt gives d an id, if it has none, and starts observing its buffer when
// changes are tracked. It is the one place that assigns ids.
func (e *Editor) adopt(d *Document) {
	if d.Seam().Id == 0 {
		e.seam.nextID++
		d.Seam().Id = DocID(e.seam.nextID)
	}
	if e.seam.tracked > 0 && d.Seam().Obs == nil && !d.SharedMirror() {
		e.observe(d)
	}
}

// addDoc appends d as the last tab. Every place that adds a tab calls it.
func (e *Editor) addDoc(d *Document) {
	e.adopt(d)
	e.seedRulers(d)
	e.docs = append(e.docs, d)
	e.touchRecent(d)
}

// setDoc puts d in place of tab i, whose document the caller has released.
func (e *Editor) setDoc(i int, d *Document) {
	if i >= 0 && i < len(e.docs) {
		e.forgetRecent(e.docs[i])
	}
	e.adopt(d)
	e.seedRulers(d)
	e.docs[i] = d
	e.touchRecent(d)
}

// adoptAll adopts every document, for the callers that look documents up.
// Documents enter through addDoc and setDoc, so this only finds the ones a test
// put in the tab list directly.
func (e *Editor) adoptAll() {
	for _, d := range e.docs {
		if d.Seam().Id == 0 || (e.seam.tracked > 0 && d.Seam().Obs == nil) {
			e.adopt(d)
		}
	}
}

// DocByID returns the document with the given id and the index of its tab.
func (e *Editor) DocByID(id DocID) (*Document, int, bool) {
	e.adoptAll()
	for i, d := range e.docs {
		if d.Seam().Id == id && id != 0 {
			return d, i, true
		}
	}
	return nil, -1, false
}

// ActiveID returns the id of the active document, or 0 when no tab is open.
func (e *Editor) ActiveID() DocID {
	e.adoptAll()
	if d := e.Active(); d != nil {
		return d.Seam().Id
	}
	return 0
}

// DocIDs returns the ids of the open documents in tab order.
func (e *Editor) DocIDs() []DocID {
	e.adoptAll()
	ids := make([]DocID, len(e.docs))
	for i, d := range e.docs {
		ids[i] = d.Seam().Id
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
