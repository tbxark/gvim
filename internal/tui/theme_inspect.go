package tui

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/style"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

// themeInfoMaxKeys is how many theme keys the inspector lists per side before
// it summarises the rest.
const themeInfoMaxKeys = 8

// themeInfoPage is the theme inspector popup: the theme keys whose resolved
// colors are the colors of the cell under the cursor (or the click), with the
// context facts that pick a role out of several keys with the same color.
type themeInfoPage struct {
	model  *Model
	lines  []themeInfoLine
	active bool
	box    space.Rect
}

// themeInfoLine is one row of the popup. A line with a color draws a swatch in
// that color before its text.
type themeInfoLine struct {
	text   string
	swatch *theme.Color
	bold   bool
}

var _ Overlay = (*themeInfoPage)(nil)

func (p *themeInfoPage) Active() bool { return p.active }

func (p *themeInfoPage) Close() { p.active = false }

// TakesEscape reports that Esc closes the popup (any key does).
func (p *themeInfoPage) TakesEscape() bool { return true }

// Update: the inspector is dismissed by any key and by a click outside it; the
// click or key is not consumed by the editor, as Fresh's popup passes through.
func (p *themeInfoPage) Update(msg tea.Msg) (Overlay, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		p.Close()
	case tea.MouseClickMsg:
		mo := tea.Mouse(msg)
		if !p.box.Contains(mo.X, mo.Y) {
			p.Close()
		}
	}
	if !p.active {
		return nil, nil
	}
	return p, nil
}

// Draw paints the popup near the inspected cell, clamped to the frame.
func (p *themeInfoPage) Draw(g *grid.Grid) {
	w := 64
	if w > g.Width() {
		w = g.Width()
	}
	if w < 8 {
		return
	}
	lines := p.lines
	maxH := max(g.Height()-2, 3)
	if len(lines)+3 > maxH {
		lines = lines[:max(1, maxH-3)]
	}
	h := len(lines) + 3
	x := 1
	y := 1
	p.box = space.Rect{X: x, Y: y, W: w, H: h}
	ov := p.model.styles.Overlay
	ov.PaintBox(g, x, y, w, h, "Theme at cursor")
	end := x + w - 2
	for i, line := range lines {
		row := y + 1 + i
		text := line.text
		if line.swatch != nil {
			sw := ov.Box
			sw.FG = style.RenderedColor(*line.swatch)
			g.Put(x+2, row, x+3, "██", sw)
			textX := x + 4
			st := ov.Box
			if line.bold {
				st.Bold = true
			}
			g.Put(textX, row, end, ansi.Truncate(text, max(end-textX+1, 1), "…"), st)
			continue
		}
		st := ov.Box
		if line.bold {
			st.Bold = true
		}
		g.Put(x+2, row, end, ansi.Truncate(text, max(end-x-1, 1), "…"), st)
	}
	ov.PaintFooter(g, x, y+h-1, w, "Esc or any key closes")
}

// inspectThemeAtCursor opens the inspector for the caret cell.
func (m *Model) inspectThemeAtCursor() {
	x, y, ok := m.caret()
	if !ok {
		// With no caret (no buffer, an overlay or the explorer has the focus)
		// there is still a frame cell to inspect: use the top-left of the editor
		// body when it exists.
		m.notice = "Theme: there is no editor caret to inspect"
		return
	}
	m.inspectThemeAt(x, y)
}

// paintCaptured repaints the frame once with per-cell theme-key capture, for the
// inspector. Every other paint (View and the hit map) runs without the capture
// buffer, so the normal path allocates nothing extra and records nothing.
func (m *Model) paintCaptured() *grid.Grid {
	g := grid.New(m.width, m.height)
	g.CaptureKeys()
	m.paintInto(g)
	return g
}

// inspectThemeAt opens the inspector for the frame cell (x, y).
func (m *Model) inspectThemeAt(x, y int) {
	g := m.paintCaptured()
	if x < 0 || y < 0 || x >= g.Width() || y >= g.Height() {
		return
	}
	cell := g.At(x, y)
	pos, doc, hasPos := m.inspectPositionAt(x, y)
	page := &themeInfoPage{model: m, active: true}
	add := func(text string, swatch *theme.Color, bold bool) {
		page.lines = append(page.lines, themeInfoLine{text: text, swatch: swatch, bold: bold})
	}
	add(fmt.Sprintf("Cell: row %d, col %d", y+1, x+1), nil, false)
	if hasPos {
		add(fmt.Sprintf("Buffer: line %d, col %d", pos.Line+1, pos.Col+1), nil, false)
	}
	// Context facts that pick a role among keys with the same color. The welcome
	// screen has no document.
	if doc != nil {
		cur := doc.Cursor()
		add(fmt.Sprintf("Current line: %s", yesNo(hasPos && pos.Line == cur.Line)), nil, false)
		if a, h, ok := doc.Selection(); ok && hasPos {
			from, to := buffer.Selection{Anchor: a, Head: h}.Ordered()
			inSel := !pos.Less(from) && !to.Less(pos)
			add("Selection: "+yesNo(inSel), nil, false)
		} else {
			add("Selection: no", nil, false)
		}
	}
	if m.findBar != nil {
		if q := m.findBar.Query(); q.Text != "" {
			add("Find: "+q.Text, nil, false)
		}
	}
	if hasPos {
		if diags := m.diagsAt(doc.Name(), pos); len(diags) > 0 {
			msg := ansi.Truncate(diags[0].Message, 44, "…")
			add(fmt.Sprintf("Diagnostic: %s %s", diagnosticSeverityName(diags[0].Severity), msg), nil, false)
		}
		if kind, ok := syntaxKindAt(doc, pos); ok {
			add("Syntax: "+strings.ToLower(kind.String()), nil, false)
		}
	}
	if names := gridAttributeNames(cell.Style); names != "" {
		add("Attributes: "+names, nil, false)
	}
	// The exact painted keys, when the surface recorded them during the capture
	// repaint. This is provenance, not a colour match: a style can be a mix (the
	// muted overlay roles) and a colour can name many keys.
	fgID, bgID := g.KeysAt(x, y)
	fgName, fgExact := m.inspectKeyLine("Painted by (fg)", fgID, add)
	bgName, bgExact := m.inspectKeyLine("Painted by (bg)", bgID, add)
	if fgExact && bgExact {
		add("Painted by: "+fgName+" on "+bgName, nil, true)
	}
	// Anything the surface did not attribute falls back to the rendered colour and
	// the context facts above; the labels say which is which.
	addedFg := fgExact
	addedBg := bgExact
	if !fgExact && cell.Style.FG != nil {
		addedFg = sectionKeys(m, "Foreground (matched by colour)", cell.Style.FG, add)
	}
	if !bgExact && cell.Style.BG != nil {
		addedBg = sectionKeys(m, "Background (matched by colour)", cell.Style.BG, add)
	}
	if !addedFg && !addedBg {
		add("No theme key resolves to this cell's colors", nil, false)
	}
	if m.theme.Name != "" {
		add("Theme: "+m.theme.Name, nil, false)
	}
	m.pushOverlay(page)
}

