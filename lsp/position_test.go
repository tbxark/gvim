package lsp

import (
	"testing"
	"unicode/utf8"
)

func TestRuneColToUTF16(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		runeCol int
		want    uint32
	}{
		{name: "empty line", line: "", runeCol: 0, want: 0},
		{name: "ascii", line: "hello", runeCol: 3, want: 3},
		{name: "two byte rune is one unit", line: "héllo", runeCol: 2, want: 2},
		{name: "three byte rune is one unit", line: "中文x", runeCol: 2, want: 2},
		{name: "column before surrogate pair", line: "a😀b", runeCol: 1, want: 1},
		{name: "column after surrogate pair", line: "a😀b", runeCol: 2, want: 3},
		{name: "column after last rune", line: "a😀b", runeCol: 3, want: 4},
		{name: "column past end clamps", line: "a😀b", runeCol: 99, want: 4},
		{name: "negative column is zero", line: "a😀b", runeCol: -1, want: 0},
		{name: "consecutive surrogate pairs", line: "😀😀", runeCol: 1, want: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RuneColToUTF16(tt.line, tt.runeCol); got != tt.want {
				t.Fatalf("RuneColToUTF16(%q, %d) = %d, want %d", tt.line, tt.runeCol, got, tt.want)
			}
		})
	}
}

func TestUTF16ToRuneCol(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		utf16Col uint32
		want     int
	}{
		{name: "empty line", line: "", utf16Col: 0, want: 0},
		{name: "ascii", line: "hello", utf16Col: 3, want: 3},
		{name: "two byte rune is one unit", line: "héllo", utf16Col: 2, want: 2},
		{name: "start of surrogate pair", line: "a😀b", utf16Col: 1, want: 1},
		{name: "inside surrogate pair snaps to its rune", line: "a😀b", utf16Col: 2, want: 1},
		{name: "after surrogate pair", line: "a😀b", utf16Col: 3, want: 2},
		{name: "end of line", line: "a😀b", utf16Col: 4, want: 3},
		{name: "past end clamps", line: "a😀b", utf16Col: 40, want: 3},
		{name: "inside second surrogate pair", line: "😀😀", utf16Col: 3, want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UTF16ToRuneCol(tt.line, tt.utf16Col); got != tt.want {
				t.Fatalf("UTF16ToRuneCol(%q, %d) = %d, want %d", tt.line, tt.utf16Col, got, tt.want)
			}
		})
	}
}

func TestPositionConversionRoundTrip(t *testing.T) {
	lines := []string{"", "hello", "héllo", "a😀b", "😀😀x", "中文😀"}
	for _, line := range lines {
		for col := 0; col <= utf8.RuneCountInString(line); col++ {
			units := RuneColToUTF16(line, col)
			if got := UTF16ToRuneCol(line, units); got != col {
				t.Errorf("line %q: rune %d -> utf16 %d -> rune %d", line, col, units, got)
			}
		}
	}
}
