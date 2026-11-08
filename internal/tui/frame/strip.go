package frame

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/tbxark/gvim/internal/tui/icons"
	"github.com/tbxark/gvim/internal/tui/space"
)

// TabInfo describes one tab of the tab strip. Path is the file the tab holds, or ""
// for an untitled tab. The root uses it to watch the folders of the open tabs and to
// reload the tabs whose files change.
type TabInfo struct {
	Name     string
	Path     string
	Modified bool
	Preview  bool
	Active   bool
	// Icon is the file type icon the strip draws before the name. The root fills it
	// when the icon set has file icons; an editor leaves it empty.
	Icon string
}

// Tab Strip glyphs and labels (spec section 3). The modified marker is a dot
// before the name. The close and new-tab buttons come from the icon set (package
// icons): one cell each, so the tab widths do not depend on the mode.
const (
	PreviewSuffix = " (preview)" // the label of a preview tab
	ModifiedMark  = "●"          // before the name of a modified tab
	// ComparePairMark is before the name of each of the two tabs of a compare view.
	ComparePairMark = "⇄"
)

// StripLabel is the name of one tab as the Strip shows it: the file icon (when the
// root gave one) before the name, the modified marker before that, and " (preview)"
// after the name for a preview tab. An unnamed tab reads as NoNameLabel.
func StripLabel(t TabInfo) string {
	base := strings.TrimSuffix(t.Name, PreviewSuffix)
	if base == "" {
		base = NoNameLabel
	}
	if t.Icon != "" {
		base = t.Icon + " " + base
	}
	if t.Modified {
		base = ModifiedMark + " " + base
	}
	if t.Preview || strings.HasSuffix(t.Name, PreviewSuffix) {
		base += PreviewSuffix
	}
	return base
}

// Strip is the tab Strip of one row, placed by PlaceStrip. Its runs are both
// what drawTabStrip paints and what the hit test reads.
type Strip struct {
	Runs     []Run // the painted pieces, with the hit target of each
	First    int   // the first tab on screen, after the scroll is clamped
	Active   int   // the index of the active tab, or -1
	Overflow bool  // the tabs are wider than the room left of the "+"
	Before   bool  // tabs are scrolled off the left edge
	After    bool  // tabs are scrolled off the right edge
}

// tabCells is the width of one tab in cells: the label " name " followed by the
// "×" and one blank. Tabs follow each other with no gap.
func tabCells(t TabInfo) int {
	return ansi.StringWidth(space.Pad(StripLabel(t))) + 2
}

// TabStarts returns the first cell of every tab, measured from the Strip's left
// edge, and the width of all the tabs.
func TabStarts(tabs []TabInfo) (starts []int, total int) {
	starts = make([]int, len(tabs))
	for i, t := range tabs {
		starts[i] = total
		total += tabCells(t)
	}
	return starts, total
}

// PlaceStrip places the tab Strip from column x0 in a row w cells wide. Each tab
// is the label " name × " in three runs: the name with its leading blank (hitTab),
// the "×" (hitTabClose), and the trailing blank (hitTab). Tabs follow each other
// with no gap.
//
// The "+" (hitTabNew) is pinned at the last column of the row, outside the
// scrolled window, so it is always on screen and clickable.
//
// When the tabs are wider than the room left of the "+", the Strip scrolls. The
// window keeps the cells between two marker columns: "<" (hitTabScrollBefore) at
// x0 and ">" (hitTabScrollAfter) just left of the "+". The scroll position is first,
// a tab index: the window starts at that tab. PlaceStrip clamps it so the window
// never ends past the last tab, and returns the clamped value in Strip.first.
// The painter and the hit test both read the runs, so they cannot drift apart.
func PlaceStrip(x0, w int, tabs []TabInfo, first int, ic icons.Mode) Strip {
	s := placeTabs(x0, w, tabs, first, ic)
	if plusX := w - 1; plusX >= x0 {
		s.Runs = append(s.Runs, Run{X: plusX, W: 1, Text: ic.Glyph(icons.New), Kind: HitTabNew, Index: 0})
	}
	return s
}

