package document

import (
	"github.com/tbxark/gvim/buffer"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"strings"
	"unicode/utf8"
)

// upperCase and lowerCase convert with the full Unicode case mapping, as Rust's
// to_uppercase and to_lowercase do in Fresh: "ß" becomes "SS", a ligature such as
// "ﬁ" becomes "FI", and a capital sigma at the end of a word lowers to "ς". A
// Caser keeps state, so each call makes its own.
func upperCase(s string) string { return cases.Upper(language.Und).String(s) }
func lowerCase(s string) string { return cases.Lower(language.Und).String(s) }

// keepSelection returns the place functions that leave a caret whose selection ran
// over [from, to) selecting the converted text that replaced it, in the direction it
// had. The end of the converted text is given by the edit; its start is found by
// walking back over the text, because a conversion may change the length.
func keepSelection(from buffer.Pos, converted string, backwards bool) (place, anchor placeFn) {
	start := func(end buffer.Pos) buffer.Pos {
		if n := strings.Count(converted, "\n"); n > 0 {
			return buffer.Pos{Line: end.Line - n, Col: from.Col}
		}
		return buffer.Pos{Line: end.Line, Col: end.Col - utf8.RuneCountInString(converted)}
	}
	atEnd := func(end buffer.Pos) buffer.Pos { return end }
	if backwards {
		return start, atEnd
	}
	return atEnd, start
}
