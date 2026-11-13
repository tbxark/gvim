package editorpane

import (
	"slices"
	"strings"
	"testing"
)

func TestWrapSpansFollowsTheLegacyBreakRule(t *testing.T) {
	cases := []struct {
		name string
		text string
		w    int
		want []span
	}{
		{name: "break after the last space that fits", text: "hello world", w: 6, want: []span{{0, 6}, {6, 11}}},
		{name: "no space in the width cuts at the width", text: "second line", w: 6, want: []span{{0, 6}, {6, 11}}},
		{name: "wider row keeps the space on the upper row", text: "second line", w: 7, want: []span{{0, 7}, {7, 11}}},
		{name: "a word wider than the row is cut", text: "abcdefgh", w: 3, want: []span{{0, 3}, {3, 6}, {6, 8}}},
		{name: "a wide rune takes a row of its own", text: "中文", w: 2, want: []span{{0, 1}, {1, 2}}},
		{name: "an empty line has one row", text: "", w: 6, want: []span{{0, 0}}},
		{name: "a short line is one row", text: "abc", w: 6, want: []span{{0, 3}}},
		{name: "width under two does not wrap", text: "hello world", w: 1, want: []span{{0, 11}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapSpans([]rune(tc.text), tc.w)
			if !slices.Equal(got, tc.want) {
				t.Errorf("wrapSpans(%q, %d) = %v, want %v", tc.text, tc.w, got, tc.want)
			}
		})
	}
}

func TestCaretInSpansPicksTheRowOfAnIndex(t *testing.T) {
	runes := []rune("hello world")
	spans := wrapSpans(runes, 6)
	cases := []struct {
		idx, row, cells int
	}{
		{idx: 0, row: 0, cells: 0},
		{idx: 5, row: 0, cells: 5},
		{idx: 6, row: 1, cells: 0},
		{idx: 11, row: 1, cells: 5},
	}
	for _, tc := range cases {
		row, cells := caretInSpans(runes, spans, tc.idx)
		if row != tc.row || cells != tc.cells {
			t.Errorf("caretInSpans(%d) = (%d, %d), want (%d, %d)", tc.idx, row, cells, tc.row, tc.cells)
		}
	}
}

func TestExpandCellsTabsAndControlCharacters(t *testing.T) {
	got := expandCells([]rune("a\tb\r!"), nil, nil, defaultTabSize)
	var text []rune
	for _, c := range got {
		text = append(text, c.r)
	}
	if string(text) != "a   b<0D>!" {
		t.Errorf("expanded = %q, want %q", string(text), "a   b<0D>!")
	}
}

func TestExpandedIndexAndRuneIndexAgreeWithTabs(t *testing.T) {
	raw := []rune("a\tb")
	// The rune b is at expanded index 4, after a and three tab blanks.
	if got := expandedIndex(raw, 2, defaultTabSize); got != 4 {
		t.Errorf("expandedIndex(b) = %d, want 4", got)
	}
	// Display column 2 is a blank of the tab, which is rune 1.
	if got := runeIndexAt(raw, 2, defaultTabSize); got != 1 {
		t.Errorf("runeIndexAt(column 2) = %d, want 1", got)
	}
}

func TestWrappedRowsStaySingleRowsWhenTheWidthIsTooSmall(t *testing.T) {
	// An 8-wide pane has a 6-cell gutter, so the wrap width is below two cells.
	// The line does not wrap and is cut at the edge (the legacy rule).
	p := Pane{Doc: newDoc("hello world")}
	g := p.Render(8, 1)
	if got := rowsOf(g)[0]; got != "  1 │ h" {
		t.Errorf("row = %q, want the first character only", got)
	}
}

