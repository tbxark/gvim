package textlayout

import (
	"strings"
	"unicode/utf8"
)

// Matches returns the rune columns of the matches of text in line, in order. A
// match is literal and may sit inside a longer word, and matches do not overlap.
// An empty text matches nothing.
func Matches(line, text string) []int {
	if text == "" {
		return nil
	}
	var cols []int
	from := 0
	prevByte := 0
	runeCol := 0
	for {
		i := strings.Index(line[from:], text)
		if i < 0 {
			return cols
		}
		start := from + i
		runeCol += utf8.RuneCountInString(line[prevByte:start])
		prevByte = start
		cols = append(cols, runeCol)
		from = start + len(text)
	}
}
