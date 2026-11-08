package tui

import (
	"slices"
	"testing"
)

// leafNode returns a leaf that shows tab i.
func leafNode(i int) *splitNode { return &splitNode{tab: i} }

// innerNode divides its area in direction dir between a and b.
func innerNode(dir splitDir, a, b *splitNode) *splitNode {
	return &splitNode{dir: dir, a: a, b: b}
}

// treeLayout lays out t in a w by h frame. The leaves take the reading order of
// t.leaves, and lines gives each leaf's line count in that order. The leaf at reading
// index focus has the focus.
func treeLayout(w, h int, explorer bool, t *splitNode, focus int, lines []int) layout {
	leaves := t.leaves()
	infos := make([]leafInfo, len(leaves))
	for i, n := range leaves {
		infos[i] = leafInfo{tab: n.tab, top: n.top, lines: lines[i], focused: i == focus}
	}
	return computeLayout(w, h, layoutInput{explorerOpen: explorer, split: t, leaves: infos})
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
		first, second := splitSpan(tt.total, tt.ratio)
		if first != tt.first || second != tt.second {
			t.Errorf("splitSpan(%d, %d) = %d, %d, want %d, %d", tt.total, tt.ratio, first, second, tt.first, tt.second)
		}
	}
}

// shapes returns the lines without the split and the area they belong to, so a test
// compares where the lines are and nothing else.
func shapes(bs []splitBorder) []splitBorder {
	out := make([]splitBorder, len(bs))
	for i, b := range bs {
		out[i] = splitBorder{rect: b.rect, vertical: b.vertical}
	}
	return out
}

func TestSplitLayoutPanesAtOneTwoAndThreeLeaves(t *testing.T) {
	// The band at W=80, H=24 with no explorer is x 0..79 and rows 2..22: 80 by 21.
	tests := []struct {
		name     string
		explorer bool
		tree     *splitNode
		focus    int
		panes    []rect
		borders  []splitBorder
	}{
		{
			name:  "one leaf fills the band",
			tree:  leafNode(0),
			panes: []rect{{X: 0, Y: 2, W: 80, H: 21}},
		},
		{
			name:    "two leaves side by side",
			tree:    innerNode(sideBySide, leafNode(0), leafNode(1)),
			panes:   []rect{{X: 0, Y: 2, W: 40, H: 21}, {X: 41, Y: 2, W: 39, H: 21}},
			borders: []splitBorder{{rect: rect{X: 40, Y: 2, W: 1, H: 21}, vertical: true}},
		},
		{
			name:    "two leaves stacked",
			tree:    innerNode(stacked, leafNode(0), leafNode(1)),
			panes:   []rect{{X: 0, Y: 2, W: 80, H: 10}, {X: 0, Y: 13, W: 80, H: 10}},
			borders: []splitBorder{{rect: rect{X: 0, Y: 12, W: 80, H: 1}, vertical: false}},
		},
		{
			name:    "three leaves, right column stacked",
			tree:    innerNode(sideBySide, leafNode(0), innerNode(stacked, leafNode(1), leafNode(2))),
			panes:   []rect{{X: 0, Y: 2, W: 40, H: 21}, {X: 41, Y: 2, W: 39, H: 10}, {X: 41, Y: 13, W: 39, H: 10}},
			borders: []splitBorder{{rect: rect{X: 40, Y: 2, W: 1, H: 21}, vertical: true}, {rect: rect{X: 41, Y: 12, W: 39, H: 1}, vertical: false}},
		},
		{
			name:    "three leaves, bottom row side by side",
			tree:    innerNode(stacked, leafNode(0), innerNode(sideBySide, leafNode(1), leafNode(2))),
			panes:   []rect{{X: 0, Y: 2, W: 80, H: 10}, {X: 0, Y: 13, W: 40, H: 10}, {X: 41, Y: 13, W: 39, H: 10}},
			borders: []splitBorder{{rect: rect{X: 0, Y: 12, W: 80, H: 1}, vertical: false}, {rect: rect{X: 40, Y: 13, W: 1, H: 10}, vertical: true}},
		},
		{
			name:    "three leaves, left column stacked",
			tree:    innerNode(sideBySide, innerNode(stacked, leafNode(0), leafNode(1)), leafNode(2)),
			panes:   []rect{{X: 0, Y: 2, W: 40, H: 10}, {X: 0, Y: 13, W: 40, H: 10}, {X: 41, Y: 2, W: 39, H: 21}},
			borders: []splitBorder{{rect: rect{X: 40, Y: 2, W: 1, H: 21}, vertical: true}, {rect: rect{X: 0, Y: 12, W: 40, H: 1}, vertical: false}},
		},
		{
			name:     "two leaves side by side next to the explorer",
			explorer: true,
			tree:     innerNode(sideBySide, leafNode(0), leafNode(1)),
			panes:    []rect{{X: 32, Y: 2, W: 24, H: 21}, {X: 57, Y: 2, W: 23, H: 21}},
			borders:  []splitBorder{{rect: rect{X: 56, Y: 2, W: 1, H: 21}, vertical: true}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaves := len(tt.tree.leaves())
			l := treeLayout(80, 24, tt.explorer, tt.tree, tt.focus, make([]int, leaves))
			got := make([]rect, len(l.panes))
			for i, p := range l.panes {
				got[i] = p.rect
			}
			if !slices.Equal(got, tt.panes) {
				t.Errorf("panes = %v, want %v", got, tt.panes)
			}
			if !slices.Equal(shapes(l.borders), tt.borders) {
				t.Errorf("borders = %v, want %v", shapes(l.borders), tt.borders)
			}
		})
	}
}