func TestWrapRowsKeepsAGluedRunOnOneRow(t *testing.T) {
	// glue[i] joins rune i to rune i+1, as the four runes of a control escape are.
	tests := []struct {
		name string
		text string
		glue []int // indexes joined to the next rune
		w    int
		want []span
	}{
		{"an escape that does not fit moves down whole", "abcd<07>ef", []int{4, 5, 6}, 6, []span{{0, 4}, {4, 10}}},
		{"an escape that fits stays", "ab<07>", []int{2, 3, 4}, 6, []span{{0, 6}}},
		{"an escape ending at the edge stays", "ab<07>cd", []int{2, 3, 4}, 6, []span{{0, 6}, {6, 8}}},
		{"a blank before the escape still breaks", "ab cd<07>", []int{5, 6, 7}, 6, []span{{0, 3}, {3, 9}}},
		{"an escape wider than the row is cut", "ab<07>", []int{2, 3, 4}, 3, []span{{0, 2}, {2, 5}, {5, 6}}},
		{"no glue is the plain rule", "abcd<07>ef", nil, 6, []span{{0, 6}, {6, 10}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			glue := make([]bool, len([]rune(tc.text)))
			for _, i := range tc.glue {
				glue[i] = true
			}
			got, _ := wrapHanging([]rune(tc.text), tc.w, false, glue)
			if !slices.Equal(got, tc.want) {
				t.Errorf("wrapHanging(%q, %d) = %v, want %v", tc.text, tc.w, got, tc.want)
			}
		})
	}
}

// A token that does not fit a fresh row breaks before itself only when the row already
// holds enough: Fresh's row floor, the larger of w-16 and w/2. A short marker before a
// long unspaced run is not stranded on a row of its own.
func TestWrapRowsSplitsALongTokenOnTheSameRowBelowTheFloor(t *testing.T) {
	long := strings.Repeat("a", 60)
	tests := []struct {
		name   string
		text   string
		w      int
		indent int
		want   []span
	}{
		{"a list marker stays with the run", "- " + long, 30, 0, []span{{0, 30}, {30, 60}, {60, 62}}},
		{"a word before the run stays with it", "word " + long, 30, 0, []span{{0, 30}, {30, 60}, {60, 65}}},
		{"a row past the floor breaks before the run", "0123456789abcdef " + long, 30, 0, []span{{0, 17}, {17, 47}, {47, 77}}},
		{"a token that fits a fresh row breaks before itself", "- " + strings.Repeat("b", 28), 30, 0, []span{{0, 30}}},
		{"a token that fits the next row moves down", "- " + strings.Repeat("b", 29), 30, 0, []span{{0, 2}, {2, 31}}},
		{"cjk run after a marker", "- " + strings.Repeat("中", 40), 30, 0, []span{{0, 16}, {16, 31}, {31, 42}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapRows([]rune(tc.text), tc.w, tc.indent, nil)
			if !slices.Equal(got, tc.want) {
				t.Errorf("wrapRows(%q, %d) = %v, want %v", tc.text, tc.w, got, tc.want)
			}
		})
	}
}

// A grapheme cluster is measured whole and never split across rows: a skin-tone emoji,
// a flag and a family are two cells each, as the grid and the caret count them.
func TestWrapRowsMeasuresClustersWhole(t *testing.T) {
	tests := []struct {
		name    string
		cluster string
		per     int // runes in one cluster
	}{
		{"skin tone", "👍🏽", 2},
		{"flag", "🇨🇳", 2},
		{"family", "👨‍👩‍👧‍👦", 7},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// "x" and 20 clusters at 30 cells: 14 clusters fit the first row after the x.
			got := wrapSpans([]rune("x"+strings.Repeat(tc.cluster, 20)), 30)
			want := []span{{0, 1 + 14*tc.per}, {1 + 14*tc.per, 1 + 20*tc.per}}
			if !slices.Equal(got, want) {
				t.Errorf("rows = %v, want %v", got, want)
			}
		})
	}
}

// wrapSpans splits the expanded runes of a buffer line into rows of at most w
// cells using the break rule above. An empty line still has one row. A width
// under two cells does not wrap, so the line stays one row.
func wrapSpans(runes []rune, w int) []span {
	return wrapRows(runes, w, 0, nil)
}
