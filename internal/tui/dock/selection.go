package dock

import (
	"image/color"
	"slices"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// The notices of a terminal text selection.
const (
	copiedSelectionNotice = "Copied the selection."
	noSelectionNotice     = "No selection."
)

// selPos is a selection position: an absolute combined row (prior transcript,
// then the shell's history, then the live screen rows) and a cell column.
type selPos struct {
	row, col int
}

// selState is the dock's text selection. A left press records a pending point: a
// bare click only focuses, and a drag turns it into a selection. Word and line
// selections are made by a double and a triple click in the scrollback view.
type selState struct {
	active        bool
	anchor, focus selPos
	// pending is a left press that has not become a drag or a word/line
	// selection yet; pressRow and pressCol are its body cell.
	pending            bool
	pressRow, pressCol int
	pressCount         int
	pressShift         bool
	// dragging is true while the press that started the selection is held.
	dragging bool
}

// Selection reports whether a text selection is active.
func (p *Panel) Selection() bool { return p.sel.active }

// SelectionRange is the start and end column of combined row abs that the
// selection covers, and false when it covers none of it.
func (p *Panel) SelectionRange(abs int) (start, end int, ok bool) {
	return p.selectionRange(abs)
}

// selectionRange is SelectionRange for the internal state.
func (p *Panel) selectionRange(abs int) (start, end int, ok bool) {
	if !p.sel.active {
		return 0, 0, false
	}
	a, f := p.sel.anchor, p.sel.focus
	if f.row < a.row || (f.row == a.row && f.col < a.col) {
		a, f = f, a
	}
	switch {
	case abs < a.row || abs > f.row:
		return 0, 0, false
	case a.row == f.row:
		return a.col, f.col, a.col != f.col
	case abs == a.row:
		return a.col, 1 << 30, true
	case abs == f.row:
		return 0, f.col, true
	default:
		return 0, 1 << 30, true
	}
}

// clearSelection drops the selection and any pending press.
func (p *Panel) clearSelection() { p.sel = selState{} }

// beginSelectionPress records a left press that may become a selection. It is
// called for the presses the dock handles itself: the scrollback view, and the
// live grid where no program owns the mouse, plus Shift presses (which bypass
// forwarding).
func (p *Panel) beginSelectionPress(row, col, count int, shift bool) {
	p.sel = selState{pending: true, pressRow: row, pressCol: col, pressCount: count, pressShift: shift}
}

// extendSelection turns a pending press into a drag selection at body cell
// (row, col), entering the scrollback view first when the shell is live. It
// reports whether it took the motion.
func (p *Panel) extendSelection(row, col int, l frame.Layout) (tea.Cmd, bool) {
	if !p.sel.pending && !p.sel.dragging {
		return nil, false
	}
	cols := max(p.contentCols(), 1)
	col = min(max(col, 0), cols-1)
	var cmd tea.Cmd
	if !p.scroll.on {
		if !p.canDragSelect() {
			p.clearSelection()
			return nil, true
		}
		// Entering the view clears any selection; keep the press that started the
		// drag across the transition.
		pressRow, pressCol, pressCount, pressShift := p.sel.pressRow, p.sel.pressCol, p.sel.pressCount, p.sel.pressShift
		cmd = p.enterScrollback()
		if !p.scroll.on {
			p.clearSelection()
			return cmd, true
		}
		p.sel = selState{pending: true, pressRow: pressRow, pressCol: pressCol, pressCount: pressCount, pressShift: pressShift}
	}
	if !p.sel.dragging {
		// The anchor is where the press was, mapped into the window that just
		// opened at the newest rows.
		anchorRow := p.scroll.top + p.sel.pressRow
		anchorCol := min(max(p.sel.pressCol, 0), cols-1)
		p.sel.anchor = selPos{row: anchorRow, col: anchorCol}
		p.sel.dragging = true
	}
	abs := p.scroll.top + row
	p.sel.focus = selPos{row: abs, col: col}
	p.sel.active = true
	p.sel.pending = false
	return cmd, true
}

// canDragSelect reports whether a drag on the live grid starts a selection.
func (p *Panel) canDragSelect() bool {
	return !p.effects.NoMouseDragSelects || p.sel.pressShift
}

// endSelectionPress ends the press that may have become a selection: a click
// that never moved leaves no selection.
func (p *Panel) endSelectionPress() {
	if p.sel.pending {
		p.sel = selState{}
	}
	p.sel.dragging = false
}

// contentCols is the shell's width in cells, as the layout gives it.
func (p *Panel) contentCols() int {
	cols, _ := p.contentSize()
	return cols
}

// selectUnit selects the word (count 2) or the row (count 3 and more) at body
// row row of the current scrollback window.
func (p *Panel) selectUnit(row, col, count int) {
	abs := p.scroll.top + row
	cells, ok := p.rowCells(abs)
	if !ok {
		return
	}
	text := cellsText(cells)
	start, end := 0, len(text)
	if count == 2 {
		start, end = wordAt(text, min(max(col, 0), len(text)))
	}
	p.sel = selState{
		active: true,
		anchor: selPos{row: abs, col: start},
		focus:  selPos{row: abs, col: end},
	}
}

// wordAt returns the bounds of the non-space word around cell col of text.
func wordAt(text []rune, col int) (start, end int) {
	if len(text) == 0 {
		return 0, 0
	}
	col = min(col, len(text)-1)
	if unicode.IsSpace(text[col]) {
		return col, col
	}
	start, end = col, col+1
	for start > 0 && !unicode.IsSpace(text[start-1]) {
		start--
	}
	for end < len(text) && !unicode.IsSpace(text[end]) {
		end++
	}
	return start, end
}

// cellsText returns the runes of a row of cells, one rune per cell (a blank cell
// is a space).
func cellsText(cells []terminal.Cell) []rune {
	out := make([]rune, len(cells))
	for i, c := range cells {
		if c.Rune == 0 {
			out[i] = ' '
			continue
		}
		out[i] = c.Rune
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return out
}

// rowCells returns the cells of a combined row: a history row from the loaded
// snapshot, or a live screen row. ok is false when the row is not available.
func (p *Panel) rowCells(abs int) ([]terminal.Cell, bool) {
	if abs < 0 {
		return nil, false
	}
	if abs < len(p.prior) {
		return p.prior[abs].Cells, true
	}
	hist := abs - len(p.prior)
	if hist < p.scroll.total {
		i := hist - p.scroll.from
		if i < 0 || i >= len(p.scroll.lines) {
			return nil, false
		}
		return p.scroll.lines[i].Cells, true
	}
	if p.term == nil {
		return nil, false
	}
	sc := p.term.Screen()
	r := hist - p.scroll.total
	if r < 0 || r >= sc.Rows {
		return nil, false
	}
	cells := make([]terminal.Cell, sc.Cols)
	for c := range sc.Cols {
		cells[c] = sc.Cell(r, c)
	}
	return cells, true
}

// selectionCopyMsg carries the text a copy of the selection took.
type selectionCopyMsg struct {
	gen  int
	text string
}

// copySelection copies the selected rows through the host's clipboard path. The
// rows are read in the command: the history part may need a snapshot the dock
// does not hold yet.
func (p *Panel) copySelection() tea.Cmd {
	if !p.sel.active {
		p.noticeOf(noSelectionNotice)
		return nil
	}
	if p.host.Copy == nil {
		p.noticeOf(noClipboardNotice)
		return nil
	}
	a, f := p.sel.anchor, p.sel.focus
	if f.row < a.row || (f.row == a.row && f.col < a.col) {
		a, f = f, a
	}
	t, gen := p.term, p.gen
	prior := slices.Clone(p.prior)
	total := p.scroll.total
	lo, hi := a.row, f.row
	aCol, fCol := a.col, f.col
	priorN := len(prior)
	// The history rows the selection needs, fetched as one bounded window.
	hStart := max(lo-priorN, 0)
	hEnd := min(hi+1-priorN, total)
	if hEnd-hStart > terminal.MaxScrollbackSnapshot {
		return nil
	}
	return func() tea.Msg {
		var histLines []terminal.Line
		if hEnd > hStart {
			histLines, _ = t.Scrollback(hStart, hEnd-hStart)
		}
		var sc terminal.Screen
		if hi >= priorN+total {
			sc = t.Screen()
		}
		var b strings.Builder
		for abs := lo; abs <= hi; abs++ {
			var cells []terminal.Cell
			switch {
			case abs < priorN:
				cells = prior[abs].Cells
			case abs-priorN < total:
				i := abs - priorN - hStart
				if i >= 0 && i < len(histLines) {
					cells = histLines[i].Cells
				}
			default:
				r := abs - priorN - total
				if r >= 0 && r < sc.Rows {
					cells = make([]terminal.Cell, sc.Cols)
					for c := range sc.Cols {
						cells[c] = sc.Cell(r, c)
					}
				}
			}
			start, end := 0, len(cells)
			if abs == lo {
				start = aCol
			}
			if abs == hi {
				end = fCol
			}
			start = min(max(start, 0), len(cells))
			end = min(max(end, start), len(cells))
			line := strings.TrimRight(string(cellsTextRaw(cells[start:end])), " ")
			if abs > lo {
				b.WriteByte('\n')
			}
			b.WriteString(line)
		}
		return selectionCopyMsg{gen: gen, text: strings.TrimRight(b.String(), "\n")}
	}
}

// cellsTextRaw returns the runes of cells without trimming trailing blanks.
func cellsTextRaw(cells []terminal.Cell) []rune {
	out := make([]rune, len(cells))
	for i, c := range cells {
		if c.Rune == 0 {
			out[i] = ' '
			continue
		}
		out[i] = c.Rune
	}
	return out
}

// paintSelection tints row y of the grid over the selection's columns. The dock's
// text starts at column 0, so a cell column is a grid column. bg is the selection
// background; with none the selected cells are reversed.
func (p *Panel) paintSelection(g *grid.Grid, y, w int, abs int, bg color.Color) {
	start, end, ok := p.selectionRange(abs)
	if !ok {
		return
	}
	start, end = max(start, 0), min(end, w)
	if end > start {
		tintRange(g, y, start, end, bg)
	}
}

// tintRange tints cells [start, end) of row y: the selection background keeps the
// cell's foreground, and without a background color the cells are reversed.
func tintRange(g *grid.Grid, y, start, end int, bg color.Color) {
	for c := start; c < end; c++ {
		cell := g.At(c, y)
		st := cell.Style
		if bg != nil {
			st.BG = bg
		} else {
			st.Reverse = true
		}
		g.Set(c, y, cell.Text, st)
	}
}
