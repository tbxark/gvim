package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/ui/grid"
	"github.com/tbxark/gvim/internal/ui/icons"
	"github.com/tbxark/gvim/internal/ui/space"
	"github.com/tbxark/gvim/internal/ui/style"
)

// Tab strip glyphs and labels (spec section 3). The modified marker is a dot
// before the name. The close and new-tab buttons come from the icon set (package
// icons): one cell each, so the tab widths do not depend on the mode.
const (
	previewSuffix = " (preview)" // the label of a preview tab
	modifiedMark  = "●"          // before the name of a modified tab
	// comparePairMark is before the name of each of the two tabs of a compare view.
	comparePairMark = "⇄"
)

// stripLabel is the name of one tab as the strip shows it: the file icon (when the
// root gave one) before the name, the modified marker before that, and " (preview)"
// after the name for a preview tab. An unnamed tab reads as noNameLabel.
func stripLabel(t TabInfo) string {
	base := strings.TrimSuffix(t.Name, previewSuffix)
	if base == "" {
		base = noNameLabel
	}
	if t.Icon != "" {
		base = t.Icon + " " + base
	}
	if t.Modified {
		base = modifiedMark + " " + base
	}
	if t.Preview || strings.HasSuffix(t.Name, previewSuffix) {
		base += previewSuffix
	}
	return base
}

// stripTabs is the editor's tabs with the file type icon of the icon set filled in,
// for the strip to place and draw. Plain mode has no file icons, so the tabs pass
// through as they are.
func (m *Model) stripTabs() []TabInfo {
	tabs := m.ed.Tabs()
	plain := m.view.icons.FileIcon("", false, false) == ""
	pair := m.splits.comparing && !m.splits.root.leaf()
	if plain && !pair {
		return tabs
	}
	out := slices.Clone(tabs)
	for i := range out {
		if !plain {
			out[i].Icon = m.view.icons.FileIcon(strings.TrimSuffix(out[i].Name, previewSuffix), false, false)
		}
	}
	if pair {
		// The two files of a comparison are two tabs, so the strip says they belong together.
		for _, n := range []*splitNode{m.splits.root.a, m.splits.root.b} {
			if n.tab >= 0 && n.tab < len(out) {
				out[n.tab].Name = comparePairMark + " " + out[n.tab].Name
			}
		}
	}
	return out
}

// strip is the tab strip of one row, placed by placeStrip. Its runs are both
// what drawTabStrip paints and what the hit test reads.
type strip struct {
	runs     []run // the painted pieces, with the hit target of each
	first    int   // the first tab on screen, after the scroll is clamped
	active   int   // the index of the active tab, or -1
	overflow bool  // the tabs are wider than the room left of the "+"
	before   bool  // tabs are scrolled off the left edge
	after    bool  // tabs are scrolled off the right edge
}

// tabCells is the width of one tab in cells: the label " name " followed by the
// "×" and one blank. Tabs follow each other with no gap.
func tabCells(t TabInfo) int {
	return ansi.StringWidth(space.Pad(stripLabel(t))) + 2
}

// tabStarts returns the first cell of every tab, measured from the strip's left
// edge, and the width of all the tabs.
func tabStarts(tabs []TabInfo) (starts []int, total int) {
	starts = make([]int, len(tabs))
	for i, t := range tabs {
		starts[i] = total
		total += tabCells(t)
	}
	return starts, total
}

// placeStrip places the tab strip from column x0 in a row w cells wide. Each tab
// is the label " name × " in three runs: the name with its leading blank (hitTab),
// the "×" (hitTabClose), and the trailing blank (hitTab). Tabs follow each other
// with no gap.
//
// The "+" (hitTabNew) is pinned at the last column of the row, outside the
// scrolled window, so it is always on screen and clickable.
//
// When the tabs are wider than the room left of the "+", the strip scrolls. The
// window keeps the cells between two marker columns: "<" (hitTabScrollBefore) at
// x0 and ">" (hitTabScrollAfter) just left of the "+". The scroll position is first,
// a tab index: the window starts at that tab. placeStrip clamps it so the window
// never ends past the last tab, and returns the clamped value in strip.first.
// The painter and the hit test both read the runs, so they cannot drift apart.
func placeStrip(x0, w int, tabs []TabInfo, first int, ic icons.Mode) strip {
	s := placeTabs(x0, w, tabs, first, ic)
	if plusX := w - 1; plusX >= x0 {
		s.runs = append(s.runs, run{x: plusX, w: 1, text: ic.Glyph(icons.New), kind: hitTabNew, index: 0})
	}
	return s
}

