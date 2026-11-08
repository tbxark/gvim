package pluginpanel

import (
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/pluginapi"
)

// Interactive widgets (plugin API tier 1b): the text field, number, dropdown, radio and
// dualList controls, the wrapper kinds and page scrolling.
//
// The host owns the instance state of a keyed widget after the first edit: the text and its
// caret/selection, the number, the chosen option, the included set, the rebuilt list/tree
// rows and the checked tree keys. A spec update (or the plugin's own mutation) replaces the
// spec but never the state of a widget whose key is unchanged.

// panelOverlay is a transient region painted over the panel content after the tree: the
// option list of an open dropdown, or the completion candidates of a focused text field.
type panelOverlay struct {
	kind  string // "dropdown" | "completion"
	rect  Rect
	items []string
	sel   int
	key   string // the widget key the overlay belongs to
}

// evInteractive builds a widget event for the focused interactive widget.
func (p *Panel) evInteractive(name string, w pluginapi.Widget) pluginapi.WidgetEvent {
	e := pluginapi.WidgetEvent{Event: name, Widget: w.Key, Index: -1, CursorByte: -1}
	switch w.Kind {
	case pluginapi.WidgetText:
		e.Event, e.Value = "change", p.textValue(w)
		e.CursorByte = p.textCursor(w)
	case pluginapi.WidgetNumber:
		e.Event, e.Value = "change", formatNumber(w, p.numberValue(w))
	case pluginapi.WidgetDropdown, pluginapi.WidgetRadio:
		e.Event = "change"
		sel := p.selected(w)
		e.Index = sel
		if sel >= 0 && sel < len(w.Options) {
			e.Value = w.Options[sel]
		}
	case pluginapi.WidgetDualList:
		e.Event = "change"
		e.Included = includedList(p.includedSet(w), w.Options)
	}
	return e
}

// formatNumber renders a number widget's value the way its spec asks: an integer, a
// percentage, or a trimmed float.
func formatNumber(w pluginapi.Widget, v float64) string {
	switch {
	case w.Percent:
		return strconv.FormatFloat(v*100, 'f', -1, 64) + "%"
	case w.Integer:
		return strconv.FormatInt(int64(math.Round(v)), 10)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

// includedList renders the included set in option order.
func includedList(set map[string]bool, options []string) []string {
	out := make([]string, 0, len(set))
	for _, o := range options {
		if set[o] {
			out = append(out, o)
		}
	}
	return out
}

// ---- text editing ----

// byte offsets and runes.
func prevRune(s string, i int) int {
	if i <= 0 {
		return 0
	}
	_, n := lastRune(s[:i])
	return i - n
}

func nextRune(s string, i int) int {
	if i >= len(s) {
		return len(s)
	}
	_, n := firstRune(s[i:])
	return i + n
}

func lastRune(s string) (rune, int) {
	r := []rune(s)
	if len(r) == 0 {
		return 0, 0
	}
	return r[len(r)-1], len(string(r[len(r)-1]))
}

func firstRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 0
}

// lineColOf returns the 0-based line and the byte column of the offset.
func lineColOf(text string, off int) (line, col int) {
	off = clamp(off, 0, len(text))
	for i := 0; i < off; i++ {
		if text[i] == '\n' {
			line++
			col = 0
			continue
		}
		col++
	}
	return line, col
}

// offsetOf returns the byte offset of the 0-based line and byte column.
func offsetOf(text string, line, col int) int {
	off := 0
	for l := 0; l < line; l++ {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			return len(text)
		}
		off += i + 1
	}
	end := strings.IndexByte(text[off:], '\n')
	if end < 0 {
		end = len(text) - off
	}
	return off + min(col, end)
}

// lineCount is the number of lines of the text.
func lineCount(text string) int { return strings.Count(text, "\n") + 1 }

