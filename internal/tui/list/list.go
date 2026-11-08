// Package list is the list and text-box kit of the gvim UI: View is a titled,
// filterable, scrollable list in a centred box, and TextBox is a centred box of
// text. Both are overlays, so a root model stacks them and gives them the input
// while they are on top, and both take the overlay styles and the shared space
// system as their chrome.
//
// The kit is a leaf: it imports the grid, space and style primitives and the
// overlay contract, never the root model.
package list

import (
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// MaxWidth and MaxHeight cap the box of a View. The box is centred.
const (
	MaxWidth  = 76
	MaxHeight = 28
)

// WheelRows is the number of rows one wheel notch moves the highlight of a
// list-like view.
const WheelRows = 3

// DefaultPage is the number of item rows a View assumes before its first draw,
// when it does not know the frame size yet.
const DefaultPage = 8

// Hint is the footer of a list that runs a row. It names the keys the list
// takes. A list that runs nothing shows ReadOnlyHint, and a list can set its own.
const (
	Hint         = "Type to filter, Up/Down move, Enter run, Esc close"
	ReadOnlyHint = "Type to filter, Up/Down move, Esc close"
)

// Row is one row of a View. A header row names a group and cannot be chosen.
// Left is the main text, such as an action or a setting, and Right is the
// secondary text drawn at the right edge. Value is what choosing the row means
// to the owner of the list, such as a preset name.
type Row struct {
	Header bool
	Left   string
	Right  string
	Value  string
}

// View is a titled, filterable, scrollable list in a centred box. Typed text
// filters the rows, and a row keeps its group header while one of its rows
// matches. Up and Down move the highlight over the items, skipping headers,
// PageUp and PageDown move a page, and Home and End go to the first and last
// item. Enter and a left click choose the highlighted row through run. Esc is
// not seen here: the root pops the overlay on Esc.
//
// The exported fields are the owner's configuration, and Box reports the drawn
// box; the filter, the highlight and the window are the kit's own state.
type View struct {
	Title string
	Empty string // the text shown when no row matches the filter
	Rows  []Row
	// BestFirst puts the rows whose text is the filter, then those that start with
	// it, before the rest, so typing a name reaches its row first.
	BestFirst bool
	// Fit makes the box no taller than the rows need, for a list of a few rows.
	Fit bool
	// Hint is the footer; empty means the kit's Hint, or ReadOnlyHint when run is nil.
	Hint string
	// MaxW is the widest the box is drawn; zero is MaxWidth.
	MaxW int
	// Cut fits a row to room cells; nil cuts at the end.
	Cut func(s string, room int) string
	// Box is the box at the last draw; the zero value before the first draw.
	Box space.Rect

	shown  []int  // indexes into Rows that pass the filter, headers included
	filter []rune // the filter text
	sel    int    // the highlight, an index into shown
	top    int    // the first index into shown on screen
	page   int    // the item rows on screen at the last draw
	active bool
	chrome style.Overlay
	run    func(v *View, r Row) tea.Cmd // nil: choosing a row does nothing
}

// The kit's two views are overlays of the root's stack.
var (
	_ overlay.Overlay = (*View)(nil)
	_ overlay.Caret   = (*View)(nil)
)

// New returns an open list with rows, titled title. run is called when a row
// is chosen, and it closes the list with Close when it means to.
func New(title string, rows []Row, chrome style.Overlay, run func(*View, Row) tea.Cmd) *View {
	v := &View{
		Title:  title,
		Empty:  "No matches",
		Rows:   rows,
		active: true,
		chrome: chrome,
		run:    run,
	}
	v.recompute()
	return v
}

// box returns the box of a list in a w by h frame, and the number of item rows
// it holds. The title is on the top border and the hint on the bottom one, so the
// box has a filter row and the items inside them. An empty box means the frame is
// too small to show the list.
func box(w, h int) (space.Rect, int) {
	return boxFor(w, h, MaxHeight)
}

// boxFor is box for a list of n rows: a box that is not taller than its rows
// need, so two rows do not sit in a box of 28.
func boxFor(w, h, n int) (space.Rect, int) { return boxSized(w, h, n, 0) }

// boxSized is boxFor for a list that may be maxW cells wide; zero is MaxWidth.
func boxSized(w, h, n, maxW int) (space.Rect, int) {
	if maxW == 0 {
		maxW = MaxWidth
	}
	r := Centred(w, h, maxW, min(MaxHeight, n+3))
	if r.W == 0 {
		return space.Rect{}, 0
	}
	return r, r.H - 3
}

// Centred returns a box of at most bw by bh cells, centred in a w by h frame and
// kept space.ListMargin cells inside the left and right edge and one row inside the
// top and bottom. A frame too small for a box with a title row and one body row
// gives an empty rectangle.
func Centred(w, h, bw, bh int) space.Rect {
	bw, bh = min(bw, w-2*space.ListMargin(w)), min(bh, h-2)
	if bw < 12 || bh < 3 {
		return space.Rect{}
	}
	return space.Rect{X: (w - bw) / 2, Y: (h - bh) / 2, W: bw, H: bh}
}

// recompute rebuilds the shown rows from the filter. The highlight moves to the
// first item and the window to the top.
func (v *View) recompute() {
	needle := strings.ToLower(string(v.filter))
	v.shown = v.shown[:0]
	head := -1
	for i, r := range v.Rows {
		if r.Header {
			head = i
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(r.Left+" "+r.Right+" "+r.Value), needle) {
			continue
		}
		if head >= 0 {
			v.shown = append(v.shown, head)
			head = -1
		}
		v.shown = append(v.shown, i)
	}
	if v.BestFirst && needle != "" {
		v.rankShown(needle)
	}
	v.sel, v.top = v.firstItem(), 0
}

// rankShown moves the rows whose text equals needle, then those that start with it,
// ahead of the others. The order within each group is kept. A list with
// bestFirst has no headers.
func (v *View) rankShown(needle string) {
	rank := func(i int) int {
		left := strings.ToLower(v.Rows[i].Left)
		switch {
		case left == needle:
			return 0
		case strings.HasPrefix(left, needle):
			return 1
		}
		return 2
	}
	slices.SortStableFunc(v.shown, func(a, b int) int { return rank(a) - rank(b) })
}

// Visible returns the rows that pass the filter, in display order: the rows to
// draw, with the headers of the groups that still have a matching row.
func (v *View) Visible() []Row {
	out := make([]Row, len(v.shown))
	for i, idx := range v.shown {
		out[i] = v.Rows[idx]
	}
	return out
}

// firstItem is the index in shown of the first item, or 0 when there is none.
func (v *View) firstItem() int {
	for k, i := range v.shown {
		if !v.Rows[i].Header {
			return k
		}
	}
	return 0
}

// selected returns the highlighted row, and false when no item is highlighted.
func (v *View) selected() (Row, bool) {
	if v.sel < 0 || v.sel >= len(v.shown) {
		return Row{}, false
	}
	r := v.Rows[v.shown[v.sel]]
	return r, !r.Header
}

// update handles one key or mouse message and returns the command the choice
// starts, if any.
func (v *View) update(msg tea.Msg) tea.Cmd {
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
			v.move(-WheelRows)
		case tea.MouseWheelDown:
			v.move(WheelRows)
		}
	}
	return nil
}

