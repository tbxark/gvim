// Package popup is the anchored list popup of gvim: a titled box of items placed
// beside a cell of the frame. The remote indicator menu uses it now, and LSP hover
// and completion will use it in P4.
//
// A popup takes the keys and mouse input of the overlay stack while it is open.
// Up, Down, PageUp, PageDown, Home and End move the highlight, Enter and a left
// click on a row choose it, and a click outside the box closes the popup. Esc is
// not seen here: the root pops every overlay on Esc. Choosing a row emits Chosen
// and closes the popup. The popup runs nothing itself, so the owner of the popup
// decides what the choice does.
package popup

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
)

// maxRows is the most item rows a popup shows at once.
const maxRows = 10

// The box around the items: two border rows, the narrowest box and the blank cells
// between a label and its detail.
const (
	borderRows  = 2
	minBoxWidth = 8
	detailGap   = 2
	// marginMinFrame is the narrowest frame in which a box keeps its margin.
	marginMinFrame = 32
)

// pageStep is the number of rows PageUp and PageDown move the highlight.
const pageStep = 8

// wheelRows is the number of rows one wheel notch moves the highlight.
const wheelRows = 3

// Point is a cell of the frame: X is the column and Y the row, both zero-based.
type Point struct{ X, Y int }

// Item is one row of a popup. Label is the text. Detail, when set, is drawn after
// the label in the faint style. Data is the owner's value, sent back in Chosen.
type Item struct {
	Label  string
	Detail string
	Data   any
}

// Chosen is the message a popup emits when a row is chosen. Index is the position
// of the row in the items given to Show.
type Chosen struct {
	Index int
	Item  Item
}

// Styles are the grid styles of a popup. A zero value leaves the terminal default.
type Styles struct {
	Panel    grid.Style // the box and its title
	Border   grid.Style // the border; the zero value draws it in Panel
	Selected grid.Style // the highlighted row
	Detail   grid.Style // the detail text after a label
}

// Popup is an anchored list. Use New to make one and Show to open it. The zero
// value is a closed popup that draws nothing.
type Popup struct {
	styles Styles
	open   bool
	anchor Point
	title  string
	items  []Item
	sel    int // the highlighted item
	top    int // the first item shown
	rows   int // item rows in the last draw
	box    rect
}

// rect is a block of cells: x and y are the top-left corner, w and h the size.
type rect struct{ x, y, w, h int }

// contains reports whether the cell (x, y) lies inside r.
func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// New returns a closed popup that paints with st.
func New(st Styles) *Popup {
	return &Popup{styles: st}
}

// Show opens the popup at anchor with title and items. The box goes on the row
// below anchor, or above it when there is no room below, and its left edge is
// moved left so the box stays inside the frame. The highlight starts on the first
// item. Show replaces any earlier contents.
func (p *Popup) Show(anchor Point, title string, items []Item) {
	p.open = true
	p.anchor = anchor
	p.title = title
	p.items = slices.Clone(items)
	p.sel, p.top, p.rows = 0, 0, 0
	p.box = rect{}
}

// Active reports whether the popup is open.
func (p *Popup) Active() bool { return p.open }

// Selected is the index of the highlighted item.
func (p *Popup) Selected() int { return p.sel }

// Items returns a copy of the items the popup lists.
func (p *Popup) Items() []Item { return slices.Clone(p.items) }

// Update handles one key or mouse message and returns the command it starts: the
// Chosen message when a row is chosen. Other messages are ignored.
func (p *Popup) Update(msg tea.Msg) tea.Cmd {
	if !p.open {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch tea.Key(msg).Code {
		case tea.KeyUp:
			p.move(-1)
		case tea.KeyDown:
			p.move(1)
		case tea.KeyPgUp:
			p.move(-pageStep)
		case tea.KeyPgDown:
			p.move(pageStep)
		case tea.KeyHome:
			p.move(-len(p.items))
		case tea.KeyEnd:
			p.move(len(p.items))
		case tea.KeyEnter:
			return p.choose(p.sel)
		}
	case tea.MouseClickMsg:
		m := tea.Mouse(msg)
		if m.Button != tea.MouseLeft {
			return nil
		}
		if i, ok := p.rowAt(m.X, m.Y); ok {
			return p.choose(i)
		}
		if !p.box.contains(m.X, m.Y) {
			p.open = false
		}
	case tea.MouseMotionMsg:
		m := tea.Mouse(msg)
		if i, ok := p.rowAt(m.X, m.Y); ok {
			p.sel = i
			p.keepVisible()
		}
	case tea.MouseWheelMsg:
		switch tea.Mouse(msg).Button {
		case tea.MouseWheelUp:
			p.move(-wheelRows)
		case tea.MouseWheelDown:
			p.move(wheelRows)
		}
	}
	return nil
}

// choose closes the popup and returns the command that sends the Chosen message
// for row i. An empty popup has no row to choose, so Enter does nothing there.
func (p *Popup) choose(i int) tea.Cmd {
	if i < 0 || i >= len(p.items) {
		return nil
	}
	c := Chosen{Index: i, Item: p.items[i]}
	p.open = false
	return func() tea.Msg { return c }
}

// move shifts the highlight by d rows, clamped to the items, and keeps it on screen.
func (p *Popup) move(d int) {
	if len(p.items) == 0 {
		return
	}
	p.sel = min(max(p.sel+d, 0), len(p.items)-1)
	p.keepVisible()
}

// keepVisible scrolls the window of item rows so that the highlight is in it.
func (p *Popup) keepVisible() {
	if p.rows <= 0 {
		return
	}
	if p.sel < p.top {
		p.top = p.sel
	}
	if p.sel >= p.top+p.rows {
		p.top = p.sel - p.rows + 1
	}
	p.top = max(0, min(p.top, len(p.items)-p.rows))
}