// insertText types s into the field: it replaces the selection (when there is one) and
// returns the new caret.
func (p *Panel) insertText(w pluginapi.Widget, typed string) int {
	text := p.textValue(w)
	start, end := p.textSelection(w)
	if start == end {
		start = p.textCursor(w)
		end = start
	}
	next := text[:start] + typed + text[end:]
	caret := start + len(typed)
	p.setText(w, next, caret)
	p.setSelection(w, caret, caret)
	return caret
}

// deleteRange deletes the selection, or one rune back (back) or forward of the caret.
func (p *Panel) deleteRange(w pluginapi.Widget, back bool) {
	text := p.textValue(w)
	start, end := p.textSelection(w)
	if start == end {
		if back {
			start = prevRune(text, p.textCursor(w))
			end = p.textCursor(w)
		} else {
			start = p.textCursor(w)
			end = nextRune(text, start)
		}
	}
	next := text[:start] + text[end:]
	p.setText(w, next, start)
	p.setSelection(w, start, start)
}

// moveCaret moves the text caret by one rune (left/right) or one line (up/down), extending
// the selection when extend is set.
func (p *Panel) moveCaret(w pluginapi.Widget, name string, extend bool) {
	text := p.textValue(w)
	caret := p.textCursor(w)
	line, col := lineColOf(text, caret)
	switch name {
	case "left":
		caret = prevRune(text, caret)
	case "right":
		caret = nextRune(text, caret)
	case "up":
		if line == 0 {
			return
		}
		caret = offsetOf(text, line-1, col)
	case "down":
		if line >= lineCount(text)-1 {
			return
		}
		caret = offsetOf(text, line+1, col)
	case "home":
		caret = offsetOf(text, line, 0)
	case "end":
		caret = offsetOf(text, line, math.MaxInt)
	}
	if extend {
		if _, selEnd := p.textSelection(w); selEnd == 0 {
			p.setSelection(w, p.textCursor(w), caret)
		} else {
			start, _ := p.textSelection(w)
			p.setSelection(w, start, caret)
		}
	} else {
		p.setSelection(w, caret, caret)
	}
	p.setText(w, text, caret)
}

// editFocusedText applies one editing key or typed text to a focused text widget.
func (p *Panel) editFocusedText(w pluginapi.Widget, k Key) (Result, bool) {
	if w.ReadOnly {
		switch k.Name {
		case "left", "right", "up", "down", "home", "end":
			p.moveCaret(w, k.Name, k.Shift)
			return Result{Handled: true}, true
		}
		return Result{Handled: true}, true
	}
	switch k.Name {
	case "left", "right", "up", "down", "home", "end":
		p.moveCaret(w, k.Name, k.Shift)
		return Result{Handled: true}, true
	case "backspace", "delete":
		p.deleteRange(w, k.Name == "backspace")
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("change", w)}}, true
	case "space":
		if !k.Ctrl && !k.Alt {
			p.insertText(w, " ")
			return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("change", w)}}, true
		}
	case "enter":
		if w.Rows > 1 {
			p.insertText(w, "\n")
			return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("change", w)}}, true
		}
		e := p.evInteractive("change", w)
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{e}}, true
	}
	if k.Text != "" && !k.Ctrl && !k.Alt {
		p.insertText(w, k.Text)
		return Result{Handled: true, Events: []pluginapi.WidgetEvent{p.evInteractive("change", w)}}, true
	}
	return Result{}, false
}

// ---- dropdown, radio, dual list ----

// optionIndex returns the effective selected option of a dropdown/radio widget.
func (p *Panel) optionIndex(w pluginapi.Widget) int {
	sel := p.selected(w)
	if sel < 0 {
		sel = 0
	}
	return clamp(sel, 0, max(len(w.Options)-1, 0))
}

// changeOption selects option i of a dropdown/radio widget and returns the change event.
func (p *Panel) changeOption(w pluginapi.Widget, i int) pluginapi.WidgetEvent {
	p.setSelected(w, clamp(i, 0, max(len(w.Options)-1, 0)))
	return p.evInteractive("change", w)
}

