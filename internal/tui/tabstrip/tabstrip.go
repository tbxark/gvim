// Package tabstrip is the tab strip screen of the gvim UI: it paints the strip
// that the frame layout placed and maps a pointer column to the tab under it.
//
// The geometry of the strip (the run rectangles, the scroll window) is computed
// by the frame kit, because the layout reserves the strip row; this package paints
// those runs with the theme's styles and interprets them for the pointer, so the
// painter and the hit test cannot drift apart.
//
// The drag of a tab belongs to the root model: it moves tabs through the editor
// port and needs the pane drop zones. The package is a leaf: it imports the grid,
// the frame kit and the style kit.
package tabstrip

import (
	"strings"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/internal/tui/style"
)

// Draw paints the strip s on row y of g, from column x0. The strip fills the row
// from x0. The label and the close glyph of the active tab take the active style,
// the other tabs and the plus take the tab style, and the markers take the bar
// style.
//
// hover is the run under the pointer; its text is underlined, so
// a tab, its close glyph and the plus say they are targets before they are pressed.
func Draw(g *grid.Grid, st style.Styles, x0, y int, s frame.Strip, _ bool, hover frame.HitTarget) {
	barStyle := style.Cell(st.TabBar)
	g.Fill(x0, g.Width(), y, barStyle)
	tab, active := style.Cell(st.Tab), style.Cell(st.TabActive)
	for i, r := range s.Runs {
		cell := tab
		switch {
		case r.Kind == frame.HitTabScrollBefore || r.Kind == frame.HitTabScrollAfter:
			cell = barStyle
		case r.Kind != frame.HitTabNew && r.Index == s.Active:
			// The active tab carries weight as well as its colour, so it reads as the
			// current one in a theme whose two tab colours are close.
			cell = active
			cell.Bold = true
		}
		if hover.Kind != frame.HitNone && hover.Kind == r.Kind && hover.Index == r.Index {
			cell.Underline = true
		}
		g.Put(r.X, y, g.Width(), r.Text, cell)
		// Put the divider at the start of the next tab, leaving a blank after ×.
		boundary := i > 0 && r.Kind == frame.HitTab && strings.HasPrefix(r.Text, " ") && s.Runs[i-1].Kind == frame.HitTab && s.Runs[i-1].Index != r.Index && s.Runs[i-1].X+s.Runs[i-1].W == r.X
		if boundary {
			sep := barStyle
			sep.Underline = cell.Underline
			if fg := style.Cell(st.Scrollbar).FG; fg != nil {
				sep.FG = fg
			} else {
				sep.Faint = true
			}
			g.Set(r.X, y, "│", sep)
		}
	}
}

// PassedMidpoint reports whether the pointer at column x has crossed the middle
// of tab to, coming from tab from. A neighbour that is wider than the dragged tab
// is still under the pointer after the swap, so the swap must not repeat on the
// next cell. The pointer outside the tab (left of the first, right of the last)
// always counts.
func PassedMidpoint(runs []frame.Run, to, from, x int) bool {
	lo, hi := -1, -1
	for _, r := range runs {
		if (r.Kind != frame.HitTab && r.Kind != frame.HitTabClose) || r.Index != to {
			continue
		}
		if lo < 0 || r.X < lo {
			lo = r.X
		}
		hi = max(hi, r.X+r.W)
	}
	if lo < 0 || x < lo || x >= hi {
		return true
	}
	mid := lo + (hi-lo)/2
	if to > from {
		return x >= mid
	}
	return x < mid
}

// TabAt is the index of the tab under column x of the strip: the tab whose label
// or close button covers it, the first tab for a column left of the tabs, and the
// last for a column right of them. It is -1 when the strip shows no tab.
func TabAt(runs []frame.Run, x int) int {
	first, last := -1, -1
	for _, r := range runs {
		if r.Kind != frame.HitTab && r.Kind != frame.HitTabClose {
			continue
		}
		if r.Covers(x) {
			return r.Index
		}
		if first < 0 || r.Index < first {
			first = r.Index
		}
		last = max(last, r.Index)
	}
	if first < 0 {
		return -1
	}
	for _, r := range runs {
		if r.Kind == frame.HitTab && r.Index == first && x < r.X {
			return first
		}
	}
	return last
}
