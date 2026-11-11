package document

import "testing"

func TestLeadColumnsStopsAtTabStops(t *testing.T) {
	for _, tt := range []struct {
		line string
		want int
	}{
		{"\tx", 4},
		{" \tx", 4},
		{"   \t x", 5},
		{"    \tx", 8},
	} {
		if got := leadColumns([]rune(tt.line), 100, 4); got != tt.want {
			t.Errorf("leadColumns(%q) = %d, want %d", tt.line, got, tt.want)
		}
	}
}
