package document

import "testing"

func TestEnclosingBracketsUsesRuneColumnsAndNests(t *testing.T) {
	tests := []struct {
		name           string
		line           string
		col            int
		inFrom, inTo   int
		outFrom, outTo int
	}{
		{"after a multibyte rune", "é(a)", 2, 2, 3, 1, 4},
		{"the innermost pair", "f(a(b)c)", 4, 4, 5, 3, 6},
		{"skips a closed inner pair", "f(a(b)c)", 6, 2, 7, 1, 8},
		{"a quote pairs with the nearest", `x = "hi" + "yo"`, 6, 5, 7, 4, 8},
	}
	for _, tt := range tests {
		inFrom, inTo, outFrom, outTo, ok := enclosingBrackets(tt.line, tt.col)
		if !ok || inFrom != tt.inFrom || inTo != tt.inTo || outFrom != tt.outFrom || outTo != tt.outTo {
			t.Errorf("%s: enclosingBrackets(%q, %d) = %d %d %d %d %v, want %d %d %d %d",
				tt.name, tt.line, tt.col, inFrom, inTo, outFrom, outTo, ok, tt.inFrom, tt.inTo, tt.outFrom, tt.outTo)
		}
	}
	if _, _, _, _, ok := enclosingBrackets("no pairs", 3); ok {
		t.Error("a line without pairs reported one")
	}
}
