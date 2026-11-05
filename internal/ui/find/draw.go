package find

import (
	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
)

// rows are the rows of the bar in a frame, or -1 for a row that is not drawn.
type rows struct {
	opt, search, repl int
}

// rowsFor places the bar in a frame h rows high. The options row is above the
// search row, and the replacement row, when shown, is the bottom row. At least
// row 2 is kept, so the bar never covers the menu bar or the tab strip.
func rowsFor(h int, replaceOpen bool) rows {
	in := h - 1
	if replaceOpen {
		in = h - 2
	}
	opt := max(in-1, 2)
	r := rows{opt: opt, search: in, repl: -1}
	if in <= opt {
		r.search = -1
	}
	if replaceOpen && h-1 > opt {
		r.repl = h - 1
	}
	return r
}

// Draw paints the bar over the bottom rows of g. It covers only the cells it
// draws: the options row, the search row, and the replacement row when shown.
func (b *Bar) Draw(g *grid.Grid) {
	r := rowsFor(g.Height(), b.replaceOpen)
	w := g.Width()
	if r.opt >= 0 {
		g.Fill(0, w, r.opt, b.styles.bar)
		x := 1
		for i := range numOptions {
			label := optionLabel(i, b.opts[i])
			g.Put(x, r.opt, w, label, b.styles.bar)
			x += ansi.StringWidth(label) + labelGap
		}
	}
	if r.search >= 0 {
		b.drawField(g, r.search, searchLabel, &b.search, !b.inReplace, b.statusText())
	}
	if r.repl >= 0 {
		b.drawField(g, r.repl, replaceLabel, &b.repl, b.inReplace, "")
	}
}

// drawField paints one labelled field on row y. The label of the focused row
// takes the active style. status, when not empty, is right-aligned on the row
// and the field text stops before it.
func (b *Bar) drawField(g *grid.Grid, y int, label string, f *textinput.Model, focused bool, status string) {
	w := g.Width()
	g.Fill(0, w, y, b.styles.bar)
	end := w
	if status != "" {
		x := w - ansi.StringWidth(status) - 1
		if x > 0 {
			g.Put(x, y, w, status, b.styles.bar)
			end = x - 1
		}
	}
	ls := b.styles.label
	if focused {
		ls = b.styles.active
	}
	prefix := label + " "
	x := g.Put(0, y, end, prefix, ls)
	g.Put(x, y, end, f.Value(), b.styles.field)
}

// Cursor returns the caret position in a frame of w by h cells. ok is false when
// the focused row is not drawn or the caret is off the right edge.
func (b *Bar) Cursor(w, h int) (x, y int, ok bool) {
	r := rowsFor(h, b.replaceOpen)
	f, row, label := &b.search, r.search, searchLabel
	if b.inReplace {
		f, row, label = &b.repl, r.repl, replaceLabel
	}
	if row < 0 {
		return 0, 0, false
	}
	runes := []rune(f.Value())
	pos := min(f.Position(), len(runes))
	x = ansi.StringWidth(label+" ") + ansi.StringWidth(string(runes[:pos]))
	if x >= w {
		return 0, 0, false
	}
	return x, row, true
}

// statusText is the text at the right of the search row: the last option
// change while it is fresh, otherwise the match status.
func (b *Bar) statusText() string {
	if b.notice != "" {
		return b.notice
	}
	return b.status
}

// optionLabel is the label of option i with its checkbox.
func optionLabel(i int, on bool) string {
	box := "[ ] "
	if on {
		box = "[x] "
	}
	return box + optionLabels[i]
}
