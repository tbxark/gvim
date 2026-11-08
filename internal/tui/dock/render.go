package dock

import (
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// Draw paints the dock under the editor body: a title row, then the shell's
// screen, or the notice that says why no shell runs. A cell keeps the colours the
// shell set, and a colour the shell left at its default takes the editor's.
func (p *Panel) Draw(g *grid.Grid, l frame.Layout, st style.Styles) {
	if !l.DockOn {
		return
	}
	bar := style.Cell(st.ExplorerTitle)
	body := style.Cell(st.Editor)
	g.Fill(0, l.W, l.DockY, bar)
	g.Put(1, l.DockY, l.W, p.title(), bar)

	top, rows := l.DockY+1, l.DockH-1
	for y := range rows {
		g.Fill(0, l.W, top+y, body)
	}
	if p.term == nil {
		g.Put(1, top, l.W, p.dockText(), body)
		return
	}
	sc := p.term.Screen()
	for r := range min(rows, sc.Rows) {
		paintShellRow(g, top+r, l.W, sc, r, body)
	}
}

// title is the title row of the dock.
func (p *Panel) title() string {
	if p.exited {
		return "Terminal (exited)"
	}
	return "Terminal"
}

// dockText is what the dock shows in place of a screen: the notice that says why no
// shell runs, or that one is starting.
func (p *Panel) dockText() string {
	if p.notice != "" {
		return p.notice
	}
	return startingShellNotice
}

// paintShellRow draws row r of the shell screen sc at grid row y, from column 0 to w.
// A wide character takes the next cell as well, and that cell is its continuation, so
// the paint skips it.
func paintShellRow(g *grid.Grid, y, w int, sc terminal.Screen, r int, base grid.Style) {
	for c := 0; c < min(w, sc.Cols); c++ {
		cell := sc.Cell(r, c)
		text := " "
		if cell.Rune != 0 {
			text = string(cell.Rune)
		}
		if g.Put(c, y, w, text, shellStyle(cell.Style, base)) == 2 {
			c++
		}
	}
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
	if !l.DockOn || p.term == nil || p.exited {
		return 0, 0, false
	}
	c := p.term.Screen().Cursor
	if !c.Visible || c.Col < 0 || c.Col >= l.W || c.Row < 0 || c.Row >= l.DockH-1 {
		return 0, 0, false
	}
	return c.Col, l.DockY + 1 + c.Row, true
}