// rowAt returns the item under the cell (x, y) in the last draw. The borders and the
// cells outside the box are not items. The padding cells beside an item are part of
// its highlighted row.
func (p *Popup) rowAt(x, y int) (int, bool) {
	b := p.box
	if !b.contains(x, y) || y <= b.y || y > b.y+p.rows || x <= b.x || x >= b.x+b.w-1 {
		return 0, false
	}
	i := p.top + (y - b.y - 1)
	if i < 0 || i >= len(p.items) {
		return 0, false
	}
	return i, true
}

// Draw paints the box over g. It records the box, so that a mouse position maps
// to a row. Draw changes no item and no highlight, except to keep the highlight in
// the window when the frame height changed. The box has a rounded border with the
// title on its top edge, a padding cell between the border and the text on each
// side, and a margin from the edges of the frame when the frame has room for it.
func (p *Popup) Draw(g *grid.Grid) {
	if !p.open || g.Width() < minBoxWidth || g.Height() < borderRows+1 {
		p.box = rect{}
		return
	}
	w, h := g.Width(), g.Height()
	p.rows = visibleRows(len(p.items), h)
	p.keepVisible()
	p.box = placeBox(p.anchor, boxWidth(p.title, p.items, w), p.rows+borderRows, w, h)

	panel, border := p.styles.Panel, p.styles.Border
	if border == (grid.Style{}) {
		border = panel
	}
	b := p.box
	right, bottom := b.x+b.w-1, b.y+b.h-1
	for y := b.y; y <= bottom; y++ {
		g.Fill(b.x, b.x+b.w, y, panel)
	}
	g.Put(b.x, b.y, b.x+b.w, "╭"+strings.Repeat("─", b.w-2)+"╮", border)
	g.Put(b.x, bottom, b.x+b.w, "╰"+strings.Repeat("─", b.w-2)+"╯", border)
	for y := b.y + 1; y < bottom; y++ {
		g.Set(b.x, y, "│", border)
		g.Set(right, y, "│", border)
	}
	titled := panel
	titled.Bold = true
	if p.title != "" {
		g.Put(b.x+2, b.y, right, " "+p.title+" ", titled)
	}
	inner, end := b.x+1+space.PadX, right-space.PadX
	if len(p.items) == 0 {
		g.Put(inner, b.y+1, end, "No items", p.styles.Detail)
		return
	}
	for k := range p.rows {
		i := p.top + k
		if i >= len(p.items) {
			break
		}
		y := b.y + 1 + k
		st := panel
		if i == p.sel {
			st = p.styles.Selected
		}
		g.Fill(b.x+1, right, y, st)
		x := inner + g.Put(inner, y, end, space.End(p.items[i].Label, end-inner), st)
		if d := p.items[i].Detail; d != "" {
			detail := p.styles.Detail
			if i == p.sel {
				detail = st
				detail.Faint = true
			}
			g.Put(x+detailGap, y, end, space.End(d, end-x-detailGap), detail)
		}
	}
}

// visibleRows is the number of item rows a popup shows in a frame h rows high, with
// the two border rows around them. An empty popup still has one row, for its message.
func visibleRows(n, h int) int {
	return min(max(n, 1), maxRows, max(h-borderRows, 1))
}

// boxWidth is the width of a popup box with title and items in a frame fw cells
// wide: the widest line, a padding cell and a border on each side. The title sits on
// the top border, between one border dash and the corner on each side. A box wider
// than the frame, less its margin, is cut to it and the long rows end with "…".
func boxWidth(title string, items []Item, fw int) int {
	body := 0
	for _, it := range items {
		n := ansi.StringWidth(it.Label)
		if it.Detail != "" {
			n += detailGap + ansi.StringWidth(it.Detail)
		}
		body = max(body, n)
	}
	if len(items) == 0 {
		body = len("No items")
	}
	w := body + 2*(1+space.PadX)
	if title != "" {
		w = max(w, ansi.StringWidth(title)+2+4)
	}
	return min(max(w, minBoxWidth), max(fw-2*edgeMargin(fw, w), minBoxWidth), fw)
}

// edgeMargin is the cells a box w wide keeps from the left and right edge of a frame
// fw wide: one when the frame has room for it on both sides, else none.
func edgeMargin(fw, w int) int {
	if fw >= w+2*space.PadX && fw >= marginMinFrame {
		return space.PadX
	}
	return 0
}

// placeBox places a w by h box for a popup anchored at anchor in a fw by fh frame.
// The box goes on the row below the anchor, or above it when it does not fit there,
// and it is moved to stay inside the frame, one cell from its left and right edges
// and from its bottom row when there is room.
func placeBox(anchor Point, w, h, fw, fh int) rect {
	m := edgeMargin(fw, w)
	x := max(m, min(anchor.X, fw-w-m))
	x = max(x, 0)
	y := anchor.Y + 1
	if y+h > fh-m {
		y = anchor.Y - h
	}
	y = max(0, min(y, fh-h-m))
	y = max(y, 0)
	return rect{x: x, y: y, w: w, h: h}
}

// maxTextWidth is the widest a row of wrapped text is made, however wide the frame: longer
// lines are hard to read.
const maxTextWidth = 80

// TextWidth is the width in cells that a caller wraps long rows to so that they show whole in
// a popup in a frame fw cells wide: the room between the borders and padding of the widest box
// the frame allows, and at most maxTextWidth.
func TextWidth(fw int) int {
	box := fw
	if fw >= marginMinFrame {
		box = fw - 2*space.PadX
	}
	return max(min(box-2*(1+space.PadX), maxTextWidth), 1)
}