// key handles one key press.
func (v *View) key(k tea.Key) tea.Cmd {
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
func (v *View) typeText(text string) {
	if text == "" || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return
	}
	v.filter = append(v.filter, []rune(text)...)
	v.recompute()
}

// move shifts the highlight by d items, skipping headers, and keeps it on screen.
func (v *View) move(d int) {
	step, n := 1, d
	if d < 0 {
		step, n = -1, -d
	}
	for range n {
		next := v.sel + step
		for next >= 0 && next < len(v.shown) && v.Rows[v.shown[next]].Header {
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
func (v *View) pageRows() int {
	if v.page > 0 {
		return v.page
	}
	return DefaultPage
}

// keepVisible scrolls the window so that the highlight is in it.
func (v *View) keepVisible() {
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
func (v *View) chooseSelected() tea.Cmd {
	r, ok := v.selected()
	if !ok || v.run == nil {
		return nil
	}
	return v.run(v, r)
}

// rowAt returns the index in shown of the item drawn at (x, y) in the last draw.
func (v *View) rowAt(x, y int) (int, bool) {
	b := v.Box
	if !b.Contains(x, y) || y < b.Y+2 || y >= b.Y+b.H-1 {
		return 0, false
	}
	k := v.top + (y - b.Y - 2)
	if k >= len(v.shown) || v.Rows[v.shown[k]].Header {
		return 0, false
	}
	return k, true
}

// cursor is the terminal caret position in the filter text, for the root to show.
// ok is false before the first draw, or when the box has no room for the caret.
func (v *View) cursor(int, int) (int, int, bool) {
	if v.Box.W == 0 {
		return 0, 0, false
	}
	labelX := v.Box.X + 2 + ansi.StringWidth(filterLabel)
	room := v.Box.X + v.Box.W - 2 - labelX
	if room < 0 {
		return 0, 0, false
	}
	return labelX + ansi.StringWidth(space.Tail(string(v.filter), room)), v.Box.Y + 1, true
}

// filterLabel is the text before the filter on the filter row.
const filterLabel = "Filter: "

// draw paints the box over g. It records the box and the page size, so the keys
// and the mouse use what is on screen. The box has the border and title of the
// dialogs, the filter row, the items, and the hint on the bottom border.
func (v *View) draw(g *grid.Grid) {
	n := MaxHeight
	if v.Fit {
		n = len(v.Rows)
	}
	box, rows := boxSized(g.Width(), g.Height(), n, v.MaxW)
	v.Box = box
	v.page = rows
	if rows == 0 {
		return
	}
	v.keepVisible()
	ov := v.chrome
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, v.Title)
	end := box.X + box.W - 2 // text stops before the padding cell at the right border
	filterY := box.Y + 1
	labelX := box.X + 2 + g.Put(box.X+2, filterY, end, filterLabel, ov.Muted)
	g.Put(labelX, filterY, end, space.Tail(string(v.filter), end-labelX), ov.Box)

	for k := range rows {
		idx := v.top + k
		if idx >= len(v.shown) {
			break
		}
		r := v.Rows[v.shown[idx]]
		y := box.Y + 2 + k
		st := ov.Box
		right := ov.Muted
		switch {
		case r.Header:
			st.Bold = true
		case idx == v.sel:
			st = ov.Selected
			right = st
			g.Fill(box.X+1, box.X+box.W-1, y, st)
		}
		left, room := r.Left, end-box.X-2
		if r.Right != "" {
			// A long value keeps its end, and the label keeps its own width.
			value := space.Tail(r.Right, end-(box.X+2)-ansi.StringWidth(left)-2)
			rightX := end - ansi.StringWidth(value)
			g.Put(rightX, y, end, value, right)
			room = rightX - (box.X + 2) - 2
		}
		if room > 0 {
			g.Put(box.X+2, y, end, v.fitText(left, room), st)
		}
	}
	if len(v.shown) == 0 {
		g.Put(box.X+2, box.Y+2, end, v.Empty, ov.Muted)
	}
	ov.PaintFooter(g, box.X, box.Y+box.H-1, box.W, v.footer())
}

// footer is the hint under the items: the keys this list takes.
func (v *View) footer() string {
	switch {
	case v.Hint != "":
		return v.Hint
	case v.run == nil:
		return ReadOnlyHint
	}
	return Hint
}

// fitText shortens a row's text to room cells, with the list's own cut when it has one.
func (v *View) fitText(s string, room int) string {
	if v.Cut != nil {
		return v.Cut(s, room)
	}
	return space.End(s, room)
}

// Update handles one key or mouse message through the overlay contract. It
// returns the list, or nil once it has closed itself (a chosen row cleared
// active, or run closed it), so the stack pops it.
func (v *View) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	cmd := v.update(msg)
	if !v.active {
		return nil, cmd
	}
	return v, cmd
}

// Draw paints the list over the frame.
func (v *View) Draw(g *grid.Grid) { v.draw(g) }

// Active reports whether the list is still open.
func (v *View) Active() bool { return v.active }

// Close closes the list, as a chosen row that runs nothing would.
func (v *View) Close() { v.active = false }

// Cursor returns the caret in the filter text.
func (v *View) Cursor(w, h int) (int, int, bool) { return v.cursor(w, h) }
