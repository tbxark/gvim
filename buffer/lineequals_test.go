package buffer

import "testing"

func TestLineEquals(t *testing.T) {
	t.Parallel()
	b := New("héllo\n日本\n\nabc")
	tests := []struct {
		line int
		text string
		want bool
	}{
		{0, "héllo", true},
		{0, "hello", false},
		{0, "héll", false},
		{0, "héllo!", false},
		{1, "日本", true},
		{1, "日", false},
		{1, "日本語", false},
		{2, "", true},
		{2, "x", false},
		{3, "abc", true},
		{3, "abd", false},
		{99, "abc", true}, // clamped, like Line
	}
	for _, tt := range tests {
		if got := b.LineEquals(tt.line, tt.text); got != tt.want {
			t.Errorf("LineEquals(%d, %q) = %v, want %v", tt.line, tt.text, got, tt.want)
		}
	}
}
