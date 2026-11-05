package editorpane

import (
	"slices"
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

func TestExpandCellsTabsAndCarriageReturns(t *testing.T) {
	got := expandCells([]rune("a\tb\r!"), nil, nil)
	var text []rune
	for _, c := range got {
		text = append(text, c.r)
	}
	if string(text) != "a   b!" {
		t.Errorf("expanded = %q, want %q", string(text), "a   b!")
	}
}

func TestExpandedIndexAndRuneIndexAgreeWithTabs(t *testing.T) {
	raw := []rune("a\tb")
	// The rune b is at expanded index 4, after a and three tab blanks.
	if got := expandedIndex(raw, 2); got != 4 {
		t.Errorf("expandedIndex(b) = %d, want 4", got)
	}
	// Display column 2 is a blank of the tab, which is rune 1.
	if got := runeIndexAt(raw, 2); got != 1 {
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
