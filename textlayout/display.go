package textlayout

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Span is a run of runes [Start, End) on the zero-based line Line.
type Span struct {
	Line, Start, End int
}

// IndentWidth returns the display width of the spaces and tabs at the start of
// line, as LineIndent measures it. A tab advances to the next multiple of tabWidth.
// A tabWidth below one is treated as one.
func IndentWidth(line string, tabWidth int) int {
	width, _ := LineIndent(line, tabWidth)
	return width
}

// Occurrences returns the whole-word matches of word in lines. A match must not
// touch a word character on either side. Columns are rune columns. An empty
// word matches nothing.
func Occurrences(lines []string, word string) []Span {
	if word == "" {
		return nil
	}
	var spans []Span
	n := utf8.RuneCountInString(word)
	for i, line := range lines {
		for _, col := range wordColumns(line, word) {
			spans = append(spans, Span{Line: i, Start: col, End: col + n})
		}
	}
	return spans
}

// wordColumns returns the rune columns of the whole-word matches of word in
// line, in order. After a rejected candidate the search resumes one rune later,
// so overlapping candidates are all checked.
func wordColumns(line, word string) []int {
	var cols []int
	from := 0
	for {
		i := strings.Index(line[from:], word)
		if i < 0 {
			return cols
		}
		start := from + i
		end := start + len(word)
		if !wordBefore(line, start) && !wordAt(line, end) {
			cols = append(cols, utf8.RuneCountInString(line[:start]))
			from = end
			continue
		}
		_, size := utf8.DecodeRuneInString(line[start:])
		from = start + size
	}
}

// wordBefore reports whether the rune ending at byte i of line is a word
// character. At the start of line it reports false.
func wordBefore(line string, i int) bool {
	if i <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(line[:i])
	return isWordRune(r)
}

// wordAt reports whether the rune starting at byte i of line is a word
// character. At the end of line it reports false.
func wordAt(line string, i int) bool {
	if i >= len(line) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(line[i:])
	return isWordRune(r)
}

// isWordRune is the word-character rule of the search package: Alphabetic,
// Mark, Nd, Pc and Join_Control.
func isWordRune(r rune) bool {
	return unicode.IsLetter(r) ||
		unicode.Is(unicode.Nl, r) ||
		unicode.Is(unicode.Other_Alphabetic, r) ||
		unicode.IsMark(r) ||
		unicode.IsDigit(r) ||
		unicode.Is(unicode.Pc, r) ||
		r == 0x200C || r == 0x200D
}
