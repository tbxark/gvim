package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/list"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/theme"
)

// themeColorPicker is the theme editor's colour picker: a swatch grid of the
// working theme's own colours plus the named ANSI colours. Moving the selection
// live-previews the candidate on the edited key; Enter applies it as a normal
// colour edit (the editor becomes dirty and can save), Esc restores the value the
// editor had before the picker opened, and a click on a row's swatch in the editor
// opens it too. Mouse clicks and drags select and preview; they do not commit, so
// the keyboard remains the confirm step (the same as Enter in the editor's list).
//
// The existing Enter hex prompt and Ctrl+A attributes prompt are untouched.
type themeColorPicker struct {
	editor *themeEditorPage
	key    theme.Key
	prior  theme.Color
	colors []theme.Color

	sel      int
	dragging bool
	preview  bool // a candidate was previewed on the frame
	active   bool
	box      space.Rect
	gridX    int
	gridY    int
	cols     int
}

var _ Overlay = (*themeColorPicker)(nil)

// pickerCols is the swatch grid's width in chips.
const pickerCols = 8

// openColorPicker opens the picker for the highlighted theme key.
func (p *themeEditorPage) openColorPicker() tea.Cmd {
	row, ok := p.rowAtSel()
	if !ok {
		return nil
	}
	prior, _ := p.working.Value(row.key.Section, row.key.Key)
	return p.pushOverlay(newThemeColorPicker(p, row.key, prior))
}

// newThemeColorPicker builds the overlay over the editor's working theme.
func newThemeColorPicker(editor *themeEditorPage, key theme.Key, prior theme.Color) *themeColorPicker {
	palette := pickerPalette(editor.working)
	sel := 0
	for i, c := range palette {
		if sameThemeColor(c, prior) {
			sel = i
			break
		}
	}
	return &themeColorPicker{editor: editor, key: key, prior: prior, colors: palette, sel: sel, active: true}
}

// sameThemeColor reports whether two theme values paint the same, attributes
// included.
func sameThemeColor(a, b theme.Color) bool {
	return a.String() == b.String() && theme.FormatModifiers(a.Modifiers) == theme.FormatModifiers(b.Modifiers)
}

// pickerPalette is the candidate colours: the working theme's distinct colors in
// key table order, then the named ANSI colours, deduplicated by their display form
// (the attributes of two values with the same color differ, but a color picker
// chooses a color; Ctrl+A edits the attributes of the listed key).
func pickerPalette(th theme.Theme) []theme.Color {
	seen := map[string]bool{}
	out := make([]theme.Color, 0, 64)
	add := func(c theme.Color) {
		id := c.String()
		if seen[id] {
			return
		}
		seen[id] = true
		out = append(out, c)
	}
	for _, key := range th.Keys() {
		if value, ok := th.Value(key.Section, key.Key); ok {
			add(value)
		}
	}
	for _, name := range theme.NamedColorNames() {
		if value, err := theme.ParseColorText(name); err == nil {
			add(value)
		}
	}
	return out
}

// Active reports whether the picker is open.
func (p *themeColorPicker) Active() bool { return p.active }

// Close closes the picker.
func (p *themeColorPicker) Close() { p.active = false }

// TakesEscape reports that Esc cancels the picker itself (restoring the value).
func (p *themeColorPicker) TakesEscape() bool { return true }

// Update handles the picker's input.
func (p *themeColorPicker) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		p.keyPress(msg)
	case tea.MouseClickMsg:
		mo := tea.Mouse(msg)
		if mo.Button != tea.MouseLeft {
			break
		}
		if i, ok := p.chipAt(mo.X, mo.Y); ok {
			p.dragging = true
			p.selectChip(i)
			break
		}
		p.cancel()
	case tea.MouseMotionMsg:
		if !p.dragging {
			break
		}
		if i, ok := p.chipAt(msg.X, msg.Y); ok {
			p.selectChip(i)
		}
	case tea.MouseReleaseMsg:
		p.dragging = false
		if i, ok := p.chipAt(msg.X, msg.Y); ok {
			p.selectChip(i)
		}
	}
	if !p.active {
		return nil, nil
	}
	return p, nil
}