// placeTabs places the tabs and their markers, without the "+". It is placeStrip
// for everything but the pinned plus.
func placeTabs(x0, w int, tabs []TabInfo, first int, ic icons.Mode) strip {
	s := strip{active: -1}
	for i, t := range tabs {
		if t.Active {
			s.active = i
		}
	}
	plusX := w - 1
	if plusX < x0 {
		return s
	}

	avail := plusX - x0 // the cells left of the "+"
	starts, total := tabStarts(tabs)
	if total <= avail {
		s.runs = tabRuns(tabs, x0, ic)
		return s
	}

	s.overflow = true
	window := avail - 2 // the cells between the two markers
	if window <= 0 {
		return s
	}
	s.first = clampFirst(starts, total, window, first)
	off := starts[s.first]
	s.before = off > 0
	s.after = off+window < total

	before, after := " ", " "
	if s.before {
		before = ic.Glyph(icons.ScrollLeft)
	}
	if s.after {
		after = ic.Glyph(icons.ScrollRight)
	}
	s.runs = append(s.runs,
		run{x: x0, w: 1, text: before, kind: hitTabScrollBefore},
		run{x: x0 + avail - 1, w: 1, text: after, kind: hitTabScrollAfter},
	)
	for _, r := range tabRuns(tabs, 0, ic) {
		if c, ok := clipRunTo(r, off, off+window); ok {
			c.x = x0 + 1 + (c.x - off)
			s.runs = append(s.runs, c)
		}
	}
	return s
}

// tabRuns lays the tabs out from column x0 with no window: each tab is the name
// with its blanks, the close button, and the trailing blank.
func tabRuns(tabs []TabInfo, x0 int, ic icons.Mode) []run {
	var runs []run
	p := x0
	for i, t := range tabs {
		name := space.Pad(stripLabel(t))
		nw := ansi.StringWidth(name)
		runs = append(runs,
			run{x: p, w: nw, text: name, kind: hitTab, index: i},
			run{x: p + nw, w: 1, text: ic.Glyph(icons.Close), kind: hitTabClose, index: i},
			run{x: p + nw + 1, w: 1, text: " ", kind: hitTab, index: i},
		)
		p += nw + 2
	}
	return runs
}

// clampFirst clamps a scroll position to the tabs. The largest position is the
// first tab from which the rest fill the window, so the window never shows blank
// cells past the last tab. A last tab wider than the window is shown from its
// start.
func clampFirst(starts []int, total, window, first int) int {
	if len(starts) == 0 {
		return 0
	}
	hi := len(starts) - 1
	for f := range starts {
		if total-starts[f] <= window {
			hi = f
			break
		}
	}
	return min(max(first, 0), hi)
}

// revealedFirst returns the scroll position that keeps tab i on screen in the row
// placeStrip gets. The scroll moves only when tab i is not fully visible in the
// window at the current position first. When the tabs fit without scrolling, it
// returns 0.
func revealedFirst(tabs []TabInfo, x0, w, first, i int) int {
	avail := w - 1 - x0
	starts, total := tabStarts(tabs)
	if i < 0 || i >= len(tabs) || total <= avail {
		return 0
	}
	window := avail - 2
	first = clampFirst(starts, total, window, first)
	if i < first {
		return i
	}
	end := total
	if i+1 < len(starts) {
		end = starts[i+1]
	}
	if end-starts[first] <= window {
		return first
	}
	// Move the window forward until tab i ends inside it. A tab wider than the
	// window ends up at the window's start, so its name is not lost.
	for f := first + 1; f < i; f++ {
		if end-starts[f] <= window {
			return f
		}
	}
	return i
}

// clipRunTo keeps the cells of r that fall in columns [lo, hi), and cuts the text
// to match. ok is false when no cell is left.
func clipRunTo(r run, lo, hi int) (run, bool) {
	from := max(lo-r.x, 0)
	to := min(hi-r.x, r.w)
	if to <= from {
		return run{}, false
	}
	return run{
		x:     r.x + from,
		w:     to - from,
		text:  ansi.Cut(r.text, from, to),
		kind:  r.kind,
		index: r.index,
	}, true
}

// stripHit returns the target of the strip under column x of its row, or hitNone
// when no run covers the column.
func stripHit(runs []run, x int) hitTarget {
	return hitRuns(runs, x, hitTarget{Kind: hitNone})
}

// drawTabStrip paints the strip s on row y of g, from column x0. The strip fills
// the row from x0. The label and the close glyph of the active tab take the
// active style, the other tabs and the plus take the tab style, and the markers
// take the bar style.
func drawTabStrip(g *grid.Grid, st style.Styles, x0, y int, s strip) {
	barStyle := style.Cell(st.TabBar)
	g.Fill(x0, g.Width(), y, barStyle)
	tab, active := style.Cell(st.Tab), style.Cell(st.TabActive)
	for _, r := range s.runs {
		cell := tab
		switch {
		case r.kind == hitTabScrollBefore || r.kind == hitTabScrollAfter:
			cell = barStyle
		case r.kind != hitTabNew && r.index == s.active:
			cell = active
		}
		g.Put(r.x, y, g.Width(), r.text, cell)
	}
}
