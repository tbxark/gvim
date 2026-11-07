package editor

import "slices"

// MoveTab moves the tab of document id by delta places, towards the end for a
// positive delta. A move past either end stops at the end, and a move of 0 or
// of a tab that is already there changes nothing. The active tab stays on its
// document, and the document keeps its id.
func (e *Editor) MoveTab(id DocID, delta int) error {
	d, from, err := e.docByID(id)
	if err != nil {
		return err
	}
	to := min(max(from+delta, 0), len(e.docs)-1)
	if to == from {
		return nil
	}
	active := e.Active()
	e.docs = slices.Insert(slices.Delete(e.docs, from, from+1), to, d)
	if i := slices.Index(e.docs, active); i >= 0 {
		e.active = i
	}
	return nil
}
