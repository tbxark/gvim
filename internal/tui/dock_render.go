package tui

import (
	"github.com/tbxark/gvim/internal/terminal"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// paintDock draws the dock under the editor body: a title row, then the shell's
// screen, or the notice that says why no shell runs. A cell keeps the colours the
// shell set, and a colour the shell left at its default takes the editor's.
func (m *Model) paintDock(g *grid.Grid, l layout) {
	if !l.DockOn {
		return
	}
	bar := style.Cell(m.styles.ExplorerTitle)
	body := style.Cell(m.styles.Editor)
	g.Fill(0, l.W, l.DockY, bar)
	g.Put(1, l.DockY, l.W, dockTitle(&m.dock), bar)

	top, rows := l.DockY+1, l.DockH-1
	for y := range rows {
		g.Fill(0, l.W, top+y, body)
	}
	if m.dock.term == nil {
		g.Put(1, top, l.W, dockText(&m.dock), body)
		return
	}
	sc := m.dock.term.Screen()
	for r := range min(rows, sc.Rows) {
		paintShellRow(g, top+r, l.W, sc, r, body)
	}
}

// dockTitle is the title row of the dock.
func dockTitle(d *dockState) string {
	if d.exited {
		return "Terminal (exited)"
	}
	return "Terminal"
}

// dockText is what the dock shows in place of a screen: the notice that says why no
// shell runs, or that one is starting.
func dockText(d *dockState) string {
	if d.notice != "" {
		return d.notice
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

// dockCaret is the terminal caret in frame cells. It is the shell's cursor when the
// shell shows it and the cursor is inside the dock's shell rows.
func (m *Model) dockCaret() (x, y int, ok bool) {
	l := m.layout()
	if !l.DockOn || m.dock.term == nil || m.dock.exited {
		return 0, 0, false
	}
	c := m.dock.term.Screen().Cursor
	if !c.Visible || c.Col < 0 || c.Col >= l.W || c.Row < 0 || c.Row >= l.DockH-1 {
		return 0, 0, false
	}
	return c.Col, l.DockY + 1 + c.Row, true
}
