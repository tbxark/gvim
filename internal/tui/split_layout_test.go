package tui

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/tui/frame"
	"github.com/tbxark/gvim/internal/tui/space"
	"github.com/tbxark/gvim/internal/tui/split"
)

// leafNode returns a leaf that shows tab i.
func leafNode(i int) *split.Node { return &split.Node{Tab: i} }

// innerNode divides its area in direction dir between a and b.
func innerNode(dir split.Dir, a, b *split.Node) *split.Node {
	return &split.Node{Dir: dir, A: a, B: b}
}

// treeLayout lays out t in a w by h frame. The leaves take the reading order of
// t.leaves, and lines gives each leaf's line count in that order. The leaf at reading
// index focus has the focus.
func treeLayout(w, h int, explorer bool, t *split.Node, focus int, lines []int) frame.Layout {
	leaves := t.Leaves()
	infos := make([]split.LeafInfo, len(leaves))
	for i, n := range leaves {
		infos[i] = split.LeafInfo{Tab: n.Tab, Top: n.Top, Lines: lines[i], Focused: i == focus}
	}
	return frame.ComputeLayout(w, h, frame.Input{ExplorerOpen: explorer, Split: t, Leaves: infos})
}

func TestSplitSpanSharesTheLineBetweenChildren(t *testing.T) {
	tests := []struct{ total, ratio, first, second int }{
		{0, 50, 0, 0},
		{1, 50, 0, 0},
		{2, 50, 1, 0},
		{3, 50, 1, 1},
		{4, 50, 2, 1},
		{21, 50, 10, 10},
		{80, 50, 40, 39},
		// The ratio is the first child's share of the cells left, rounded to the nearest.
		{80, 25, 20, 59},
		{80, 75, 59, 20},
		{21, 10, 2, 18},
		{21, 90, 18, 2},
		// Each child keeps one cell when the area has room for one.
		{3, 10, 1, 1},
		{3, 90, 1, 1},
		{5, 10, 1, 3},
	}
	for _, tt := range tests {
		first, second := split.Span(tt.total, tt.ratio)
		if first != tt.first || second != tt.second {
			t.Errorf("split.Span(%d, %d) = %d, %d, want %d, %d", tt.total, tt.ratio, first, second, tt.first, tt.second)
		}
	}
}

// shapes returns the lines without the split and the area they belong to, so a test
// compares where the lines are and nothing else.
func shapes(bs []split.Border) []split.Border {
	out := make([]split.Border, len(bs))
	for i, b := range bs {
		out[i] = split.Border{Rect: b.Rect, Vertical: b.Vertical}
	}
	return out
}

