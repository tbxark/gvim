package ui

import (
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// listMaxWidth and listMaxHeight cap the box of a listView. The box is centred.
const (
	listMaxWidth  = 76
	listMaxHeight = 28
)

// listWheelRows is the number of rows one wheel notch moves the highlight.
const listWheelRows = 3

// listDefaultPage is the number of item rows a listView assumes before its first
// draw, when it does not know the frame size yet.
const listDefaultPage = 8

// listHint is the row under the items. It names the keys the list takes.
const listHint = "Type to filter, Up/Down move, Enter run, Esc close"

// listRow is one row of a listView. A header row names a group and cannot be
// chosen. left is the main text, such as an action or a setting, and right is
// the secondary text drawn at the right edge. value is what choosing the row
// means to the owner of the list, such as a preset name.
type listRow struct {
	header bool
	left   string
	right  string
	value  string
}

// listView is a titled, filterable, scrollable list in a centred box. Typed text
// filters the rows, and a row keeps its group header while one of its rows
// matches. Up and Down move the highlight over the items, skipping headers,
// PageUp and PageDown move a page, and Home and End go to the first and last
// item. Enter and a left click choose the highlighted row through run. Esc is
// not seen here: the root pops the overlay on Esc.
type listView struct {
	title  string
	empty  string // the text shown when no row matches the filter
	rows   []listRow
	shown  []int  // indexes into rows that pass the filter, headers included
	filter []rune // the filter text
	sel    int    // the highlight, an index into shown
	top    int    // the first index into shown on screen
	page   int    // the item rows on screen at the last draw
	active bool
	chrome style.Overlay
	run    func(v *listView, r listRow) tea.Cmd // nil: choosing a row does nothing
	box    rect                                 // the box at the last draw
}

// newListView returns an open list with rows, titled title. run is called when a
// row is chosen, and it closes the list by clearing active when it means to.
func newListView(title string, rows []listRow, chrome style.Overlay, run func(*listView, listRow) tea.Cmd) *listView {
	v := &listView{
		title:  title,
		empty:  "No matches",
		rows:   rows,
		active: true,
		chrome: chrome,
		run:    run,
	}
	v.recompute()
	return v
}

// listBox returns the box of a list in a w by h frame, and the number of item rows
// it holds. The title is on the top border and the hint on the bottom one, so the
// box has a filter row and the items inside them. An empty box means the frame is
// too small to show the list.
func listBox(w, h int) (rect, int) {
	r := centred(w, h, listMaxWidth, listMaxHeight)
	if r.w == 0 {
		return rect{}, 0
	}
	return r, r.h - 3
}

// centred returns a box of at most bw by bh cells, centred in a w by h frame and
// kept two cells inside each edge. A frame too small for a box with a title row and
// one body row gives an empty rect.
func centred(w, h, bw, bh int) rect {
	bw, bh = min(bw, w-4), min(bh, h-2)
	if bw < 12 || bh < 3 {
		return rect{}
	}
	return rect{x: (w - bw) / 2, y: (h - bh) / 2, w: bw, h: bh}
}

// recompute rebuilds the shown rows from the filter. The highlight moves to the
// first item and the window to the top.
func (v *listView) recompute() {
	needle := strings.ToLower(string(v.filter))
	v.shown = v.shown[:0]
	head := -1
	for i, r := range v.rows {
		if r.header {
			head = i
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(r.left+" "+r.right), needle) {
			continue
		}
		if head >= 0 {
			v.shown = append(v.shown, head)
			head = -1
		}
		v.shown = append(v.shown, i)
	}
	v.sel, v.top = v.firstItem(), 0
}

// firstItem is the index in shown of the first item, or 0 when there is none.
func (v *listView) firstItem() int {
	for k, i := range v.shown {
		if !v.rows[i].header {
			return k
		}
	}
	return 0
}

// selected returns the highlighted row, and false when no item is highlighted.
func (v *listView) selected() (listRow, bool) {
	if v.sel < 0 || v.sel >= len(v.shown) {
		return listRow{}, false
	}
	r := v.rows[v.shown[v.sel]]
	return r, !r.header
}

// update handles one key or mouse message and returns the command the choice
// starts, if any.
func (v *listView) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return v.key(tea.Key(msg))
	case tea.PasteMsg:
		v.typeText(msg.Content)
	case tea.MouseClickMsg:
		m := tea.Mouse(msg)
		if k, ok := v.rowAt(m.X, m.Y); ok && m.Button == tea.MouseLeft {
			v.sel = k
			return v.chooseSelected()
		}
	case tea.MouseWheelMsg:
		switch tea.Mouse(msg).Button {
		case tea.MouseWheelUp:
			v.move(-listWheelRows)
		case tea.MouseWheelDown:
			v.move(listWheelRows)
		}
	}
	return nil
}

// key handles one key press.
func (v *listView) key(k tea.Key) tea.Cmd {
	page := v.pageRows()
	switch k.Code {
	case tea.KeyUp:
		v.move(-1)
	case tea.KeyDown:
		v.move(1)
	case tea.KeyPgUp:
		v.move(-page)
	case tea.KeyPgDown:
		v.move(page)
	case tea.KeyHome:
		v.move(-len(v.shown))
	case tea.KeyEnd:
		v.move(len(v.shown))
	case tea.KeyEnter:
		return v.chooseSelected()
	case tea.KeyBackspace:
		if n := len(v.filter); n > 0 {
			v.filter = v.filter[:n-1]
			v.recompute()
		}
	default:
		if k.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
			v.typeText(k.Text)
		}
	}
	return nil
}

