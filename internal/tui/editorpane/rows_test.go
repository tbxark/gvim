package editorpane

import (
	"slices"
	"testing"
)

func TestRowStartsMatchTheWrappedRows(t *testing.T) {
	tests := []struct {
		name string
		text string
		wrap bool
		want []int
	}{
		// W=14 has 8 text cells and a wrap width of 6.
		{"breaks after the last blank that fits", "hello world", true, []int{0, 6}},
		{"a line that fits is one row", "hello", true, nil},
		{"no blank cuts at the width", "second line", true, []int{0, 6}},
		{"wrap off has no rows", "hello world", false, nil},
		{"a tab counts its cells but the columns stay runes", "a\tbcdefgh", true, []int{0, 2, 8}},
		{"wide characters take two cells", "你好世界你好", true, []int{0, 3}},
		{"a control character takes four cells", "a\x07bcdefgh ijkl", true, []int{0, 3, 9}},
		{"a carriage return takes four cells", "a\rbcdefgh ijkl", true, []int{0, 3, 9}},
		{"an escape moves to the next row whole", "abcd\x07efgh", true, []int{0, 4, 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Pane{Doc: newDoc(tt.text), Wrap: tt.wrap}
			if got := p.RowStarts(14, 6, 0); !slices.Equal(got, tt.want) {
				t.Fatalf("RowStarts = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRowStartsOutsideTheDocument(t *testing.T) {
	p := Pane{Doc: newDoc("hello world"), Wrap: true}
	for _, line := range []int{-1, 1, 9} {
		if got := p.RowStarts(14, 6, line); got != nil {
			t.Errorf("RowStarts(line %d) = %v, want nil", line, got)
		}
	}
}
