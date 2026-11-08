package document

import (
	"github.com/tbxark/gvim/buffer"
)

// jumpEntry is one place in the navigation history: a tab, a caret position and,
// when text was selected there, the selection anchor.
type jumpEntry struct {
	Doc      *Document
	Pos      buffer.Pos
	Anchor   buffer.Pos
	Selected bool
}

// jumpEntryOf is the place d is at now.
func jumpEntryOf(d *Document) jumpEntry {
	en := jumpEntry{Doc: d, Pos: d.Cursor}
	if d.HasSelection() {
		en.Anchor, en.Selected = d.Sel.Anchor, true
	}
	return en
}