func TestSinglePaneIsTheCurrentBand(t *testing.T) {
	// With no split tree the band is one pane, and the top-level editor fields are
	// that pane's, so the single-leaf layout is the layout the tests above pin.
	for _, g := range sampleWidths {
		for _, h := range sampleHeights {
			l := layoutFor(g.w, h, layoutInput{explorerOpen: true, lines: 40})
			if len(l.panes) != 1 {
				t.Fatalf("W=%d H=%d: %d panes, want 1", g.w, h, len(l.panes))
			}
			p := l.panes[0]
			want := rect{X: l.editorX, Y: l.bodyTop, W: g.w - l.editorX, H: l.bodyBottom - l.bodyTop + 1}
			if p.rect != want || !p.focused || l.focus != 0 {
				t.Errorf("W=%d H=%d: pane %+v focus %d, want rect %+v focused", g.w, h, p, l.focus, want)
			}
			if p.gutter != l.gutter || p.textX != l.textX || p.textW != l.textW || p.scrollbar != l.scrollbar {
				t.Errorf("W=%d H=%d: pane fields %+v differ from the editor fields", g.w, h, p)
			}
		}
	}
}

func TestFocusedPaneSetsTheEditorFields(t *testing.T) {
	// The first pane is short and the second is long. Focus on the second makes
	// the top-level scrollbar and gutter those of the second pane.
	tree := innerNode(sideBySide, leafNode(0), leafNode(1))
	l := treeLayout(80, 24, false, tree, 1, []int{3, 100})
	if len(l.panes) != 2 {
		t.Fatalf("%d panes, want 2", len(l.panes))
	}
	if l.focus != 1 {
		t.Fatalf("focus = %d, want 1", l.focus)
	}
	if l.panes[0].scrollbar || !l.panes[1].scrollbar {
		t.Errorf("scrollbars = %v, %v; want false, true", l.panes[0].scrollbar, l.panes[1].scrollbar)
	}
	if !l.scrollbar || l.gutter != gutterWidth(100) || l.textX != l.panes[1].textX || l.textW != l.panes[1].textW {
		t.Errorf("editor fields %+v do not match the focused pane %+v", l, l.panes[1])
	}
}

func TestEachPaneSizesItsGutterFromItsOwnDocument(t *testing.T) {
	tree := innerNode(sideBySide, leafNode(0), leafNode(1))
	l := treeLayout(80, 24, false, tree, 0, []int{2, 1000})
	if len(l.panes) != 2 {
		t.Fatalf("%d panes, want 2", len(l.panes))
	}
	if g := l.panes[0].gutter; g != 6 {
		t.Errorf("two-line pane gutter = %d, want 6", g)
	}
	// 1000 lines is four digits: the gutter is 8 and the text starts after it.
	if p := l.panes[1]; p.gutter != 8 || p.textX != 49 || p.textW != 31 {
		t.Errorf("long pane gutter %d textX %d textW %d, want 8, 49, 31", p.gutter, p.textX, p.textW)
	}
}

func TestHitSplitBordersAndPanes(t *testing.T) {
	tests := []struct {
		name  string
		tree  *splitNode
		lines []int
		x, y  int
		want  hitTarget
	}{
		{"left pane text", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 20, 5,
			hitTarget{Kind: hitEditorText, Index: 3, Pane: 0}},
		{"left pane gutter", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 3, 2,
			hitTarget{Kind: hitGutter, Index: 0, Pane: 0}},
		{"vertical border", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 40, 5,
			hitTarget{Kind: hitSplitBorder, Index: 0}},
		{"vertical border top row", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 40, 2,
			hitTarget{Kind: hitSplitBorder, Index: 0}},
		{"vertical border bottom row", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 40, 22,
			hitTarget{Kind: hitSplitBorder, Index: 0}},
		{"right pane gutter", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 41, 2,
			hitTarget{Kind: hitGutter, Index: 0, Pane: 1}},
		{"right pane text", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{2, 2}, 50, 5,
			hitTarget{Kind: hitEditorText, Index: 3, Pane: 1}},
		{"upper pane row", innerNode(stacked, leafNode(0), leafNode(1)), []int{2, 2}, 10, 11,
			hitTarget{Kind: hitEditorText, Index: 9, Pane: 0}},
		{"horizontal border", innerNode(stacked, leafNode(0), leafNode(1)), []int{2, 2}, 10, 12,
			hitTarget{Kind: hitSplitBorder, Index: 0}},
		{"lower pane first row", innerNode(stacked, leafNode(0), leafNode(1)), []int{2, 2}, 10, 13,
			hitTarget{Kind: hitEditorText, Index: 0, Pane: 1}},
		{"scrollbar of a long pane", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{100, 2}, 39, 5,
			hitTarget{Kind: hitScrollbarTrack, Index: 3, Pane: 0}},
		{"last column of a short pane is text", innerNode(sideBySide, leafNode(0), leafNode(1)), []int{100, 2}, 79, 5,
			hitTarget{Kind: hitEditorText, Index: 3, Pane: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := treeLayout(80, 24, false, tt.tree, 0, tt.lines)
			if got := l.hit(tt.x, tt.y); got != tt.want {
				t.Errorf("hit(%d, %d) = %+v, want %+v", tt.x, tt.y, got, tt.want)
			}
		})
	}
}