// keyPress handles one key press: arrows and pages move the selection and preview it,
// Enter applies it as an edit, Esc restores the previous value and closes.
func (p *themeColorPicker) keyPress(msg tea.KeyPressMsg) {
	switch tea.Key(msg).Keystroke() {
	case "left":
		p.move(-1, 0)
	case "right":
		p.move(1, 0)
	case "up":
		p.move(0, -1)
	case "down":
		p.move(0, 1)
	case "home":
		p.selectChip(0)
	case "end":
		p.selectChip(len(p.colors) - 1)
	case "pgup":
		p.selectChip(max(p.sel-pickerCols*4, 0))
	case "pgdown":
		p.selectChip(min(p.sel+pickerCols*4, len(p.colors)-1))
	case "enter":
		p.apply()
	case "esc":
		p.cancel()
	}
}

// move shifts the selection by one chip, staying on the grid's columns.
func (p *themeColorPicker) move(dx, dy int) {
	index := p.sel + dx + dy*pickerCols
	if index < 0 || index >= len(p.colors) {
		return
	}
	p.selectChip(index)
}

// selectChip highlights a candidate and previews it on the frame.
func (p *themeColorPicker) selectChip(i int) {
	if i < 0 || i >= len(p.colors) {
		return
	}
	p.sel = i
	p.preview = true
	p.editor.previewValue(p.key, p.colors[i])
}

// apply commits the highlighted colour as a normal editor edit.
func (p *themeColorPicker) apply() {
	if p.sel >= 0 && p.sel < len(p.colors) {
		p.editor.setColor(p.key.Name(), p.colors[p.sel])
	}
	p.Close()
}

// cancel restores the value the editor had before the picker opened and closes.
func (p *themeColorPicker) cancel() {
	if p.preview {
		p.editor.reapplyValues()
	}
	p.Close()
}

// layout computes the box and the grid's origin from it.
func (p *themeColorPicker) layout() {
	cols := min(pickerCols, max(len(p.colors), 1))
	rows := (len(p.colors) + cols - 1) / cols
	w, h := cols*3+3, rows+4
	box := list.Centred(p.editor.model.width, p.editor.model.height, w, h)
	p.box = box
	p.cols = cols
	p.gridX = box.X + 2
	p.gridY = box.Y + 1
}

// chipAt maps a screen position to a swatch index.
func (p *themeColorPicker) chipAt(x, y int) (int, bool) {
	p.layout()
	if p.box.W == 0 {
		return 0, false
	}
	col := (x - p.gridX) / 3
	if x < p.gridX || y < p.gridY || col < 0 || col >= p.cols || y >= p.gridY+p.box.H-3 {
		return 0, false
	}
	row := y - p.gridY
	i := row*p.cols + col
	if i < 0 || i >= len(p.colors) {
		return 0, false
	}
	return i, true
}

// Draw paints the picker.
func (p *themeColorPicker) Draw(g *grid.Grid) {
	p.editor.layout()
	p.layout()
	box := p.box
	if box.W == 0 || box.H < 4 {
		return
	}
	ov := p.editor.model.styles.Overlay
	ov.PaintBox(g, box.X, box.Y, box.W, box.H, "Theme colour — "+p.key.Name())
	for i, c := range p.colors {
		if i >= (box.H-3)*p.cols {
			break
		}
		x := p.gridX + (i%p.cols)*3
		y := p.gridY + i/p.cols
		st := grid.Style{FG: style.RenderedColor(c), BG: style.RenderedColor(c)}
		marker := " "
		if i == p.sel {
			marker = "›"
			st = grid.Style{FG: style.RenderedColor(c), BG: style.RenderedColor(c), Bold: true}
		}
		g.Put(x, y, box.X+box.W-1, marker, ov.Focus)
		g.Put(x+1, y, box.X+box.W-1, "██", st)
	}
	selected := ""
	if p.sel >= 0 && p.sel < len(p.colors) {
		selected = "Selected: " + p.colors[p.sel].Hex()
		if attrs := theme.FormatModifiers(p.colors[p.sel].Modifiers); attrs != "" {
			selected += " " + attrs
		}
	}
	g.Put(box.X+2, box.Y+box.H-3, box.X+box.W-2, ansi.Truncate(selected, box.W-4, "…"), ov.Muted)
	ov.PaintFooter(g, box.X, box.Y+box.H-1, box.W, "Arrows select · Enter apply · Esc restore · click/drag preview")
}
