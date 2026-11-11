package dock

import (
	"image/color"
	"slices"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// The notices of a terminal text selection.
// The copy notices, translated at display time.

func copiedSelectionNotice() string { return i18n.T("", "gvim.terminal.copied_selection", nil) }
func noSelectionNotice() string     { return i18n.T("", "gvim.scrollback.no_selection", nil) }

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
	// unit is 2 when a double click selected a word and 3 or more when a triple
	// click selected a whole row; a drag then extends by words or rows instead
	// of by characters. unitStart and unitEnd are the clicked unit's bounds, and
	// a drag keeps the whole unit selected whichever way it goes.
	unit               int
	unitStart, unitEnd selPos
	// dir is the direction a drag that left the body band scrolls: -1 above,
	// +1 below, 0 inside. autoRunning says the repeat tick runs for dragSeq.
	dir         int
	autoRunning bool
}

// dragScrollInterval is how often a drag held above or below the body band
// scrolls the scrollback one row: about 12 rows a second, bounded and without
// acceleration.
const dragScrollInterval = 80 * time.Millisecond

// dragScrollMsg is one repeat tick of a drag's auto-scroll. seq invalidates the
// ticks of an older drag.
type dragScrollMsg struct{ seq int }

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

// clearSelection drops the selection and any pending press, and stops the drag
// auto-scroll.
func (p *Panel) clearSelection() {
	p.sel = selState{}
	p.dragSeq++
}

// beginSelectionPress records a left press that may become a selection. It is
// called for the presses the dock handles itself: the scrollback view, and the
// live grid where no program owns the mouse, plus Shift presses (which bypass
// forwarding).
func (p *Panel) beginSelectionPress(row, col, count int, shift bool) {
	p.stopAutoScroll()
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
		p.sel.dragging = true
		p.sel.pending = false
		if p.sel.unit >= 2 {
			// A word or row selection keeps its clicked unit selected and extends
			// by whole units from there.
			abs := p.scroll.top + row
			p.extendUnitSelection(abs, col)
			return cmd, true
		}
		// The anchor is where the press was, mapped into the window that just
		// opened at the newest rows.
		anchorRow := p.scroll.top + p.sel.pressRow
		anchorCol := min(max(p.sel.pressCol, 0), cols-1)
		p.sel.anchor = selPos{row: anchorRow, col: anchorCol}
	}
	abs := p.scroll.top + row
	if p.sel.unit >= 2 {
		p.extendUnitSelection(abs, col)
	} else {
		p.sel.focus = selPos{row: abs, col: col}
	}
	p.sel.active = true
	p.sel.pending = false
	return cmd, true
}

// extendUnitSelection extends a word (unit 2) or row (unit 3) selection to the
// unit under (abs, col), keeping the clicked unit whole: the anchor and focus
// become the range's two ends, so a drag left covers the clicked word too.
func (p *Panel) extendUnitSelection(abs, col int) {
	cells, ok := p.rowCells(abs)
	if !ok {
		p.sel.focus = selPos{row: abs, col: col}
		return
	}
	text := cellsText(cells)
	start, end := 0, len(text)
	if p.sel.unit == 2 {
		start, end = wordAt(text, min(max(col, 0), max(len(text)-1, 0)))
	}
	unitStart, unitEnd := p.sel.unitStart, p.sel.unitEnd
	before := abs < unitStart.row || (abs == unitStart.row && start < unitStart.col)
	after := abs > unitEnd.row || (abs == unitEnd.row && end > unitEnd.col)
	switch {
	case before:
		p.sel.anchor = selPos{row: abs, col: start}
		p.sel.focus = unitEnd
	case after:
		p.sel.anchor = unitStart
		p.sel.focus = selPos{row: abs, col: end}
	default:
		p.sel.anchor = unitStart
		p.sel.focus = unitEnd
	}
	p.sel.active = true
}

// dragSelectionMove extends a selection for a drag motion that may be outside
// the visible body rows and manages the bounded auto-scroll repeat. dir is -1
// above the band, +1 below it and 0 inside.
func (p *Panel) dragSelectionMove(row, col, dir int, l frame.Layout) tea.Cmd {
	cmd, _ := p.extendSelection(row, col, l)
	if !p.scroll.on || !p.sel.dragging {
		p.stopAutoScroll()
		return cmd
	}
	switch {
	case dir == 0:
		p.stopAutoScroll()
		return cmd
	case p.sel.dir != dir || !p.sel.autoRunning:
		p.sel.dir = dir
		return tea.Batch(cmd, p.startAutoScroll())
	}
	return cmd
}

// startAutoScroll starts the retained auto-scroll tick for the drag.
func (p *Panel) startAutoScroll() tea.Cmd {
	if p.sel.autoRunning {
		return nil
	}
	p.sel.autoRunning = true
	p.dragSeq++
	seq := p.dragSeq
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg { return dragScrollMsg{seq: seq} })
}