// toggleIncluded adds or removes value from a dual list's included set.
func (p *Panel) toggleIncluded(w pluginapi.Widget, value string) pluginapi.WidgetEvent {
	set := p.includedSet(w)
	if set[value] {
		delete(set, value)
	} else {
		set[value] = true
	}
	p.setIncluded(w, includedList(set, w.Options))
	return p.evInteractive("change", w)
}

// ---- page scrolling ----

// viewportRows is the body height of the panel in the last draw.
func (p *Panel) viewportRows() int {
	if p.framed {
		return max(p.rect.H-2, 0)
	}
	return max(p.rect.H, 0)
}

// scrollPage moves the page window by delta content rows, clamped to the content.
func (p *Panel) scrollPage(delta int) bool {
	if !p.page {
		return false
	}
	rows := p.viewportRows()
	top := clamp(p.pageTop+delta, 0, max(p.contentRows-rows, 0))
	if top == p.pageTop {
		return true
	}
	p.pageTop = top
	return true
}

// pageScrollKey handles a movement key that no focused widget took in a page panel.
func (p *Panel) pageScrollKey(k Key) (Result, bool) {
	if !p.page {
		return Result{}, false
	}
	switch k.Name {
	case "up", "ctrl+p":
		p.scrollPage(-1)
		return Result{Handled: true}, true
	case "down", "ctrl+n":
		p.scrollPage(1)
		return Result{Handled: true}, true
	case "pageup":
		p.scrollPage(-max(p.viewportRows()-1, 1))
		return Result{Handled: true}, true
	case "pagedown":
		p.scrollPage(max(p.viewportRows()-1, 1))
		return Result{Handled: true}, true
	case "home":
		p.pageTop = 0
		return Result{Handled: true}, true
	case "end":
		p.scrollPage(1 << 30)
		return Result{Handled: true}, true
	}
	return Result{}, false
}

// ---- painting ----

// paintText draws a text field: single-line with a horizontal window, or the visible lines
// of a multiline field, with the caret and selection.
func (pt *painter) textField(node pluginapi.Widget, x, y, wd, h int) {
	p := pt.p
	base := p.styleOf(node)
	text := p.textValue(node)
	cursor := p.textCursor(node)
	selStart, selEnd := p.textSelection(node)
	focused := pt.ownsFocus(node)
	if focused {
		base = p.focusedStyle(base)
	}
	field := wd
	if node.FieldWidth > 0 && !node.FullWidth {
		field = min(node.FieldWidth+2, wd)
	}
	rows := h
	if node.Rows > 1 {
		rows = min(max(node.Rows, 1), h)
	}
	if node.MaxRows > 0 {
		lines := lineCount(text)
		rows = min(max(min(lines+1, max(node.MinRows, 1)), 1), max(node.MaxRows, 1))
		rows = min(rows, h)
	}
	if rows <= 1 {
		pt.textSingleLine(node, x, y, field, base, text, cursor, selStart, selEnd, focused)
	} else {
		pt.textMultiLine(node, x, y, field, rows, base, text, cursor, selStart, selEnd, focused)
	}
	if focused && p.completionsOpen(node) {
		items := p.completionsOf(node)
		build := make([]string, len(items))
		for i, it := range items {
			build[i] = " " + it
		}
		p.overlays = append(p.overlays, panelOverlay{kind: "completion", rect: Rect{X: x, Y: y + rows, W: min(max(joinWidth(build)+2, 10), max(wd, 10)), H: min(len(items), 8) + 2}, items: build, sel: p.completionSelOf(node), key: node.Key})
	}
}

