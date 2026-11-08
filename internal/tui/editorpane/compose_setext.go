package editorpane

import (
	"strings"

	"github.com/tbxark/gvim/syntax"
)

// A setext heading is a paragraph line followed by a line of "=" (level 1) or
// "-" (level 2). The tokenizer paints both lines as a heading; the page view
// keeps the text styled and conceals the underline, which is the marker. The
// underline is only an underline where a paragraph precedes it: a rule after a
// blank line stays a thematic break.

// isSetextUnderline reports whether raw is a setext underline line: at most three
// blanks, then a run of "=" or "-", then blanks.
func isSetextUnderline(raw []rune) bool {
	s := strings.TrimRight(string(raw), " \t\r")
	if s == "" {
		return false
	}
	lead := len(s) - len(strings.TrimLeft(s, " "))
	if lead > 3 {
		return false
	}
	body := s[lead:]
	if body == "" {
		return false
	}
	c := body[0]
	if c != '=' && c != '-' {
		return false
	}
	for i := 0; i < len(body); i++ {
		if body[i] != c {
			return false
		}
	}
	return true
}

// setextHeadingAbove reports whether line i's setext underline makes a heading:
// the tokenizer's heading spans on the line are authoritative, and without them
// the previous line is checked for a paragraph.
func (f frame) setextHeadingAbove(i int, spans []syntax.Span, raw []rune) bool {
	covered := 0
	if len(spans) > 0 {
		all := true
		for _, sp := range spans {
			if sp.Kind != syntax.KindHeading || sp.End < sp.Start {
				all = false
				break
			}
			covered += sp.End - sp.Start
		}
		if all && covered >= len(strings.TrimRight(string(raw), "\r")) {
			return true
		}
	}
	if i == 0 {
		return false
	}
	prev := f.doc.Line(i - 1)
	trimmed := strings.TrimSpace(prev)
	if trimmed == "" {
		return false
	}
	// A previous line that is itself a block marker cannot be the heading text.
	switch {
	case strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, ">"),
		strings.HasPrefix(trimmed, "```"), strings.HasPrefix(trimmed, "~~~"),
		isRuleRunes([]rune(prev)), isSetextUnderline([]rune(prev)):
		return false
	}
	return true
}
