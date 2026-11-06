package statusbar

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/encoding"
	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/style"
)

// Dispatch runs an editor action with its arguments and returns the command the
// action starts. The root supplies it, so the picker names the action but does
// not reach the editor.
type Dispatch func(action string, args map[string]any) tea.Cmd

// lineEndings are the values set_line_ending takes.
var lineEndings = []string{"LF", "CRLF"}

// Picker is a short list drawn over the status row. It chooses one value for a
// status segment: the encoding, the line ending or the language. Enter or a click
// runs the segment's action with the chosen value as its "name" argument. Up and
// Down move the choice, and Esc closes the list. A click outside the list closes
// it too. Update returns the picker, and Active reports whether it is still open.
// The root keeps it on its overlay stack, as it keeps the browser.
type Picker struct {
	items    []string
	sel      int
	action   string
	dispatch Dispatch
	popup    grid.Style
	selected grid.Style
	open     bool
	box      box // where the last Draw placed the list; zero before the first Draw
}

// box is a screen rectangle of the list, and the index of the item in its top row.
// A zero w means the list was not drawn.
type box struct {
	x, y, w, h, first int
}

// NewPicker returns the open picker for a segment kind, and false when the
// segment has none. languages are the names the language picker offers. The
// picker is drawn with the overlay styles of st.
func NewPicker(kind Kind, languages []string, dispatch Dispatch, st style.Styles) (*Picker, bool) {
	var items []string
	var action string
	switch kind {
	case KindEncoding:
		items, action = encoding.Names(), "set_encoding"
	case KindLineEnding:
		items, action = lineEndings, "set_line_ending"
	case KindLanguage:
		items, action = languages, "set_language"
	default:
		return nil, false
	}
	ov := st.Overlay
	return &Picker{
		items:    items,
		action:   action,
		dispatch: dispatch,
		popup:    ov.Box,
		selected: ov.Selected,
		open:     true,
	}, true
}

// Active reports whether the list is still open.
func (p *Picker) Active() bool { return p.open }

// Update handles a key press or a left click while the list is open. It returns
// the picker, which is closed after a choice, an Esc, or a click outside the list.
func (p *Picker) Update(msg tea.Msg) (*Picker, tea.Cmd) {
	if !p.open {
		return p, nil
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return p, p.key(tea.Key(msg))
	case tea.MouseClickMsg:
		return p, p.click(tea.Mouse(msg))
	}
	return p, nil
}

// key moves the choice, chooses it on Enter, or closes the list on Esc.
func (p *Picker) key(k tea.Key) tea.Cmd {
	switch k.Code {
	case tea.KeyEscape:
		p.open = false
	case tea.KeyEnter:
		return p.choose(p.sel)
	case tea.KeyUp:
		p.sel = max(p.sel-1, 0)
	case tea.KeyDown:
		if len(p.items) > 0 {
			p.sel = min(p.sel+1, len(p.items)-1)
		}
	}
	return nil
}

// click chooses the item under a left click, and closes the list on a click
// outside it.
func (p *Picker) click(m tea.Mouse) tea.Cmd {
	if m.Button != tea.MouseLeft {
		return nil
	}
	i, ok := p.itemAt(m.X, m.Y)
	if !ok {
		p.open = false
		return nil
	}
	return p.choose(i)
}

// itemAt returns the item index under cell (x, y) of the list as Draw placed it.
func (p *Picker) itemAt(x, y int) (int, bool) {
	b := p.box
	if b.w == 0 || x < b.x || x >= b.x+b.w || y < b.y || y >= b.y+b.h {
		return 0, false
	}
	return b.first + y - b.y, true
}

// choose closes the list and runs the action with item i. An index outside the
// list only closes it.
func (p *Picker) choose(i int) tea.Cmd {
	p.open = false
	if i < 0 || i >= len(p.items) || p.dispatch == nil {
		return nil
	}
	return p.dispatch(p.action, map[string]any{"name": p.items[i]})
}

// Draw paints the list on the rows above the status row, flush with the right
// edge. The selected item takes the selected style. A list taller than the frame
// shows the rows that end at the selection. Draw records where it placed the
// list, so a click can find its item.
func (p *Picker) Draw(g *grid.Grid) {
	p.box = box{}
	w, h := g.Width(), g.Height()
	rows := min(len(p.items), h-2)
	if !p.open || rows <= 0 {
		return
	}
	bw := 2 // a blank on each side of the widest item
	for _, it := range p.items {
		bw = max(bw, ansi.StringWidth(it)+2)
	}
	bw = min(bw, w)
	first := 0
	if p.sel >= rows {
		first = p.sel - rows + 1
	}
	p.box = box{x: w - bw, y: h - 1 - rows, w: bw, h: rows, first: first}
	for r := range rows {
		i := first + r
		st := p.popup
		if i == p.sel {
			st = p.selected
		}
		y := p.box.y + r
		g.Fill(p.box.x, p.box.x+bw, y, st)
		g.Put(p.box.x, y, w, " "+p.items[i], st)
	}
}