// textSingleLine paints one row of editable text with a window that keeps the caret in view.
func (pt *painter) textSingleLine(node pluginapi.Widget, x, y, field int, base grid.Style, text string, cursor, selStart, selEnd int, focused bool) {
	g := pt.g
	inner := max(field-2, 1)
	shown, caretCol := windowText(text, cursor, inner)
	// Selection styling on the visible slice, as a second pass over the cells.
	g.Put(x, y, x+field, "[", base)
	g.Fill(x+1, x+field-1, y, pt.p.chrome.Box)
	if focused {
		g.Fill(x+1, x+field-1, y, pt.p.chrome.Idle)
	}
	g.Put(x+1, y, x+field-1, shown, base)
	if selStart != selEnd {
		selCols := selectionColumns(text, shown, cursor, selStart, selEnd)
		for _, c := range selCols {
			if cx := x + 1 + c; cx < x+field-1 {
				g.Set(cx, y, cellAt(shown, c), pt.p.chrome.Selected)
			}
		}
	}
	if focused {
		cx := x + 1 + caretCol
		if cx < x+field-1 {
			ch := " "
			if cx-x-1 < len(shown) {
				ch = string([]rune(shown)[max(caretCol, 0)])
				if ch == "" {
					ch = " "
				}
			}
			g.Set(cx, y, ch, pt.p.focusedStyle(pt.p.chrome.Box))
		}
	}
	g.Put(x+field-1, y, x+field, "]", base)
	pt.addHit(node, x, y, field, 1, hitText)
}

// textMultiLine paints the visible lines of a multiline field.
func (pt *painter) textMultiLine(node pluginapi.Widget, x, y, field, rows int, base grid.Style, text string, cursor, selStart, selEnd int, focused bool) {
	g := pt.g
	line, _ := lineColOf(text, cursor)
	top := clamp(line-rows+1, 0, max(lineCount(text)-rows, 0))
	if in := pt.p.inst[node.Key]; in != nil {
		top = clamp(in.textTop, 0, max(lineCount(text)-rows, 0))
		in.textTop = clamp(line-rows/2, 0, max(lineCount(text)-rows, 0))
	}
	lines := strings.Split(text, "\n")
	for r := 0; r < rows; r++ {
		ln := top + r
		if ln >= len(lines) {
			g.Fill(x, x+field, y+r, pt.p.chrome.Box)
			continue
		}
		rowStyle := pt.p.chrome.Box
		if focused {
			rowStyle = pt.p.chrome.Idle
		}
		g.Fill(x, x+field, y+r, rowStyle)
		s := elide(lines[ln], field)
		g.Put(x, y+r, x+field, s, base)
	}
	if focused {
		// Caret block: the line and column of the caret, when the line is visible.
		_, col := lineColOf(text, cursor)
		if line >= top && line < top+rows {
			cx := x + min(col, field-1)
			ch := " "
			if line < len(lines) && col < len(lines[line]) {
				ch = string([]rune(lines[line][col:])[0])
			}
			g.Set(cx, y+line-top, ch, pt.p.focusedStyle(pt.p.chrome.Box))
		}
	}
	pt.addHit(node, x, y, field, rows, hitText)
}

// windowText returns the slice of text shown in an inner of cells with the caret in view,
// and the caret's display column in that slice.
func windowText(text string, cursor, inner int) (string, int) {
	cursor = clamp(cursor, 0, len(text))
	before := text[:cursor]
	width := ansi.StringWidth(before)
	start := 0
	for width > inner-1 && start < len(before) {
		r, n := firstRune(before[start:])
		width -= ansi.StringWidth(string(r))
		start += n
	}
	shown := text[start:]
	caret := ansi.StringWidth(text[start:cursor])
	return shown, caret
}

// selectionColumns returns the display columns of the selection inside shown.
func selectionColumns(text, shown string, cursor, selStart, selEnd int) []int {
	start := len(text) - len(shown)
	from := max(selStart-start, 0)
	to := min(selEnd-start, len(shown))
	if from >= to {
		return nil
	}
	a := ansi.StringWidth(shown[:from])
	b := ansi.StringWidth(shown[:to])
	out := make([]int, 0, b-a)
	for c := a; c < b; c++ {
		out = append(out, c)
	}
	return out
}

// cellAt returns the grapheme at display column c of s, a space when past its end.
func cellAt(s string, c int) string {
	col := 0
	for rest := s; rest != ""; {
		gr, w := ansi.FirstGraphemeCluster(rest, ansi.GraphemeWidth)
		if gr == "" {
			break
		}
		rest = rest[len(gr):]
		if col+w > c {
			return gr
		}
		col += w
	}
	return " "
}