// typeText appends printable text to the filter. Control characters, such as Tab,
// do not type.
func (v *listView) typeText(text string) {
	if text == "" || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return
	}
	v.filter = append(v.filter, []rune(text)...)
	v.recompute()
}

// move shifts the highlight by d items, skipping headers, and keeps it on screen.
func (v *listView) move(d int) {
	step, n := 1, d
	if d < 0 {
		step, n = -1, -d
	}
	for range n {
		next := v.sel + step
		for next >= 0 && next < len(v.shown) && v.rows[v.shown[next]].header {
			next += step
		}
		if next < 0 || next >= len(v.shown) {
			break
		}
		v.sel = next
	}
	v.keepVisible()
}

// pageRows is the number of item rows on screen: the last draw's, or the default
// before the first draw.
func (v *listView) pageRows() int {
	if v.page > 0 {
		return v.page
	}
	return listDefaultPage
}

// keepVisible scrolls the window so that the highlight is in it.
func (v *listView) keepVisible() {
	page := v.pageRows()
	if v.sel < v.top {
		v.top = v.sel
	}
	if v.sel >= v.top+page {
		v.top = v.sel - page + 1
	}
	v.top = max(0, min(v.top, len(v.shown)-page))
}

// chooseSelected runs the highlighted row. A header, or a list without run, does
// nothing.
func (v *listView) chooseSelected() tea.Cmd {
	r, ok := v.selected()
	if !ok || v.run == nil {
		return nil
	}
	return v.run(v, r)
}

// rowAt returns the index in shown of the item drawn at (x, y) in the last draw.
func (v *listView) rowAt(x, y int) (int, bool) {
	b := v.box
	if !b.contains(x, y) || y < b.y+2 || y >= b.y+b.h-1 {
		return 0, false
	}
	k := v.top + (y - b.y - 2)
	if k >= len(v.shown) || v.rows[v.shown[k]].header {
		return 0, false
	}
	return k, true
}

// cursor is the terminal caret position in the filter text, for the root to show.
// ok is false before the first draw, or when the box has no room for the caret.
func (v *listView) cursor(int, int) (int, int, bool) {
	if v.box.w == 0 {
		return 0, 0, false
	}
	labelX := v.box.x + 2 + ansi.StringWidth(filterLabel)
	room := v.box.x + v.box.w - 2 - labelX
	if room < 0 {
		return 0, 0, false
	}
	return labelX + ansi.StringWidth(tailCells(string(v.filter), room)), v.box.y + 1, true
}

// tailCells returns the end of s that fits in w cells. The start of a longer text
// is cut, so the end of the filter stays in view while it is typed.
func tailCells(s string, w int) string {
	r := []rune(s)
	for len(r) > 0 && ansi.StringWidth(string(r)) > w {
		r = r[1:]
	}
	return string(r)
}

// filterLabel is the text before the filter on the filter row.
const filterLabel = "Filter: "

// draw paints the box over g. It records the box and the page size, so the keys
// and the mouse use what is on screen. The box has the border and title of the
// dialogs, the filter row, the items, and the hint on the bottom border.
func (v *listView) draw(g *grid.Grid) {
	box, rows := listBox(g.Width(), g.Height())
	v.box = box
	v.page = rows
	if rows == 0 {
		return
	}
	v.keepVisible()
	ov := v.chrome
	ov.PaintBox(g, box.x, box.y, box.w, box.h, v.title)
	end := box.x + box.w - 2 // text stops before the padding cell at the right border
	filterY := box.y + 1
	labelX := box.x + 2 + g.Put(box.x+2, filterY, end, filterLabel, ov.Muted)
	g.Put(labelX, filterY, end, tailCells(string(v.filter), end-labelX), ov.Box)

	for k := range rows {
		idx := v.top + k
		if idx >= len(v.shown) {
			break
		}
		r := v.rows[v.shown[idx]]
		y := box.y + 2 + k
		st := ov.Box
		right := ov.Muted
		switch {
		case r.header:
			st.Bold = true
		case idx == v.sel:
			st = ov.Selected
			right = st
			g.Fill(box.x+1, box.x+box.w-1, y, st)
		}
		left, room := r.left, end-box.x-2
		if r.right != "" {
			// A long value keeps its end, and the label keeps its own width.
			value := tailCells(r.right, end-(box.x+2)-ansi.StringWidth(left)-2)
			rightX := end - ansi.StringWidth(value)
			g.Put(rightX, y, end, value, right)
			room = rightX - (box.x + 2) - 2
		}
		if room > 0 {
			g.Put(box.x+2, y, end, truncateCells(left, room), st)
		}
	}
	if len(v.shown) == 0 {
		g.Put(box.x+2, box.y+2, end, v.empty, ov.Muted)
	}
	g.Put(box.x+2, box.y+box.h-1, end, truncateCells(" "+listHint+" ", end-(box.x+2)), ov.Muted)
}

// listOverlay adapts *listView to Overlay. The list closes itself when it clears
// active, and the adapter reports that to the stack.
type listOverlay struct{ v *listView }

// Update forwards the message to the list. It returns nil once the list is closed,
// so the stack pops it.
func (o listOverlay) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	cmd := o.v.update(msg)
	if !o.v.active {
		return nil, cmd
	}
	return o, cmd
}

// Draw paints the list over the frame.
func (o listOverlay) Draw(g *grid.Grid) { o.v.draw(g) }

// Active reports whether the list is still open.
func (o listOverlay) Active() bool { return o.v.active }

// Cursor returns the caret in the filter text.
func (o listOverlay) Cursor(w, h int) (int, int, bool) { return o.v.cursor(w, h) }