func TestSplitLayoutPanesAtOneTwoAndThreeLeaves(t *testing.T) {
	// The editor band at W=80, H=24 is x 1..78 and rows 3..21,
	// inside the classic frame and above the status row.
	tests := []struct {
		name     string
		explorer bool
		tree     *split.Node
		focus    int
		panes    []space.Rect
		borders  []split.Border
	}{
		{
			name:  "one leaf fills the band",
			tree:  leafNode(0),
			panes: []space.Rect{{X: 1, Y: 3, W: 78, H: 19}},
		},
		{
			name:    "two leaves side by side",
			tree:    innerNode(split.SideBySide, leafNode(0), leafNode(1)),
			panes:   []space.Rect{{X: 1, Y: 3, W: 39, H: 19}, {X: 41, Y: 3, W: 38, H: 19}},
			borders: []split.Border{{Rect: space.Rect{X: 40, Y: 3, W: 1, H: 19}, Vertical: true}},
		},
		{
			name:    "two leaves stacked",
			tree:    innerNode(split.Stacked, leafNode(0), leafNode(1)),
			panes:   []space.Rect{{X: 1, Y: 3, W: 78, H: 9}, {X: 1, Y: 13, W: 78, H: 9}},
			borders: []split.Border{{Rect: space.Rect{X: 1, Y: 12, W: 78, H: 1}, Vertical: false}},
		},
		{
			name:    "three leaves, right column stacked",
			tree:    innerNode(split.SideBySide, leafNode(0), innerNode(split.Stacked, leafNode(1), leafNode(2))),
			panes:   []space.Rect{{X: 1, Y: 3, W: 39, H: 19}, {X: 41, Y: 3, W: 38, H: 9}, {X: 41, Y: 13, W: 38, H: 9}},
			borders: []split.Border{{Rect: space.Rect{X: 40, Y: 3, W: 1, H: 19}, Vertical: true}, {Rect: space.Rect{X: 41, Y: 12, W: 38, H: 1}, Vertical: false}},
		},
		{
			name:    "three leaves, bottom row side by side",
			tree:    innerNode(split.Stacked, leafNode(0), innerNode(split.SideBySide, leafNode(1), leafNode(2))),
			panes:   []space.Rect{{X: 1, Y: 3, W: 78, H: 9}, {X: 1, Y: 13, W: 39, H: 9}, {X: 41, Y: 13, W: 38, H: 9}},
			borders: []split.Border{{Rect: space.Rect{X: 1, Y: 12, W: 78, H: 1}, Vertical: false}, {Rect: space.Rect{X: 40, Y: 13, W: 1, H: 9}, Vertical: true}},
		},
		{
			name:    "three leaves, left column stacked",
			tree:    innerNode(split.SideBySide, innerNode(split.Stacked, leafNode(0), leafNode(1)), leafNode(2)),
			panes:   []space.Rect{{X: 1, Y: 3, W: 39, H: 9}, {X: 1, Y: 13, W: 39, H: 9}, {X: 41, Y: 3, W: 38, H: 19}},
			borders: []split.Border{{Rect: space.Rect{X: 40, Y: 3, W: 1, H: 19}, Vertical: true}, {Rect: space.Rect{X: 1, Y: 12, W: 39, H: 1}, Vertical: false}},
		},
		{
			name:     "two leaves side by side next to the explorer",
			explorer: true,
			tree:     innerNode(split.SideBySide, leafNode(0), leafNode(1)),
			panes:    []space.Rect{{X: 32, Y: 3, W: 23, H: 19}, {X: 56, Y: 3, W: 23, H: 19}},
			borders:  []split.Border{{Rect: space.Rect{X: 55, Y: 3, W: 1, H: 19}, Vertical: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := len(tt.tree.Leaves())
			l := treeLayout(80, 24, tt.explorer, tt.tree, tt.focus, make([]int, leaves))
			got := make([]space.Rect, len(l.Panes))
			for i, p := range l.Panes {
				got[i] = p.Rect
			}
			if !slices.Equal(got, tt.panes) {
				t.Errorf("panes = %v, want %v", got, tt.panes)
			}
			if !slices.Equal(shapes(l.Borders), tt.borders) {
				t.Errorf("borders = %v, want %v", shapes(l.Borders), tt.borders)
			}
		})
	}
}

func TestSinglePaneIsTheCurrentBand(t *testing.T) {
	// With no split tree the band is one pane, and the top-level editor fields are
	// that pane's, so the single-leaf layout is the layout the tests above pin.
	for _, g := range sampleWidths {
		for _, h := range sampleHeights {
			l := layoutFor(g.w, h, frame.Input{ExplorerOpen: true, Lines: 40})
			if len(l.Panes) != 1 {
				t.Fatalf("W=%d H=%d: %d panes, want 1", g.w, h, len(l.Panes))
			}
			p := l.Panes[0]
			want := space.Rect{X: l.EditorX, Y: l.BodyTop, W: l.EditorW, H: l.BodyBottom - l.BodyTop + 1}
			if p.Rect != want || !p.Focused || l.Focus != 0 {
				t.Errorf("W=%d H=%d: pane %+v focus %d, want rect %+v focused", g.w, h, p, l.Focus, want)
			}
			if p.Gutter != l.Gutter || p.TextX != l.TextX || p.TextW != l.TextW || p.Scrollbar != l.Scrollbar {
				t.Errorf("W=%d H=%d: pane fields %+v differ from the editor fields", g.w, h, p)
			}
		}
	}
}

func TestFocusedPaneSetsTheEditorFields(t *testing.T) {
	// The first pane is short and the second is long. Focus on the second makes
	// the top-level scrollbar and gutter those of the second pane.
	tree := innerNode(split.SideBySide, leafNode(0), leafNode(1))
	l := treeLayout(80, 24, false, tree, 1, []int{3, 100})
	if len(l.Panes) != 2 {
		t.Fatalf("%d panes, want 2", len(l.Panes))
	}
	if l.Focus != 1 {
		t.Fatalf("focus = %d, want 1", l.Focus)
	}
	if l.Panes[0].Scrollbar || !l.Panes[1].Scrollbar {
		t.Errorf("scrollbars = %v, %v; want false, true", l.Panes[0].Scrollbar, l.Panes[1].Scrollbar)
	}
	if !l.Scrollbar || l.Gutter != frame.GutterWidth(100) || l.TextX != l.Panes[1].TextX || l.TextW != l.Panes[1].TextW {
		t.Errorf("editor fields %+v do not match the focused pane %+v", l, l.Panes[1])
	}
}

func TestEachPaneSizesItsGutterFromItsOwnDocument(t *testing.T) {
	tree := innerNode(split.SideBySide, leafNode(0), leafNode(1))
	l := treeLayout(80, 24, false, tree, 0, []int{2, 1000})
	if len(l.Panes) != 2 {
		t.Fatalf("%d panes, want 2", len(l.Panes))
	}
	if g := l.Panes[0].Gutter; g != 6 {
		t.Errorf("two-line pane gutter = %d, want 6", g)
	}
	// 1000 lines is four digits: the gutter is 8 and the text starts after it.
	if p := l.Panes[1]; p.Gutter != 8 || p.TextX != 49 || p.TextW != 30 {
		t.Errorf("long pane gutter %d textX %d textW %d, want 8, 49, 30", p.Gutter, p.TextX, p.TextW)
	}
}

func TestHitSplitBordersAndPanes(t *testing.T) {
	tests := []struct {
		name  string
		tree  *split.Node
		lines []int
		x, y  int
		want  frame.HitTarget
	}{
		{"left pane text", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 20, 5,
			frame.HitTarget{Kind: frame.HitEditorText, Index: 2, Pane: 0}},
		{"left pane gutter", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 3, 3,
			frame.HitTarget{Kind: frame.HitGutter, Index: 0, Pane: 0}},
		{"vertical border", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 40, 5,
			frame.HitTarget{Kind: frame.HitSplitBorder, Index: 0}},
		{"vertical border top row", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 40, 3,
			frame.HitTarget{Kind: frame.HitSplitBorder, Index: 0}},
		{"vertical border bottom row", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 40, 21,
			frame.HitTarget{Kind: frame.HitSplitBorder, Index: 0}},
		{"right pane gutter", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 41, 3,
			frame.HitTarget{Kind: frame.HitGutter, Index: 0, Pane: 1}},
		{"right pane text", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 50, 5,
			frame.HitTarget{Kind: frame.HitEditorText, Index: 2, Pane: 1}},
		{"upper pane row", innerNode(split.Stacked, leafNode(0), leafNode(1)), []int{2, 2}, 10, 10,
			frame.HitTarget{Kind: frame.HitEditorText, Index: 7, Pane: 0}},
		{"horizontal border", innerNode(split.Stacked, leafNode(0), leafNode(1)), []int{2, 2}, 10, 12,
			frame.HitTarget{Kind: frame.HitSplitBorder, Index: 0}},
		{"lower pane first row", innerNode(split.Stacked, leafNode(0), leafNode(1)), []int{2, 2}, 10, 13,
			frame.HitTarget{Kind: frame.HitEditorText, Index: 0, Pane: 1}},
		{"scrollbar of a long pane", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{100, 2}, 39, 5,
			frame.HitTarget{Kind: frame.HitScrollbarTrack, Index: 2, Pane: 0}},
		{"last column of a short pane is text", innerNode(split.SideBySide, leafNode(0), leafNode(1)), []int{100, 2}, 78, 5,
			frame.HitTarget{Kind: frame.HitEditorText, Index: 2, Pane: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := treeLayout(80, 24, false, tt.tree, 0, tt.lines)
			if got := l.Hit(tt.x, tt.y); got != tt.want {
				t.Errorf("hit(%d, %d) = %+v, want %+v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}
