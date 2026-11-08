package editor

import "github.com/tbxark/gvim/document"

// registerDabbrevHandlers registers dabbrev_expand.
func registerDabbrevHandlers(h map[Action]handler) {
	h["dabbrev_expand"] = dabbrevExpand
}

// dabbrevExpand completes the word before the caret from earlier words of the
// active document, then from the words of the other open buffers in MRU order. A
// repeated press, with the text untouched, moves to the next candidate.
func dabbrevExpand(e *Editor, _ map[string]any) error {
	d := e.Active()
	c := &e.extras.dabbrev
	if !c.Shows(d) {
		// Other tabs in MRU order (the active tab first, skipped here), the same order
		// Fresh's dabbrev provider scans its other buffers in.
		other := make([]*Document, 0, min(len(e.docs)-1, document.DabbrevOtherBufferLimit))
		for _, i := range e.RecentTabIndices() {
			if i == e.active {
				continue
			}
			other = append(other, e.docs[i])
			if len(other) >= document.DabbrevOtherBufferLimit {
				break
			}
		}
		if err := c.Begin(d, other); err != nil {
			*c = dabbrevCycle{}
			return err
		}
	}
	c.Step(d)
	return nil
}
