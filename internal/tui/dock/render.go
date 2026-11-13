package dock

import (
	"fmt"
	"image/color"
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// Draw paints the dock in the editor column: a title row, then the shell's
// screen, or the notice that says why no shell runs. A cell keeps the colours the
// shell set, and a colour the shell left at its default takes the editor's.
func (p *Panel) Draw(g *grid.Grid, l frame.Layout, st style.Styles) {
	if !l.DockOn || l.EditorW <= 0 {
		return
	}
	x0, w := l.DockX, l.EditorW
	if cols, _ := p.contentSize(); cols > 0 {
		p.reflowPrior(cols)
	}
	bar := style.Cell(st.ExplorerTitle)
	body := style.Cell(st.Editor)
	g.Fill(x0, x0+w, l.DockY, bar)
	g.Put(x0+1, l.DockY, x0+w, p.title(), bar)

	top, rows := l.DockY+1, l.DockH-1
	for y := range rows {
		g.Fill(x0, x0+w, top+y, body)
	}
	var sc terminal.Screen
	if p.term != nil {
		sc = p.term.Screen()
	}
	if p.scroll.on {
		p.paintScrollback(g, x0, top, rows, w, sc, body, style.Cell(st.Selection).BG)
		return
	}
	if p.term == nil {
		g.Put(x0+1, top, x0+w, p.bodyText(), body)
		return
	}
	for r := range min(rows, sc.Rows) {
		paintShellRow(g, x0, top+r, w, sc, r, body)
		p.paintHoverRow(g, x0, top+r, r, w, false)
	}
}

// paintScrollback paints the scrollback view: a restarted shell's prior
// transcript, then the history window, then the live screen rows, with the
// selected row and the text selection on the selection background. A row the
// window needs but the snapshot does not hold yet is left blank until the next
// snapshot arrives.
func (p *Panel) paintScrollback(g *grid.Grid, x0, top, rows, w int, sc terminal.Screen, base grid.Style, sel color.Color) {
	// The marker row and the row count are the same for every row of the frame; sc is
	// the snapshot the shell's own Screen would give, so it is not taken again per row.
	marker := -1
	if p.exitMarkerPending() {
		termRows := p.scroll.screenRows
		if p.term != nil {
			termRows = sc.Rows
		}
		marker = p.exitMarkerRowAt(termRows)
	}
	combined := p.combinedWith(marker)
	for i := range rows {
		abs := p.scroll.top + i
		if abs >= combined {
			break
		}
		y := top + i
		hist := abs - len(p.prior)
		switch {
		case abs == marker:
			paintShellCells(g, x0, y, w, lineFromText(terminalExitMarker).Cells, base)
		case abs < len(p.prior):
			paintShellCells(g, x0, y, w, p.prior[abs].Cells, base)
		case hist < p.scroll.total:
			j := hist - p.scroll.from
			if j < 0 || j >= len(p.scroll.lines) {
				continue
			}
			paintShellCells(g, x0, y, w, p.scroll.lines[j].Cells, base)
		default:
			r := hist - p.scroll.total
			if r < 0 || r >= sc.Rows {
				continue
			}
			paintShellRow(g, x0, y, w, sc, r, base)
		}
		if sel != nil && abs == p.scroll.selected && !p.sel.active {
			tintRow(g, x0, y, w, sel)
		}
		p.paintSelection(g, x0, y, w, abs, sel)
		p.paintHoverRow(g, x0, y, abs, w, true)
	}
	p.paintSavedCursor(g, x0, top, rows, w)
}

// paintSavedCursor paints the restored snapshot's saved cursor as a reversed cell
// while the tab has no shell (the read-only restored view). On a dive the live
// shell owns the cursor, so the saved position stops being painted.
func (p *Panel) paintSavedCursor(g *grid.Grid, x0, top, rows, w int) {
	if !p.restored || p.term != nil || p.savedVisibleRows <= 0 || !p.savedCursor.Visible {
		return
	}
	abs := len(p.prior) - p.savedVisibleRows + p.savedCursor.Row
	if abs < p.scroll.top || abs >= p.scroll.top+rows {
		return
	}
	x := p.savedCursor.Col
	if x < 0 || x >= w {
		return
	}
	cell := g.At(x0+x, top+abs-p.scroll.top)
	st := cell.Style
	st.Reverse = !st.Reverse
	text := cell.Text
	if text == "" {
		text = " "
	}
	g.Set(x0+x, top+abs-p.scroll.top, text, st)
}

// tintRow sets the background of the w cells of row y at column x0 when they have
// no background of their own.
func tintRow(g *grid.Grid, x0, y, w int, bg color.Color) {
	if bg == nil {
		return
	}
	for c := range w {
		cell := g.At(x0+c, y)
		style := cell.Style
		if style.BG == nil {
			style.BG = bg
		}
		g.Set(x0+c, y, cell.Text, style)
	}
}

// paintShellCells paints one history row from its cells, clipped to w columns.
func paintShellCells(g *grid.Grid, x0, y, w int, cells []terminal.Cell, base grid.Style) {
	for c := 0; c < min(w, len(cells)); c++ {
		cell := cells[c]
		if g.Put(x0+c, y, x0+w, cellText(cell.Rune), shellStyle(cell.Style, base)) == 2 {
			c++
		}
	}
}

// title is the title row of the dock: the live shell, its frozen last screen, or
// the read-only scrollback view with its row window and search.
func (p *Panel) title() string {
	if p.scroll.on {
		return p.scrollbackTitle()
	}
	if p.exited {
		return p.exitedTitle()
	}
	return dockText("gvim.dock.title", "Terminal")
}

// exitedTitle is the dock title of a shell that quit: Fresh shows a restart
// call-to-action with a non-zero exit code in its status bar; the dock's
// in-scope surface is the tab title, which shows the non-zero code (or a signal)
// and keeps "exit 0" implicit exactly like Fresh.
func (p *Panel) exitedTitle() string {
	title := dockText("gvim.dock.title_exited", "Terminal (exited)")
	if suffix := p.exitSuffix(); suffix != "" {
		title = dockTextVars("gvim.dock.title_exited_suffix", "Terminal (exited, %{suffix})", map[string]any{"suffix": suffix})
	}
	if agent := p.launch.Agent; agent != "" {
		title += " — " + agent
	}
	return title
}

// exitSuffix names how a shell ended for the dock titles: "" for a clean/unknown
// exit (Fresh omits exit 0 too), "exit N" for a non-zero code and "signal" for a
// signal (Unix reports -1).
func (p *Panel) exitSuffix() string {
	if !p.exitKnown {
		return ""
	}
	if p.exitCode < 0 {
		return dockText("gvim.dock.exit_signal", "signal")
	}
	if p.exitCode == 0 {
		return ""
	}
	return dockTextVars("gvim.dock.exit_code", "exit %{code}", map[string]any{"code": p.exitCode})
}

// scrollbackTitle is the title of the scrollback view: its visible rows of the
// total, and the search when one is open.
func (p *Panel) scrollbackTitle() string {
	combined := p.combined()
	rows := max(p.bodyRows(), 1)
	first := min(p.scroll.top+1, combined)
	last := min(p.scroll.top+rows, combined)
	name := dockText("gvim.dock.scrollback", "Scrollback")
	if p.restored && p.term == nil {
		name = dockText("gvim.dock.scrollback_restored", "Scrollback (restored)")
		if p.reflowed {
			name = dockText("gvim.dock.scrollback_restored_rewrapped", "Scrollback (restored, rewrapped)")
		}
	}
	if p.exited {
		// The restored read-only view is the scrollback, so the exit status rides
		// on this title too (Fresh shows it in its restart status-bar indicator).
		// A live shell's auto-shown transcript names the exit as well, so the title
		// still says the shell is gone now that the frozen screen is a pager.
		if p.term != nil {
			name = dockText("gvim.dock.scrollback_exited", "Scrollback (exited)")
		}
		if suffix := p.exitSuffix(); suffix != "" {
			name += ", " + suffix
		}
	}
	// The agent name is part of the record a restart rejoins, so it stays visible
	// on the dead transcript too.
	if agent := p.launch.Agent; agent != "" {
		name += " — " + agent
	}
	title := fmt.Sprintf("%s — rows %d-%d of %d (Ctrl+Space returns)", name, first, last, combined)
	if p.scroll.typing {
		return title + " — /" + p.scroll.searchInput
	}
	if p.scroll.search == "" {
		return title
	}
	pos := 0
	if len(p.scroll.matches) > 0 {
		pos = p.scroll.match + 1
	}
	opts := ""
	if p.scroll.regex {
		opts += " rx"
	}
	if p.scroll.matchCase {
		opts += " case"
	}
	return fmt.Sprintf("%s — /%s%s %d/%d", title, p.scroll.search, opts, pos, len(p.scroll.matches))
}

// bodyText is what the dock shows in place of a screen: the notice that says why no
// shell runs, or that one is starting.
func (p *Panel) bodyText() string {
	if p.notice != "" {
		return p.notice
	}
	return startingShellNotice()
}

// paintShellRow draws row r of the shell screen sc at grid row y, from column x0
// to x0+w. A wide character takes the next cell as well, and that cell is its
// continuation, so the paint skips it.
func paintShellRow(g *grid.Grid, x0, y, w int, sc terminal.Screen, r int, base grid.Style) {
	for c := 0; c < min(w, sc.Cols); c++ {
		cell := sc.Cell(r, c)
		if g.Put(x0+c, y, x0+w, cellText(cell.Rune), shellStyle(cell.Style, base)) == 2 {
			c++
		}
	}
}

// asciiText holds the one-rune strings of ASCII, so painting a screen of mostly ASCII
// text does not allocate a string per cell per frame.
var asciiText = func() (t [utf8.RuneSelf]string) {
	for i := range t {
		t[i] = string(rune(i))
	}
	return t
}()

// cellText is the text a shell cell paints: its rune, or a space for an empty cell.
func cellText(r rune) string {
	switch {
	case r == 0:
		return " "
	case uint32(r) < utf8.RuneSelf:
		return asciiText[r]
	}
	return string(r)
}

// shellStyle is the grid style of a shell cell. A colour the shell did not set takes
// the base colour.
func shellStyle(st terminal.Style, base grid.Style) grid.Style {
	out := grid.Style{FG: st.Fg, BG: st.Bg, Bold: st.Bold, Underline: st.Underline, Reverse: st.Reverse}
	if out.FG == nil {
		out.FG = base.FG
	}
	if out.BG == nil {
		out.BG = base.BG
	}
	return out
}

// Caret is the terminal caret in frame cells. It is the shell's cursor when the shell
// shows it and the cursor is inside the dock's shell rows.
func (p *Panel) Caret(l frame.Layout) (x, y int, ok bool) {
	if !l.DockOn || p.term == nil || p.exited || p.scroll.on {
		return 0, 0, false
	}
	c := p.term.Screen().Cursor
	if !c.Visible || c.Col < 0 || c.Col >= l.EditorW || c.Row < 0 || c.Row >= l.DockH-1 {
		return 0, 0, false
	}
	return l.DockX + c.Col, l.DockY + 1 + c.Row, true
}
