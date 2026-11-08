package find

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// rows are the rows of the bar in a frame. top and bottom are the card's borders,
// opt is the options row and search and repl the field rows; a row is -1 when the
// frame has no room for the whole card.
type rows struct {
	top, opt, search, repl, bottom int
}

// rowsFor places the bar in a frame h rows high. The bar is a card docked to the last
// row: a title border, the options row, the search row, the replacement row when it is
// open, and the hints on the bottom border. The card never covers the menu bar or the
// tab strip (rows 0 and 1); a frame too short for that shows no bar at all.
func rowsFor(h int, replaceOpen bool) rows {
	r := rows{top: -1, opt: -1, search: -1, repl: -1, bottom: -1}
	if replaceOpen {
		r.bottom, r.repl, r.search, r.opt, r.top = h-1, h-2, h-3, h-4, h-5
	} else {
		r.bottom, r.search, r.opt, r.top = h-1, h-2, h-3, h-4
	}
	if r.top < 2 {
		return rows{top: -1, opt: -1, search: -1, repl: -1, bottom: -1}
	}
	return r
}

// cardColumns returns the first and last border column of the bar's card in a frame w
// cells wide. The card keeps the same edge margin as every other floating surface.
func cardColumns(w int) (left, right int) {
	m := space.Margin(w)
	return m, w - 1 - m
}

// title is the card's title: what the bar does at the moment.
func (b *Bar) title() string {
	if b.replaceOpen {
		return "Find and Replace"
	}
	return "Find"
}

// hint is the text on the card's bottom border: the keys the bar answers.
func (b *Bar) hint() string {
	if b.replaceOpen {
		return "Enter replace · Tab field · Esc close"
	}
	return "Enter find · Tab replace · Esc close"
}

// Draw paints the bar over the last rows of g. The bar is one card: the title border,
// the options row, the search row, the replacement row when shown, and the key hints
// on the bottom border. A frame with no room for the card paints nothing.
func (b *Bar) Draw(g *grid.Grid) {
	r := rowsFor(g.Height(), b.replaceOpen)
	if r.top < 0 {
		return
	}
	left, right := cardColumns(g.Width())
	if right <= left+1 {
		return
	}
	boxW, boxH := right-left+1, r.bottom-r.top+1
	b.styles.ov.PaintBox(g, left, r.top, boxW, boxH, b.title())
	x0, end := left+1+space.PadX, right-space.PadX
	if r.opt >= 0 {
		b.drawOptions(g, r.opt, x0, end)
	}
	if r.search >= 0 {
		b.drawField(g, r.search, searchLabel, &b.search, !b.inReplace, b.statusText(), x0, end)
	}
	if r.repl >= 0 {
		b.drawField(g, r.repl, replaceLabel, &b.repl, b.inReplace, "", x0, end)
	}
	b.styles.ov.PaintFooter(g, left, r.bottom, boxW, b.hint())
}

// drawOptions writes the option checkboxes on row y between x0 and end.
func (b *Bar) drawOptions(g *grid.Grid, y, x0, end int) {
	labels, gap := b.optionRow(end - x0)
	x := x0
	for _, label := range labels {
		g.Put(x, y, end, label, b.styles.label)
		x += ansi.StringWidth(label) + gap
	}
}

// minFieldRoom is the narrowest input the bar keeps beside the status text. When the
// room is smaller, the status text is dropped and the input takes the row.
const minFieldRoom = 12

// labelCols is the width of the label column: the longest label and its gap, so the
// search and replacement inputs start at the same column.
var labelCols = ansi.StringWidth(replaceLabel) + 1

// fieldSpan is the columns of the input on a row that runs from x0 to end: it starts
// after the label and ends before status when status is shown. status is dropped, and
// the input runs to end, when it would leave less than minFieldRoom cells.
func fieldSpan(x0, end int, status string) (x, fieldEnd int, showStatus bool) {
	x = x0 + labelCols
	if status != "" {
		if sx := end - ansi.StringWidth(status); sx > 0 && sx-x >= minFieldRoom {
			return x, sx - 1, true
		}
	}
	return x, end, false
}

// drawField paints one labelled field on row y. The label of the focused row takes the
// active style, and the field is the raised input row of the card. status, when not
// empty, is right-aligned on the row and the field stops before it. The text scrolls
// so that the caret stays in the room (see space.Window).
func (b *Bar) drawField(g *grid.Grid, y int, label string, f *textinput.Model, focused bool, status string, x0, end int) {
	x, fieldEnd, showStatus := fieldSpan(x0, end, status)
	if showStatus {
		g.Put(fieldEnd+1, y, end, status, b.styles.status)
	}
	ls := b.styles.label
	if focused {
		ls = b.styles.active
	}
	g.Put(x0, y, min(x, end), label, ls)
	g.Fill(x, fieldEnd, y, b.styles.field)
	shown, _ := space.Window(f.Value(), f.Position(), fieldEnd-x)
	g.Put(x, y, fieldEnd, shown, b.styles.field)
}

// Cursor returns the caret position in a frame of w by h cells. ok is false when the
// focused row is not drawn or the caret is off the right edge.
func (b *Bar) Cursor(w, h int) (x, y int, ok bool) {
	r := rowsFor(h, b.replaceOpen)
	f, row, status := &b.search, r.search, b.statusText()
	if b.inReplace {
		f, row, status = &b.repl, r.repl, ""
	}
	if row < 0 {
		return 0, 0, false
	}
	left, right := cardColumns(w)
	x0, end := left+1+space.PadX, right-space.PadX
	start, fieldEnd, _ := fieldSpan(x0, end, status)
	_, col := space.Window(f.Value(), f.Position(), fieldEnd-start)
	x = start + col
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
// row w cells wide. Each label is a checkbox and a name. The full labels with their
// key hints come first. A narrower frame drops the hints, then closes the gaps, and the
// narrowest uses the short names, so all four options stay on the row. Only a frame too
// narrow even for that gets a cut label, which ends with an ellipsis; the options it
// leaves out are the last ones.
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
		total := f.gap * (numOptions - 1)
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
	room := w
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
