package searchpanel

import (
	"slices"
	"testing"
)

func TestPreviewWrapSegmentsBreaksByCells(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		text      string
		width     int
		maxRows   int
		want      [][2]int
		truncated bool
	}{
		{"ascii", "abcdefg", 3, 10, [][2]int{{0, 3}, {3, 6}, {6, 7}}, false},
		{"wide runes take two cells", "你好世界", 4, 10, [][2]int{{0, 2}, {2, 4}}, false},
		{"a wide rune that does not fit moves down", "a你b", 2, 10, [][2]int{{0, 1}, {1, 2}, {2, 3}}, false},
		{"a combining mark stays with its base", "ééé", 2, 10, [][2]int{{0, 4}, {4, 6}}, false},
		{"the row cap truncates", "abcdef", 2, 2, [][2]int{{0, 2}, {2, 4}}, true},
		{"a row takes one rune at least", "你", 1, 10, [][2]int{{0, 1}}, false},
		{"empty", "", 5, 3, nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, truncated := previewWrapSegments([]rune(tt.text), tt.width, tt.maxRows)
			if !slices.Equal(got, tt.want) || truncated != tt.truncated {
				t.Fatalf("segments = %v (truncated %v), want %v (%v)", got, truncated, tt.want, tt.truncated)
			}
		})
	}
}