// placeTabs places the tabs and their markers, without the "+". It is PlaceStrip
// for everything but the pinned plus.
func placeTabs(x0, w int, tabs []TabInfo, first int, ic icons.Mode) Strip {
	s := Strip{Active: -1}
	for i, t := range tabs {
		if t.Active {
			s.Active = i
		}
	}
	plusX := w - 1
	if plusX < x0 {
		return s
	}

	avail := plusX - x0 // the cells left of the "+"
	starts, total := TabStarts(tabs)
	if total <= avail {
		s.Runs = tabRuns(tabs, x0, ic)
		return s
	}

	s.Overflow = true
	window := avail - 2 // the cells between the two markers
	if window <= 0 {
		return s
	}
	s.First = ClampFirst(starts, total, window, first)
	off := starts[s.First]
	s.Before = off > 0
	s.After = off+window < total

	before, after := " ", " "
	if s.Before {
		before = ic.Glyph(icons.ScrollLeft)
	}
	if s.After {
		after = ic.Glyph(icons.ScrollRight)
	}
	s.Runs = append(s.Runs,
		Run{X: x0, W: 1, Text: before, Kind: HitTabScrollBefore},
		Run{X: x0 + avail - 1, W: 1, Text: after, Kind: HitTabScrollAfter},
	)
	for _, r := range tabRuns(tabs, 0, ic) {
		if c, ok := clipRunTo(r, off, off+window); ok {
			c.X = x0 + 1 + (c.X - off)
			s.Runs = append(s.Runs, c)
		}
	}
	return s
}

// tabRuns lays the tabs out from column x0 with no window: each tab is the name
// with its blanks, the close button, and the trailing blank.
func tabRuns(tabs []TabInfo, x0 int, ic icons.Mode) []Run {
	var runs []Run
	p := x0
	for i, t := range tabs {
		name := space.Pad(StripLabel(t))
		nw := ansi.StringWidth(name)
		runs = append(runs,
			Run{X: p, W: nw, Text: name, Kind: HitTab, Index: i},
			Run{X: p + nw, W: 1, Text: ic.Glyph(icons.Close), Kind: HitTabClose, Index: i},
			Run{X: p + nw + 1, W: 1, Text: " ", Kind: HitTab, Index: i},
		)
		p += nw + 2
	}
	return runs
}

// ClampFirst clamps a scroll position to the tabs. The largest position is the
// first tab from which the rest fill the window, so the window never shows blank
// cells past the last tab. A last tab wider than the window is shown from its
// start.
func ClampFirst(starts []int, total, window, first int) int {
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

// RevealedFirst returns the scroll position that keeps tab i on screen in the row
// PlaceStrip gets. The scroll moves only when tab i is not fully visible in the
// window at the current position first. When the tabs fit without scrolling, it
// returns 0.
func RevealedFirst(tabs []TabInfo, x0, w, first, i int) int {
	avail := w - 1 - x0
	starts, total := TabStarts(tabs)
	if i < 0 || i >= len(tabs) || total <= avail {
		return 0
	}
	window := avail - 2
	first = ClampFirst(starts, total, window, first)
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
func clipRunTo(r Run, lo, hi int) (Run, bool) {
	from := max(lo-r.X, 0)
	to := min(hi-r.X, r.W)
	if to <= from {
		return Run{}, false
	}
	return Run{
		X:     r.X + from,
		W:     to - from,
		Text:  ansi.Cut(r.Text, from, to),
		Kind:  r.Kind,
		Index: r.Index,
	}, true
}

// StripHit returns the target of the Strip under column x of its row, or hitNone
// when no Run covers the column.
func StripHit(runs []Run, x int) HitTarget {
	return hitRuns(runs, x, HitTarget{Kind: HitNone})
}
