package editor

import (
	"slices"

	"github.com/tbxark/gvim/internal/buffer"
)

// jumpHistoryMax is how many places the navigation history keeps, as in Fresh.
const jumpHistoryMax = 100

// jumpEntry is one place in the navigation history: a tab, a caret position and,
// when text was selected there, the selection anchor.
type jumpEntry struct {
	doc      *Document
	pos      buffer.Pos
	anchor   buffer.Pos
	selected bool
}

// jumpEntryOf is the place d is at now.
func jumpEntryOf(d *Document) jumpEntry {
	en := jumpEntry{doc: d, pos: d.Cursor}
	if d.HasSelection() {
		en.anchor, en.selected = d.Sel.Anchor, true
	}
	return en
}

// jumpHistory is the list of places that navigate_back and navigate_forward
// walk. cur is the index of the place the user is at in the list. It is only
// meaningful while entries is not empty.
type jumpHistory struct {
	entries []jumpEntry
	cur     int
}

// push records en as the newest place. Places after cur are dropped first, so
// a new jump clears the forward history. Recording the place already at cur is
// a no-op. The oldest place goes when the list is full.
func (h *jumpHistory) push(en jumpEntry) {
	if len(h.entries) > 0 {
		h.entries = h.entries[:h.cur+1]
		if h.entries[h.cur] == en {
			return
		}
	}
	h.entries = append(h.entries, en)
	if len(h.entries) > jumpHistoryMax {
		h.entries = slices.Delete(h.entries, 0, 1)
	}
	h.cur = len(h.entries) - 1
}

// registerNavHandlers registers navigate_back and navigate_forward.
func registerNavHandlers(h map[Action]handler) {
	h["navigate_back"] = navigateBack
	h["navigate_forward"] = navigateForward
}

// pushJump records the active caret as a place to return to. Goto line, search,
// bookmark jumps and opening a file call it before they move the caret. The
// untouched placeholder tab is not a place.
func (e *Editor) pushJump() {
	d := e.Active()
	if d == nil || e.pristineUntitled() {
		return
	}
	e.extras.jumps.push(jumpEntryOf(d))
}

// goToJump switches to the tab of en and puts the caret at its position. A
// place whose tab has been closed is skipped without an error.
func (e *Editor) goToJump(en jumpEntry) {
	i := e.indexOfDoc(en.doc)
	if i < 0 {
		return
	}
	e.switchTo(i)
	d := en.doc
	d.Cursor = d.Buffer.Clamp(en.pos)
	d.Sel = nil
	if a := d.Buffer.Clamp(en.anchor); en.selected && a != d.Cursor {
		d.Sel = &buffer.Selection{Anchor: a, Head: d.Cursor}
	}
	d.want = -1
	e.ensureVisibleForJump(d)
}

// navigateBack goes to the previous place. At the newest place it first keeps
// the current caret, so navigate_forward can come back to it. A place whose tab
// has been closed is skipped. With no earlier open place it does nothing.
func navigateBack(e *Editor, _ map[string]any) error {
	h := &e.extras.jumps
	if len(h.entries) == 0 {
		return nil
	}
	if h.cur == len(h.entries)-1 {
		if d := e.Active(); d != nil {
			h.push(jumpEntryOf(d))
		}
	}
	e.stepJump(-1)
	return nil
}

// navigateForward goes to the next place after a navigate_back. A place whose
// tab has been closed is skipped. At the newest place it does nothing.
func navigateForward(e *Editor, _ map[string]any) error {
	e.stepJump(1)
	return nil
}

// stepJump moves the history by dir, -1 back or +1 forward, to the nearest place
// whose tab is still open. It changes nothing when there is no such place.
func (e *Editor) stepJump(dir int) {
	h := &e.extras.jumps
	for i := h.cur + dir; i >= 0 && i < len(h.entries); i += dir {
		if e.indexOfDoc(h.entries[i].doc) >= 0 {
			h.cur = i
			e.goToJump(h.entries[i])
			return
		}
	}
}
