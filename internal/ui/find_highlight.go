package ui

import (
	"unicode/utf8"

	"github.com/tbxark/gvim/internal/search"
	"github.com/tbxark/gvim/internal/ui/editorpane"
)

// docLines is the line source of a document for the search highlights: its line
// count and the rune length of each line.
type docLines struct{ d Document }

// Lines is the number of lines of the document.
func (l docLines) Lines() int { return l.d.LineCount() }

// LineLen is the rune length of line i. A document that knows it without building
// the text of the line is asked first: a large document caches the decoded line.
func (l docLines) LineLen(i int) int {
	if ll, ok := l.d.(interface{ LineLen(i int) int }); ok {
		return ll.LineLen(i)
	}
	return utf8.RuneCountInString(l.d.Line(i))
}

// noHighlights is the line function when no search is shown.
func noHighlights(int) []search.Highlight { return nil }

// findLineHighlights returns the search highlights of the active document as a
// function of the line. The highlights are the matches of the find bar's search
// while the bar is open, or closed by Enter with the search kept. The current
// match is the selection, when the selection is one of them. The matches are found
// once, here, so the caller makes the function once per frame and calls it for each
// line it paints. The function gives nothing when no bar search is shown, the
// search text is empty or invalid, or there is no match.
func (m *Model) findLineHighlights() func(line int) []search.Highlight {
	doc := m.ed.Active()
	if !m.findShown() || doc == nil {
		return noHighlights
	}
	q := m.findBar.Query()
	if q.Text == "" {
		return noHighlights
	}
	if ss, ok := m.streamSearch(); ok {
		return m.largeVisibleHighlights(ss, doc, q)
	}
	h := findHost{m: m}
	matches, err := h.Find(q)
	if err != nil || len(matches) == 0 {
		return noHighlights
	}
	var current *search.Match
	if start, end, ok := h.Selection(); ok {
		for i := range matches {
			if matches[i].Start == start && matches[i].End == end {
				current = &matches[i]
				break
			}
		}
	}
	lines := docLines{d: doc}
	return func(line int) []search.Highlight {
		return search.Highlights(lines, matches, line, current)
	}
}

// paneHighlights converts the search highlights of a line to the editor pane's
// runs. The pane's run has no line, because the function it gets is already for
// one line, so each highlight keeps its columns and its current flag. A nil or
// empty result stays nil.
func paneHighlights(lines func(line int) []search.Highlight) func(line int) []editorpane.Highlight {
	return func(line int) []editorpane.Highlight {
		hs := lines(line)
		if len(hs) == 0 {
			return nil
		}
		out := make([]editorpane.Highlight, len(hs))
		for i, h := range hs {
			out[i] = editorpane.Highlight{Start: h.Start, End: h.End, Current: h.Current}
		}
		return out
	}
}
