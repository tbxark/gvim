package lsp

import (
	"unicode/utf16"

	"go.lsp.dev/uri"
)

// PathURI returns the file URI for an absolute path. Pass the same URI to the
// Client request methods that take a document.
func PathURI(path string) string {
	return string(uri.File(path))
}

// RuneColToUTF16 converts a rune column in line to the UTF-16 code unit offset
// that LSP uses for Position.Character. A column past the end of line clamps to
// the line's length in code units, and a negative column is 0.
func RuneColToUTF16(line string, runeCol int) uint32 {
	var units uint32
	n := 0
	for _, r := range line {
		if n >= runeCol {
			break
		}
		units += uint32(utf16.RuneLen(r))
		n++
	}
	return units
}

// UTF16ToRuneCol converts a UTF-16 code unit offset in line to a rune column.
// An offset inside a surrogate pair maps to the rune that the pair encodes, and
// an offset past the end of line clamps to the rune count.
func UTF16ToRuneCol(line string, utf16Col uint32) int {
	var units uint32
	n := 0
	for _, r := range line {
		width := uint32(utf16.RuneLen(r))
		if utf16Col < units+width {
			return n
		}
		units += width
		n++
	}
	return n
}
