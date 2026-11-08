package find

import (
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/space"
)

// optionAt returns the option whose label covers column x of the options row of a w
// cells wide frame, laid out as Draw lays it out.
func (b *Bar) optionAt(x, w int) (int, bool) {
	labels, gap := b.optionRow(w)
	at := space.PadX
	for i, label := range labels {
		lw := ansi.StringWidth(label)
		if x >= at && x < at+lw {
			return i, true
		}
		at += lw + gap
	}
	return 0, false
}

// Covers reports whether row y of a frame h rows high is one of the rows the bar draws
// at the bottom: the options row, the search row and, when it is shown, the
// replacement row.
func (b *Bar) Covers(y, h int) bool {
	r := rowsFor(h, b.replaceOpen)
	return y >= 0 && (y == r.opt || y == r.search || y == r.repl)
}

// Click handles a left press at column x, row y of a w by h frame. A press on an option
// label flips that option, and a press on the search or the replacement row moves the
// keys to that row. It reports whether the press landed on the bar. While query-replace
// asks about each match the bar takes the press and changes nothing.
func (b *Bar) Click(x, y, w, h int) bool {
	if !b.Covers(y, h) || x < 0 || x >= w {
		return false
	}
	if b.qr != nil {
		return true
	}
	r := rowsFor(h, b.replaceOpen)
	switch y {
	case r.opt:
		if i, ok := b.optionAt(x, w); ok {
			b.toggle(i)
		}
	case r.search:
		b.inReplace = false
		b.prefilled = false
		b.focusRow()
	case r.repl:
		b.inReplace = true
		b.prefilled = false
		b.focusRow()
	}
	return true
}

// Rebase makes the caret and selection of the document the ones Esc puts back, after
// something other than the bar moved them, such as a click in the text. The next
// search starts from the caret.
func (b *Bar) Rebase() {
	b.moved, b.matched = false, false
	b.orig = b.snapshot()
	b.anchor = b.ed.Cursor()
}
