package space

import (
	"slices"
	"testing"
)

func TestWrapWords(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		in    string
		width int
		want  []string
	}{
		{"no room", "abc", 0, nil},
		{"empty", "", 5, nil},
		{"words fit a row", "a b c", 5, []string{"a b c"}},
		{"breaks at a space", "hello world", 7, []string{"hello", "world"}},
		{"newlines are spaces", "a\nb", 5, []string{"a b"}},
		{"a long word fills the room left first", "ab cdefghij", 6, []string{"ab cde", "fghij"}},
		{"a long word on its own row", "abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"wide runes at width one", "中文", 1, []string{"中", "文"}},
		{"wide runes after a word", "a 中文", 3, []string{"a", "中", "文"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := WrapWords(tt.in, tt.width); !slices.Equal(got, tt.want) {
				t.Fatalf("WrapWords(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
		})
	}
}
