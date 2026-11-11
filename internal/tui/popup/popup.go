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
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// The popup is an overlay of the root's stack.
var _ overlay.Overlay = (*Popup)(nil)

// maxRows is the most item rows a popup shows at once.
const maxRows = 10

// The box around the items: two border rows, the narrowest box and the blank cells
// between a label and its detail.
const (
	borderRows  = 2
	minBoxWidth = 8
	detailGap   = 2
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

// Popup is an anchored list. Use New to make one and Show to open it. The zero
// value is a closed popup that draws nothing.
type Popup struct {
	chrome    style.Overlay
	open      bool
	anchor    Point
	title     string
	note      string   // a message block shown above the items (an action popup)
	noteLines []string // the wrapped note of the last draw
	items     []Item
	sel       int // the highlighted item
	top       int // the first item shown
	rows      int // item rows in the last draw
	box       rect
}

// rect is a block of cells: x and y are the top-left corner, w and h the size.
type rect struct{ x, y, w, h int }

// contains reports whether the cell (x, y) lies inside r.
func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// New returns a closed popup painted with the shared overlay frame: the box, the
// rounded border, the title, the selected row and the muted detail of a dialog.
func New(chrome style.Overlay) *Popup {
	return &Popup{chrome: chrome}
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

// ShowNote opens the popup like Show with a message block shown above the items: the
// message wraps to the box width and grows the box. It is the action popup's shape.
func (p *Popup) ShowNote(anchor Point, title, note string, items []Item) {
	p.Show(anchor, title, items)
	p.note = note
}

// Active reports whether the popup is open.
func (p *Popup) Active() bool { return p.open }

// Selected is the index of the highlighted item.
func (p *Popup) Selected() int { return p.sel }

// Select highlights item i, clamped to the list (0 for an empty list). It is for a
// caller that rebuilds a list and keeps the user's prior choice; Show starts on the
// first row, so a rebuilt popup calls this after Show.
func (p *Popup) Select(i int) {
	if len(p.items) == 0 {
		p.sel, p.top = 0, 0
		return
	}
	p.sel = min(max(i, 0), len(p.items)-1)
	p.keepVisible()
}

// Items returns a copy of the items the popup lists.
func (p *Popup) Items() []Item { return slices.Clone(p.items) }

// update handles one key or mouse message and returns the command it starts: the
// Chosen message when a row is chosen. Other messages are ignored.
func (p *Popup) update(msg tea.Msg) tea.Cmd {
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

// Update handles one message through the overlay contract: it runs update and
// returns the popup, or nil once it is closed so the stack pops it.
func (p *Popup) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	cmd := p.update(msg)
	if !p.Active() {
		return nil, cmd
	}
	return p, cmd
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
	if !b.contains(x, y) || y <= b.y+len(p.noteLines) || y > b.y+len(p.noteLines)+p.rows || x <= b.x || x >= b.x+b.w-1 {
		return 0, false
	}
	i := p.top + (y - b.y - 1 - len(p.noteLines))
	if i < 0 || i >= len(p.items) {
		return 0, false
	}
	return i, true
}

// Draw paints the box over g. It records the box, so that a mouse position maps
// to a row. Draw changes no item and no highlight, except to keep the highlight in
// the window when the frame height changed. The box is the shared overlay frame:
// a rounded border with the title on its top edge and the plain rule after it, the item
// rows with one padding cell beside each border, and space.Margin from the frame
// edges. It never covers the menu bar's label row.
func (p *Popup) Draw(g *grid.Grid) {
	if !p.open || g.Width() < minBoxWidth || g.Height() < borderRows+1 {
		p.box = rect{}
		return
	}
	w, h := g.Width(), g.Height()
	boxW := boxWidthNote(p.title, p.note, p.items, w)
	p.noteLines = wrapNote(p.note, max(boxW-2*(1+space.PadX), 1))
	p.rows = visibleRows(len(p.items), h-len(p.noteLines))
	p.keepVisible()
	p.box = placeBox(p.anchor, boxW, p.rows+borderRows+len(p.noteLines), w, h)

	chrome := p.chrome
	b := p.box
	chrome.PaintBox(g, b.x, b.y, b.w, b.h, p.title)
	inner, end := b.x+1+space.PadX, b.x+b.w-1-space.PadX
	for i, line := range p.noteLines {
		g.Put(inner, b.y+1+i, end, line, chrome.Box)
	}
	itemTop := b.y + 1 + len(p.noteLines)
	if len(p.items) == 0 {
		g.Put(inner, itemTop, end, "No items", chrome.Muted)
		return
	}
	for k := range p.rows {
		i := p.top + k
		if i >= len(p.items) {
			break
		}
		y := itemTop + k
		st := chrome.Box
		if i == p.sel {
			st = chrome.Selected
		}
		g.Fill(b.x+1, b.x+b.w-1, y, st)
		x := inner + g.Put(inner, y, end, space.End(p.items[i].Label, end-inner), st)
		if d := p.items[i].Detail; d != "" {
			detail := chrome.Muted
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

// boxWidth is the width of a popup box with title and items in a frame fw cells wide.
func boxWidth(title string, items []Item, fw int) int {
	return boxWidthNote(title, "", items, fw)
}

// boxWidthNote is boxWidth plus the width want of a note block: the note wraps, so it
// only asks the box to be wide enough for a readable line of it (at most half the frame).
func boxWidthNote(title, note string, items []Item, fw int) int {
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
	if note != "" {
		w = max(w, min(ansi.StringWidth(note), fw/2)+2*(1+space.PadX)+2)
	}
	return min(max(w, minBoxWidth), max(fw-2*space.Margin(fw), minBoxWidth), fw)
}

// wrapNote word-wraps a note to width cells, one line per row. A word longer than the
// width is broken.
func wrapNote(note string, width int) []string {
	note = strings.TrimSpace(note)
	if note == "" || width <= 0 {
		return nil
	}
	var lines []string
	for _, para := range strings.Split(note, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := ""
		for _, word := range words {
			for ansi.StringWidth(word) > width {
				if line != "" {
					lines = append(lines, line)
					line = ""
				}
				head := ansi.Truncate(word, width, "")
				lines = append(lines, head)
				word = word[len(head):]
			}
			switch {
			case line == "":
				line = word
			case ansi.StringWidth(line)+1+ansi.StringWidth(word) <= width:
				line += " " + word
			default:
				lines = append(lines, line)
				line = word
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// placeBox places a w by h box for a popup anchored at anchor in a fw by fh frame.
// The box goes on the row below the anchor, or above it when it does not fit there,
// and it is moved to stay inside the frame, space.Margin from the left and right edges
// and from the bottom row when there is room. It never covers the menu bar's label row.
func placeBox(anchor Point, w, h, fw, fh int) rect {
	m := space.Margin(fw)
	if fw < w+2*m {
		m = 0
	}
	x := max(m, min(anchor.X, fw-w-m))
	x = max(x, 0)
	y := anchor.Y + 1
	if y+h > fh-m {
		y = anchor.Y - h
	}
	y = min(y, fh-h-m)
	y = max(y, 1)
	return rect{x: x, y: y, w: w, h: h}
}

// maxTextWidth is the widest a row of wrapped text is made, however wide the frame: longer
// lines are hard to read.
const maxTextWidth = 80

// TextWidth is the width in cells that a caller wraps long rows to so that they show whole in
// a popup in a frame fw cells wide: the room between the borders and padding of the widest box
// the frame allows, and at most maxTextWidth.
func TextWidth(fw int) int {
	box := fw - 2*space.Margin(fw)
	return max(min(box-2*(1+space.PadX), maxTextWidth), 1)
}
