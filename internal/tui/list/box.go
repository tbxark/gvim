package list

import (
	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/overlay"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
)

// BoxMaxWidth is the widest a text box is drawn.
const BoxMaxWidth = 76

// TextBox is a centred box of text: a title, the body lines and a footer. The
// tour, the calibration view and the event view use it. The title is on the top
// border and the footer on the bottom one, so the body lines fill the rows
// between them. A long body keeps the first rows it can show.
type TextBox struct {
	// Content returns the title, the body lines and the footer of the box at
	// every draw, so a view shows the live state it reads.
	Content func() (title string, lines []string, footer string)
	// OnKey, when set, sees each key press the box takes, and returns true to
	// close the box.
	OnKey func(k tea.Key) (done bool)
	// Data is the owner's value; the kit never reads it, so an owner can find
	// its own state again on the stack.
	Data any
	// Box is the box at the last draw; the zero value before the first draw.
	Box space.Rect

	active bool
	chrome style.Overlay
}

var _ overlay.Overlay = (*TextBox)(nil)

// NewTextBox returns an open text box drawn with chrome, whose body is content.
func NewTextBox(chrome style.Overlay, content func() (title string, lines []string, footer string)) *TextBox {
	return &TextBox{Content: content, active: true, chrome: chrome}
}

// draw paints the box with the border and title of the dialogs.
func (v *TextBox) draw(g *grid.Grid) {
	title, lines, footer := v.Content()
	box := Centred(g.Width(), g.Height(), BoxMaxWidth, len(lines)+2)
	v.Box = box
	if box.W == 0 || box.H < 3 {
		return
	}
	end := box.X + box.W - 2 // text stops before the padding cell at the right border
	v.chrome.PaintBox(g, box.X, box.Y, box.W, box.H, title)
	for k := range min(len(lines), box.H-2) {
		g.Put(box.X+2, box.Y+1+k, end, space.End(lines[k], end-(box.X+2)), v.chrome.Box)
	}
	v.chrome.PaintFooter(g, box.X, box.Y+box.H-1, box.W, footer)
}

// Update passes a key press to OnKey. It returns the box, or nil once OnKey
// reports done, so the stack pops it.
func (v *TextBox) Update(msg tea.Msg) (overlay.Overlay, tea.Cmd) {
	if k, ok := msg.(tea.KeyPressMsg); ok && v.OnKey != nil && v.OnKey(tea.Key(k)) {
		v.active = false
	}
	if !v.active {
		return nil, nil
	}
	return v, nil
}

// Draw paints the box over the frame.
func (v *TextBox) Draw(g *grid.Grid) { v.draw(g) }

// Active reports whether the box is still open.
func (v *TextBox) Active() bool { return v.active }