// stopAutoScroll ends the repeat, if it runs.
func (p *Panel) stopAutoScroll() {
	p.sel.autoRunning = false
	p.sel.dir = 0
	p.dragSeq++
}

// dragScrollTick scrolls the scrollback one row and extends the selection to the
// window edge. It stops at both ends of the output, when the drag ended and for
// ticks of an older drag.
func (p *Panel) dragScrollTick(msg dragScrollMsg) tea.Cmd {
	if msg.seq != p.dragSeq || !p.sel.dragging || p.sel.dir == 0 || !p.scroll.on {
		return nil
	}
	rows := max(p.bodyRows(), 1)
	combined := p.combined()
	if combined == 0 {
		p.stopAutoScroll()
		return nil
	}
	if p.sel.dir < 0 {
		if p.scroll.top <= 0 {
			p.stopAutoScroll()
			return nil
		}
		p.scroll.top--
	} else {
		if p.scroll.top >= p.maxTop() {
			p.stopAutoScroll()
			return nil
		}
		p.scroll.top++
	}
	edge := p.scroll.top
	if p.sel.dir > 0 {
		edge = min(p.scroll.top+rows-1, combined-1)
	}
	p.scroll.selected = edge
	if p.sel.unit >= 2 {
		col := 0
		if p.sel.dir > 0 {
			if cells, ok := p.rowCells(edge); ok {
				col = max(len(cellsText(cells))-1, 0)
			}
		}
		p.extendUnitSelection(edge, col)
	} else {
		p.sel.focus = selPos{row: edge, col: p.sel.focus.col}
		p.sel.active = true
	}
	return tea.Batch(p.afterScroll(), p.nextAutoScroll())
}

// nextAutoScroll schedules the next repeat tick of the running drag.
func (p *Panel) nextAutoScroll() tea.Cmd {
	if !p.sel.autoRunning {
		return nil
	}
	seq := p.dragSeq
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg { return dragScrollMsg{seq: seq} })
}

// SelectionDragging reports whether a press that may extend a selection is held.
// The root keeps forwarding motion and release messages to the dock while it is
// set, even when the pointer leaves the dock's rows, so a drag keeps extending
// past the dock's edges (see the auto-scroll in input.go).
func (p *Panel) SelectionDragging() bool { return p.sel.pending || p.sel.dragging }

// canDragSelect reports whether a drag on the live grid starts a selection.
func (p *Panel) canDragSelect() bool {
	return !p.effects.NoMouseDragSelects || p.sel.pressShift
}

// endSelectionPress ends the press that may have become a selection: a click
// that never moved leaves no selection.
func (p *Panel) endSelectionPress() {
	p.stopAutoScroll()
	if p.sel.pending && p.sel.unit < 2 {
		p.sel = selState{}
	}
	p.sel.pending = false
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
	unit := 3
	if count == 2 {
		unit = 2
		start, end = wordAt(text, min(max(col, 0), max(len(text)-1, 0)))
	}
	p.stopAutoScroll()
	startPos := selPos{row: abs, col: start}
	endPos := selPos{row: abs, col: end}
	p.sel = selState{
		active: true, anchor: startPos, focus: endPos,
		pending: true, pressRow: row, pressCol: col, pressCount: count,
		unit: unit, unitStart: startPos, unitEnd: endPos,
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
		p.noticeOf(noSelectionNotice())
		return nil
	}
	if p.host.Copy == nil {
		p.noticeOf(noClipboardNotice())
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

// cellsTextRaw returns the printed runes of cells without trimming trailing blanks:
// the right half of a wide character (a cell with no rune) adds nothing.
func cellsTextRaw(cells []terminal.Cell) []rune {
	out := make([]rune, 0, len(cells))
	for _, c := range cells {
		if c.Rune != 0 {
			out = append(out, c.Rune)
		}
	}
	return out
}

// paintSelection tints row y of the grid over the selection's columns, at column
// x0 of the frame. A cell column is a grid column offset by the dock's first
// column. bg is the selection background; with none the selected cells are reversed.
func (p *Panel) paintSelection(g *grid.Grid, x0, y, w int, abs int, bg color.Color) {
	start, end, ok := p.selectionRange(abs)
	if !ok {
		return
	}
	start, end = max(start, 0), min(end, w)
	if end > start {
		tintRange(g, x0, y, start, end, bg)
	}
}

// tintRange tints cells [start, end) of row y, starting at column x0: the selection
// background keeps the cell's foreground, and without a background color the cells
// are reversed.
func tintRange(g *grid.Grid, x0, y, start, end int, bg color.Color) {
	for c := start; c < end; c++ {
		cell := g.At(x0+c, y)
		st := cell.Style
		if bg != nil {
			st.BG = bg
		} else {
			st.Reverse = true
		}
		g.Set(x0+c, y, cell.Text, st)
	}
}
