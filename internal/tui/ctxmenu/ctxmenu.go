// Package ctxmenu is the right-click context menu of gvim: a small bordered list of
// actions that opens at the pointer. It follows Fresh's context menus (tab menu and
// file explorer menu): the highlight follows the pointer, a click on a row chooses
// it, and a press outside closes the menu.
//
// A menu takes the keys and mouse input of the overlay stack while it is open. Up,
// Down, Home and End move the highlight over the rows that can be chosen, and Enter
// chooses the highlighted row. Esc is not seen here: the root pops every overlay on
// Esc. The menu runs nothing itself. A chosen row is sent as Chosen, and the owner
// decides what it does. The pointer rule matches the menu bar: where the pointer is
// not on a row that can be chosen (a separator, a disabled row, a border, outside
// the menu) no row is highlighted.
package ctxmenu

import (
	"slices"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
)

// The menu is an overlay of the root's stack.
var _ overlay.Overlay = (*Menu)(nil)

// Item is one row of a menu. Action and Data are the owner's: they come back in
// Chosen. A Separator draws a rule, and a Disabled row is drawn faint and cannot be
// chosen.
type Item struct {
	Label     string
	Action    string
	Data      any
	Separator bool
	Disabled  bool
}

// selectable reports whether the row can be highlighted and chosen.
func (it Item) selectable() bool { return !it.Separator && !it.Disabled }

// Chosen is the message a menu sends when a row is chosen.
type Chosen struct {
	Item Item
}

// Reroute is the message a menu sends when a right press falls outside it. The menu
// closes, and the owner handles the press again, so a second right-click on another
// target opens that target's menu at once.
type Reroute struct {
	Press tea.MouseClickMsg
}

// Styles are the grid styles of a menu.
type Styles struct {
	Box      grid.Style // the fill, the rows and the separators
	Border   grid.Style // the rounded border
	Selected grid.Style // the highlighted row
	Disabled grid.Style // a row that cannot be chosen
}

// Menu is a context menu. Use New to make one and Show to open it. The zero value is
// a closed menu that draws nothing.
type Menu struct {
	styles Styles
	open   bool
	x, y   int // where the pointer opened the menu
	items  []Item
	sel    int // the highlighted row, -1 for none
	first  int // the first row shown, when the rows do not fit the frame
	box    rect
}

// rect is a block of cells.
type rect struct{ x, y, w, h int }

// contains reports whether the cell (x, y) lies inside r.
func (r rect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// New returns a closed menu that paints with st.
func New(st Styles) *Menu { return &Menu{styles: st, sel: -1} }

// Show opens the menu in a w by h frame with its top-left corner at (x, y), or as
// near as the frame allows. The first row that can be chosen is highlighted. A menu
// with nothing to choose opens with no highlight.
func (m *Menu) Show(x, y, w, h int, items []Item) {
	m.open = true
	m.x, m.y = x, y
	m.items = slices.Clone(items)
	m.sel = m.step(-1, 1)
	m.first = 0
	m.place(w, h)
	m.reveal()
}

// Active reports whether the menu is open.
func (m *Menu) Active() bool { return m.open }

// Selected is the highlighted row, or -1 when none is.
func (m *Menu) Selected() int { return m.sel }

// Items returns the rows of the menu. The slice is a copy.
func (m *Menu) Items() []Item { return slices.Clone(m.items) }

// Box is the rectangle of the menu at the last draw: its column, row, width and
// height. The width is 0 before the first draw.
func (m *Menu) Box() (x, y, w, h int) { return m.box.x, m.box.y, m.box.w, m.box.h }

// update handles one key or mouse message and returns the command it starts: Chosen
// when a row is chosen, or Reroute for a right press outside the menu.
func (m *Menu) update(msg tea.Msg) tea.Cmd {
	if !m.open {
		return nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch tea.Key(msg).Code {
		case tea.KeyUp:
			m.sel = m.stepFrom(-1)
		case tea.KeyDown:
			m.sel = m.stepFrom(1)
		case tea.KeyHome:
			m.sel = m.step(-1, 1)
		case tea.KeyEnd:
			m.sel = m.step(len(m.items), -1)
		case tea.KeyEnter:
			return m.choose(m.sel)
		}
		m.reveal()
	case tea.MouseWheelMsg:
		m.wheel(tea.Mouse(msg))
	case tea.MouseMotionMsg:
		mo := tea.Mouse(msg)
		m.sel = m.rowAt(mo.X, mo.Y)
	case tea.MouseClickMsg:
		return m.press(msg)
	}
	return nil
}

// Update handles one message through the overlay contract: it runs update and
// returns the menu, or nil once it is closed so the stack pops it.
func (m *Menu) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	cmd := m.update(msg)
	if !m.Active() {
		return nil, cmd
	}
	return m, cmd
}

// visible is the number of rows the box shows.
func (m *Menu) visible() int { return max(m.box.h-2, 0) }

// reveal keeps the window on the rows that fit the box and the highlighted row in it.
func (m *Menu) reveal() {
	rows := m.visible()
	m.first = min(max(m.first, 0), max(len(m.items)-rows, 0))
	if m.sel >= 0 && rows > 0 {
		m.first = max(min(m.first, m.sel), m.sel-rows+1)
	}
}

