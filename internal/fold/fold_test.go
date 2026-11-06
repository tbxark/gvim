package fold

import (
	"slices"
	"testing"
)

func TestRanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		lines    []string
		tabWidth int
		want     []Range
	}{
		{"empty input", nil, 4, nil},
		{"single unindented line", []string{"a"}, 4, nil},
		{"only blank lines", []string{"", "  ", "\t"}, 4, nil},
		{"same level lines", []string{"a", "b", "c"}, 4, nil},
		{"block closed by dedent", []string{"fn a() {", "    x", "    y", "}"}, 4, []Range{{0, 2}}},
		{"block runs to end of file", []string{"a:", "  x", "  y"}, 4, []Range{{0, 2}}},
		{"blank lines inside block are included", []string{"a:", "    x", "", "    y", "", "b"}, 4, []Range{{0, 3}}},
		{"blank line before indented line is inside block", []string{"a:", "", "    x"}, 4, []Range{{0, 2}}},
		{"whitespace-only line counts as blank", []string{"a:", "    x", "   ", "b"}, 4, []Range{{0, 1}}},
		{"blank line never opens a block", []string{"", "    x"}, 4, nil},
		{"nested blocks", []string{"a:", "    b:", "        c", "    d", "e"}, 4, []Range{{0, 3}, {1, 2}}},
		{"dedent to intermediate level keeps outer block", []string{"a:", "        x", "    y", "z"}, 4, []Range{{0, 2}}},
		{"tab counts to next tab stop", []string{"  a:", "\tb", "  c"}, 8, []Range{{0, 1}}},
		{"tab width decides whether a line opens", []string{"  a:", "\tb", "  c"}, 2, nil},
		{"tabs and spaces at the same width", []string{"a:", "\tx", "    y"}, 4, []Range{{0, 2}}},
		{"multi-byte content", []string{"日本:", "  語", "  字"}, 4, []Range{{0, 2}}},
		{"tab width below one is treated as one", []string{"a:", " x"}, 0, []Range{{0, 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Ranges(tc.lines, tc.tabWidth)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Ranges(%q, %d) = %v, want %v", tc.lines, tc.tabWidth, got, tc.want)
			}
		})
	}
}

func TestVisible(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		n      int
		ranges []Range
		folded map[int]bool
		want   []bool
	}{
		{"no lines", 0, []Range{{0, 1}}, map[int]bool{0: true}, []bool{}},
		{"nothing folded", 3, []Range{{0, 2}}, nil, []bool{false, false, false}},
		{"folded flag set to false", 3, []Range{{0, 2}}, map[int]bool{0: false}, []bool{false, false, false}},
		{"fold hides body but not opener", 4, []Range{{0, 2}}, map[int]bool{0: true}, []bool{false, true, true, false}},
		{"inner fold hides only its body", 5, []Range{{0, 3}, {1, 2}}, map[int]bool{1: true}, []bool{false, false, true, false, false}},
		{"outer fold hides nested lines", 5, []Range{{0, 3}, {1, 2}}, map[int]bool{0: true}, []bool{false, true, true, true, false}},
		{"fold key with no range", 3, []Range{{0, 2}}, map[int]bool{1: true}, []bool{false, false, false}},
		{"range past n is clipped", 3, []Range{{0, 5}}, map[int]bool{0: true}, []bool{false, true, true}},
		{"no ranges", 2, nil, map[int]bool{0: true}, []bool{false, false}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Visible(tc.n, tc.ranges, tc.folded)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Visible(%d, %v, %v) = %v, want %v", tc.n, tc.ranges, tc.folded, got, tc.want)
			}
		})
	}
}