// ownsFocus reports whether the widget is the focused one, advancing the focus counter in
// the paint pass the same way the focus ring was collected.
func (pt *painter) ownsFocus(node pluginapi.Widget) bool {
	if !pt.p.acceptsFocus(node) {
		return false
	}
	focused := pt.fi == pt.p.focus && pt.p.focused
	pt.fi++
	return focused
}

// addHit records an interactive region and its focus index (the caller passes the index that
// ownsFocus consumed, or -1).
func (pt *painter) addHit(node pluginapi.Widget, x, y, w, h int, kind hitKind) {
	pt.p.hits = append(pt.p.hits, hit{r: Rect{X: x, Y: y, W: w, H: h}, kind: kind, focusIdx: pt.fi - 1, path: "", index: -1})
}

// paintNumber draws a number control: its optional label and the value cell.
func (pt *painter) numberField(node pluginapi.Widget, x, y, wd int) {
	p := pt.p
	base := p.styleOf(node)
	focused := pt.ownsFocus(node)
	if focused {
		base = p.focusedStyle(base)
	}
	label := ""
	if node.Label != "" {
		label = node.Label + " "
	}
	text := label + "[ " + formatNumber(node, p.numberValue(node)) + " ]"
	pt.g.Put(x, y, x+wd, elide(text, wd), base)
	pt.p.hits = append(pt.p.hits, hit{r: Rect{X: x, Y: y, W: min(ansi.StringWidth(text), wd), H: 1}, kind: hitNumber, focusIdx: pt.fi - 1, path: "", index: -1})
}

// paintDropdown draws a dropdown: the value cell, and when open the option list overlay.
func (pt *painter) dropdown(node pluginapi.Widget, x, y, wd int) {
	p := pt.p
	base := p.styleOf(node)
	focused := pt.ownsFocus(node)
	if focused {
		base = p.focusedStyle(base)
	}
	sel := p.optionIndex(node)
	value := ""
	if sel < len(node.Options) {
		value = node.Options[sel]
	}
	open := p.dropdownOpen(node)
	arrow := "▼"
	if open {
		arrow = "▲"
	}
	text := " " + value + " " + arrow + " "
	pt.g.Put(x, y, x+wd, elide("["+strings.TrimSpace(text)+" ]", wd), base)
	pt.p.hits = append(pt.p.hits, hit{r: Rect{X: x, Y: y, W: min(ansi.StringWidth(text)+2, wd), H: 1}, kind: hitDropdown, focusIdx: pt.fi - 1, path: "", index: -1})
	if open && focused {
		items := make([]string, len(node.Options))
		for i, o := range node.Options {
			items[i] = " " + o
		}
		p.overlays = append(p.overlays, panelOverlay{kind: "dropdown", rect: Rect{X: x, Y: y + 1, W: min(max(joinWidth(items), 8), max(wd, 8)), H: min(len(items), 8) + 2}, items: items, sel: sel, key: node.Key})
	}
}

// paintRadio draws a radio group: one row per option, the selected one marked.
func (pt *painter) radio(node pluginapi.Widget, x, y, wd, h int) {
	p := pt.p
	base := p.styleOf(node)
	focused := pt.ownsFocus(node)
	sel := p.optionIndex(node)
	for i, o := range node.Options {
		if i >= h {
			break
		}
		st := base
		if focused && i == sel {
			st = p.focusedStyle(base)
		}
		mark := "( )"
		if i == sel {
			mark = "(o)"
		}
		pt.g.Put(x, y+i, x+wd, elide(mark+" "+o, wd), st)
		p.hits = append(p.hits, hit{r: Rect{X: x, Y: y + i, W: min(ansi.StringWidth(mark+" "+o), wd), H: 1}, kind: hitRadio, focusIdx: pt.fi - 1, path: "", index: i})
	}
}