// wheel scrolls the window by one row per notch. A highlight that leaves the window
// is dropped, so the next key starts from the rows that are shown.
func (m *Menu) wheel(mo tea.Mouse) {
	if !m.box.contains(mo.X, mo.Y) {
		return
	}
	switch mo.Button {
	case tea.MouseWheelUp:
		m.first--
	case tea.MouseWheelDown:
		m.first++
	default:
		return
	}
	m.first = min(max(m.first, 0), max(len(m.items)-m.visible(), 0))
	if m.sel < m.first || m.sel >= m.first+m.visible() {
		m.sel = -1
	}
}

// press handles a mouse press. A left press on a row chooses it, and on the rest of
// the menu does nothing. A press outside closes the menu, and a right press there is
// sent on for the owner to handle.
func (m *Menu) press(msg tea.MouseClickMsg) tea.Cmd {
	mo := tea.Mouse(msg)
	if m.box.contains(mo.X, mo.Y) {
		if mo.Button != tea.MouseLeft {
			return nil
		}
		return m.choose(m.rowAt(mo.X, mo.Y))
	}
	m.open = false
	if mo.Button == tea.MouseRight {
		return func() tea.Msg { return Reroute{Press: msg} }
	}
	return nil
}

// choose closes the menu and returns the command that sends Chosen for row i. A row
// that cannot be chosen, and no row, do nothing.
func (m *Menu) choose(i int) tea.Cmd {
	if i < 0 || i >= len(m.items) || !m.items[i].selectable() {
		return nil
	}
	c := Chosen{Item: m.items[i]}
	m.open = false
	return func() tea.Msg { return c }
}

// rowAt is the row that can be chosen under the cell (x, y) in the last draw, or -1.
func (m *Menu) rowAt(x, y int) int {
	if !m.box.contains(x, y) || x == m.box.x || x == m.box.x+m.box.w-1 {
		return -1
	}
	i := y - m.box.y - 1
	if i < 0 || i >= m.visible() {
		return -1
	}
	i += m.first
	if i >= len(m.items) || !m.items[i].selectable() {
		return -1
	}
	return i
}

// stepFrom moves the highlight one row in dir over the rows that can be chosen. With
// no highlight, Down chooses the first row and Up the last. It stays where it is when
// no row is in that direction.
func (m *Menu) stepFrom(dir int) int {
	if m.sel < 0 {
		if dir > 0 {
			return m.step(-1, 1)
		}
		return m.step(len(m.items), -1)
	}
	return m.step(m.sel, dir)
}

// step returns the next row that can be chosen after start in direction dir, or the
// highlight (start, when it is a row) when there is none.
func (m *Menu) step(start, dir int) int {
	for i := start + dir; i >= 0 && i < len(m.items); i += dir {
		if m.items[i].selectable() {
			return i
		}
	}
	if start < 0 || start >= len(m.items) {
		return -1
	}
	return start
}

// width is the width of the box: the widest label with one blank of padding on each
// side, and the border.
func (m *Menu) width(frame int) int {
	w := 0
	for _, it := range m.items {
		if !it.Separator {
			w = max(w, ansi.StringWidth(it.Label))
		}
	}
	return min(w+4, frame)
}

// place records the box in a w by h frame. The box starts at the pointer and is moved
// left and up when it would pass the right or the bottom edge, so it is always inside
// the frame. A frame too small for a menu leaves an empty box.
func (m *Menu) place(fw, fh int) {
	if fw < 5 || fh < 3 {
		m.box = rect{}
		return
	}
	w := m.width(fw)
	h := min(len(m.items)+2, fh)
	m.box = rect{x: max(min(m.x, fw-w), 0), y: max(min(m.y, fh-h), 0), w: w, h: h}
}

// Draw paints the menu over g. It places the box for the size of g first, so that a
// pointer position maps to the row that is drawn there.
func (m *Menu) Draw(g *grid.Grid) {
	if !m.open {
		return
	}
	m.place(g.Width(), g.Height())
	b := m.box
	if b.w == 0 {
		return
	}
	m.reveal()
	rows := b.h - 2
	g.Fill(b.x, b.x+b.w, b.y, m.styles.Box)
	g.Put(b.x, b.y, b.x+b.w, "╭"+rule(b.w-2)+"╮", m.styles.Border)
	for k := range rows {
		i := m.first + k
		it := m.items[i]
		y := b.y + 1 + k
		st := m.styles.Box
		switch {
		case it.Separator:
		case i == m.sel:
			st = m.styles.Selected
		case it.Disabled:
			st = m.styles.Disabled
		}
		g.Fill(b.x, b.x+b.w, y, m.styles.Box)
		g.Set(b.x, y, "│", m.styles.Border)
		g.Set(b.x+b.w-1, y, "│", m.styles.Border)
		if it.Separator {
			g.Put(b.x+1, y, b.x+b.w-1, rule(b.w-2), m.styles.Box)
			continue
		}
		g.Fill(b.x+1, b.x+b.w-1, y, st)
		g.Put(b.x+2, y, b.x+b.w-2, it.Label, st)
	}
	g.Put(b.x, b.y+b.h-1, b.x+b.w, "╰"+rule(b.w-2)+"╯", m.styles.Border)
	if m.first > 0 {
		g.Set(b.x+b.w/2, b.y, "▲", m.styles.Border)
	}
	if m.first+rows < len(m.items) {
		g.Set(b.x+b.w/2, b.y+b.h-1, "▼", m.styles.Border)
	}
}

// rule is n cells of the horizontal rule.
func rule(n int) string {
	out := make([]rune, max(n, 0))
	for i := range out {
		out[i] = '─'
	}
	return string(out)
}
