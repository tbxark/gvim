package find

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/space"
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
		x := space.PadX
		labels, gap := b.optionRow(w)
		for _, label := range labels {
			g.Put(x, r.opt, w, label, b.styles.bar)
			x += ansi.StringWidth(label) + gap
		}
	}
	if r.search >= 0 {
		b.drawField(g, r.search, searchLabel, &b.search, !b.inReplace, b.statusText())
	}
	if r.repl >= 0 {
		b.drawField(g, r.repl, replaceLabel, &b.repl, b.inReplace, "")
	}
}

// minFieldRoom is the narrowest input the bar keeps beside the status text. When the
// room is smaller, the status text is dropped and the input takes the row.
const minFieldRoom = 12

// labelCols is the width of the label column: the longest label and its gap, so the
// search and replacement inputs start at the same column.
var labelCols = ansi.StringWidth(replaceLabel) + 1

// fieldSpan is the columns of the input on a row w cells wide: it starts after the
// label and ends before status when status is shown. status is dropped, and the
// input runs to the right edge, when it would leave less than minFieldRoom cells.
func fieldSpan(w int, status string) (x, end int, showStatus bool) {
	x = space.PadX + labelCols
	if status != "" {
		if sx := w - ansi.StringWidth(status) - 1; sx > 0 && sx-1-x >= minFieldRoom {
			return x, sx - 1, true
		}
	}
	return x, w, false
}

// drawField paints one labelled field on row y. The label of the focused row
// takes the active style. status, when not empty, is right-aligned on the row
// and the field text stops before it. The text scrolls so that the caret stays in
// the room (see space.Window).
func (b *Bar) drawField(g *grid.Grid, y int, label string, f *textinput.Model, focused bool, status string) {
	w := g.Width()
	g.Fill(0, w, y, b.styles.bar)
	x, end, showStatus := fieldSpan(w, status)
	if showStatus {
		g.Put(end+1, y, w, status, b.styles.bar)
	}
	ls := b.styles.label
	if focused {
		ls = b.styles.active
	}
	g.Put(space.PadX, y, min(x, end), label, ls)
	shown, _ := space.Window(f.Value(), f.Position(), end-x)
	g.Put(x, y, end, shown, b.styles.field)
}

// Cursor returns the caret position in a frame of w by h cells. ok is false when
// the focused row is not drawn or the caret is off the right edge.
func (b *Bar) Cursor(w, h int) (x, y int, ok bool) {
	r := rowsFor(h, b.replaceOpen)
	f, row, status := &b.search, r.search, b.statusText()
	if b.inReplace {
		f, row, status = &b.repl, r.repl, ""
	}
	if row < 0 {
		return 0, 0, false
	}
	x0, end, _ := fieldSpan(w, status)
	_, col := space.Window(f.Value(), f.Position(), end-x0)
	x = x0 + col
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
	return checkbox(on) + optionLabels[i]
}

// checkbox is the box before an option name.
func checkbox(on bool) string {
	if on {
		return "[x] "
	}
	return "[ ] "
}

// optionName is the name of option i without its key hint: "Case Sensitive" for
// "Case Sensitive (Alt+C)".
func optionName(i int) string {
	name, _, _ := strings.Cut(optionLabels[i], " (")
	return name
}

// optionShort are the option names of a frame too narrow for the full names.
var optionShort = [numOptions]string{"Case", "Word", "Regex", "Confirm"}

// optionRow returns the labels of the options row and the blanks between them, for a
// frame w cells wide. Each label is a checkbox and a name, and the row starts after the
// left padding. The full labels with their key hints come first. A narrower frame drops
// the hints, then closes the gaps, and the narrowest uses the short names, so all four
// options stay on the row. Only a frame too narrow even for that gets a cut label, which
// ends with an ellipsis; the options it leaves out are the last ones.
func (b *Bar) optionRow(w int) (labels []string, gap int) {
	full := func(i int) string { return optionLabel(i, b.opts[i]) }
	named := func(i int) string { return checkbox(b.opts[i]) + optionName(i) }
	short := func(i int) string { return checkbox(b.opts[i]) + optionShort[i] }
	forms := []struct {
		label func(int) string
		gap   int
	}{
		{full, labelGap}, {named, labelGap}, {named, 2}, {named, 1},
		{short, labelGap}, {short, 2}, {short, 1},
	}
	for _, f := range forms {
		labels = labels[:0]
		total := space.PadX + f.gap*(numOptions-1)
		for i := range numOptions {
			l := f.label(i)
			labels = append(labels, l)
			total += ansi.StringWidth(l)
		}
		if total <= w {
			return labels, f.gap
		}
	}
	// Too narrow even for the short names: the options that fit stay, and the row ends
	// with an ellipsis when an option is cut or left out.
	labels = labels[:0]
	room := w - space.PadX
	for i := range numOptions {
		l := short(i)
		if lw := ansi.StringWidth(l); lw <= room {
			labels = append(labels, l)
			room -= lw + 1
			continue
		}
		if room > 1 {
			labels = append(labels, space.End(short(i), room))
		} else if n := len(labels); n > 0 {
			labels[n-1] = space.End(labels[n-1], ansi.StringWidth(labels[n-1])-1)
		}
		break
	}
	return labels, 1
}