// paintDualList draws the dual list as one column of options with the included ones marked
// (the tier-1b simplification of Fresh's two columns; documented).
func (pt *painter) dualList(node pluginapi.Widget, x, y, wd, h int) {
	p := pt.p
	base := p.styleOf(node)
	focused := pt.ownsFocus(node)
	sel := p.selected(node)
	if sel < 0 {
		sel = 0
	}
	set := p.includedSet(node)
	rows := min(len(node.Options), h)
	top := windowTop(sel, rows, len(node.Options))
	for k := 0; k < rows; k++ {
		i := top + k
		st := base
		if focused && i == sel {
			st = p.focusedStyle(base)
		}
		mark := "[ ]"
		if set[node.Options[i]] {
			mark = "[x]"
		}
		text := mark + " " + node.Options[i]
		pt.g.Fill(x, x+wd, y+k, st)
		pt.g.Put(x, y+k, x+wd, elide(text, wd), st)
		p.hits = append(p.hits, hit{r: Rect{X: x, Y: y + k, W: wd, H: 1}, kind: hitDualRow, focusIdx: pt.fi - 1, path: "", index: i})
	}
}

// paintLabeledSection draws a rounded box with the label on its top rule, and the wrapped
// child inside.
func (pt *painter) labeledSection(node pluginapi.Widget, x, y, wd, h int) {
	if wd < 4 || h < 3 {
		return
	}
	pt.p.chrome.PaintBox(pt.g, x, y, wd, h, node.Title)
	if len(node.Children) == 0 {
		return
	}
	pt.node(node.Children[0], x+1, y+1, wd-2, h-2)
}

// paintPopup draws the wrapped child in a card at its own place (the tier-1b form: the
// anchor is the node's position, not an explicit point).
func (pt *painter) paintPopup(node pluginapi.Widget, x, y, wd, h int) {
	if len(node.Children) == 0 || wd < 4 || h < 3 {
		return
	}
	pt.p.chrome.PaintBox(pt.g, x, y, wd, h, node.Title)
	pt.node(node.Children[0], x+1, y+1, wd-2, h-2)
}

// paintWindowEmbed draws placeholder rows: a panel cannot embed a live editor pane.
func (pt *painter) paintWindowEmbed(node pluginapi.Widget, x, y, wd, h int) {
	if wd < 4 || h < 2 {
		return
	}
	pt.p.chrome.PaintBox(pt.g, x, y, wd, h, node.Title)
	label := "window " + strconv.FormatUint(uint64(node.Window), 10)
	if node.Window == 0 {
		label = "no window"
	}
	for r := 1; r < h-1; r++ {
		pt.g.Put(x+2, y+r, x+wd-1, elide(label, wd-3), pt.p.chrome.Muted)
	}
}

// paintOverlays paints the transient regions of the last draw (an open dropdown's options,
// a text field's completion candidates) over the panel content.
func (p *Panel) paintOverlays(g *grid.Grid) {
	for _, ov := range p.overlays {
		b := ov.rect
		if b.W < 4 || b.H < 2 {
			continue
		}
		p.chrome.PaintBox(g, b.X, b.Y, b.W, b.H, "")
		for k := 0; k < b.H-2 && k < len(ov.items); k++ {
			st := p.chrome.Box
			if k == ov.sel {
				st = p.chrome.Selected
			}
			g.Fill(b.X+1, b.X+b.W-1, b.Y+1+k, st)
			g.Put(b.X+2, b.Y+1+k, b.X+b.W-1, elide(ov.items[k], b.W-3), st)
			p.hits = append(p.hits, hit{r: Rect{X: b.X, Y: b.Y + 1 + k, W: b.W, H: 1}, kind: hitOverlayItem, focusIdx: -1, index: k, overlay: ov.kind, overlayKey: ov.key})
		}
	}
}

// joinWidth is the widest width of the lines.
func joinWidth(lines []string) int {
	n := 0
	for _, l := range lines {
		n = max(n, ansi.StringWidth(l))
	}
	return n
}