// inspectKeyLine adds the exact provenance line for one side of a cell and reports
// the key's display name and whether attribution existed. A missing theme value
// (a theme that leaves the key to a fallback) still names the key.
func (m *Model) inspectKeyLine(title string, id uint16, add func(string, *theme.Color, bool)) (string, bool) {
	if id == 0 {
		return "", false
	}
	section, key, ok := style.KeyName(id)
	if !ok {
		return "", false
	}
	name := section + "." + key
	if value, ok := m.theme.Value(section, key); ok {
		swatch := value
		add(title+": "+name+" "+value.Hex(), &swatch, true)
	} else {
		add(title+": "+name, nil, true)
	}
	return name, true
}

// sectionKeys lists the theme keys whose rendered color is c, with a swatch;
// it reports whether it found any.
func sectionKeys(m *Model, title string, c color.Color, add func(string, *theme.Color, bool)) bool {
	if c == nil {
		return false
	}
	matched := 0
	for _, key := range theme.Keys() {
		value, ok := m.theme.Value(key.Section, key.Key)
		if !ok {
			continue
		}
		if !style.MatchesRendered(c, value) {
			continue
		}
		matched++
		if matched > themeInfoMaxKeys {
			continue
		}
		swatch := value
		add(title+": "+key.Name()+" "+value.Hex(), &swatch, matched == 1)
	}
	if matched > themeInfoMaxKeys {
		add(fmt.Sprintf("%s: … and %d more", title, matched-themeInfoMaxKeys), nil, false)
	}
	return matched > 0
}

// inspectPositionAt maps a frame cell to a buffer position when it is inside the
// focused editor pane.
func (m *Model) inspectPositionAt(x, y int) (buffer.Pos, Document, bool) {
	doc := m.ed.Active()
	if doc == nil {
		return buffer.Pos{}, nil, false
	}
	l := m.layout()
	fp := l.FocusedPane()
	if fp.W <= 0 || fp.H <= 0 {
		return buffer.Pos{}, nil, false
	}
	if x < fp.X || x >= fp.X+fp.W || y < fp.Y || y >= fp.Y+fp.H {
		return buffer.Pos{}, doc, false
	}
	p := m.paneFor(fp, doc)
	pos := m.dragPos(p, doc, fp.W, fp.H, x-fp.X, y-fp.Y)
	return pos, doc, true
}

// syntaxKindAt returns the syntax kind of the rune at pos.
func syntaxKindAt(doc Document, pos buffer.Pos) (syntax.Kind, bool) {
	text := doc.Line(pos.Line)
	spans := doc.LineSpans(pos.Line)
	if len(spans) == 0 {
		return syntax.KindPlain, false
	}
	// Pos.Col counts runes; spans count bytes.
	byteCol := len(text)
	col := 0
	for i := range text {
		if col == pos.Col {
			byteCol = i
			break
		}
		col++
	}
	for _, span := range spans {
		if span.Start <= byteCol && byteCol < span.End {
			return span.Kind, true
		}
	}
	return syntax.KindPlain, true
}

// gridAttributeNames lists the attributes of a cell style.
func gridAttributeNames(st grid.Style) string {
	var names []string
	if st.Bold {
		names = append(names, "bold")
	}
	if st.Italic {
		names = append(names, "italic")
	}
	if st.Underline {
		names = append(names, "underlined")
	}
	if st.Faint {
		names = append(names, "dim")
	}
	if st.Reverse {
		names = append(names, "reversed")
	}
	if st.Strike {
		names = append(names, "strike")
	}
	return strings.Join(names, ", ")
}

// yesNo is "yes" or "no".
func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// diagnosticSeverityName names an LSP diagnostic severity (1..4) as the status
// row and the warnings list do.
func diagnosticSeverityName(severity int) string {
	switch severity {
	case 1:
		return "error"
	case 2:
		return "warning"
	case 3:
		return "info"
	case 4:
		return "hint"
	}
	return "diagnostic"
}
