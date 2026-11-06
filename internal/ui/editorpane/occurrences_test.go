package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

func TestOccurrencesPaintEveryWholeWordMatchOnTheScreen(t *testing.T) {
	d := newDoc("foo bar foo", "xfoo foo_ foo")
	d.cursor = buffer.Pos{Line: 0, Col: 1}
	p := Pane{Doc: d, Occurrences: true, Styles: Styles{Occurrence: grid.Style{BG: cGray}}}
	g := p.Render(20, 2)
	tests := []struct {
		name string
		x, y int
		on   bool
	}{
		{"first word under the caret", 6, 0, true},
		{"second word, line one", 14, 0, true},
		{"word glued to x", 7, 1, false},
		{"word followed by underscore", 11, 1, false},
		{"whole word at the end", 16, 1, true},
		{"space between words", 9, 0, false},
	}
	for _, tc := range tests {
		if got := bgOf(cellAt(t, g, tc.x, tc.y)) == cGray; got != tc.on {
			t.Errorf("%s: highlighted = %v, want %v", tc.name, got, tc.on)
		}
	}
}

func TestOccurrenceUsesTheWordBeforeACaretJustPastIt(t *testing.T) {
	d := newDoc("foo foo")
	d.cursor = buffer.Pos{Line: 0, Col: 3}
	p := Pane{Doc: d, Occurrences: true, Styles: Styles{Occurrence: grid.Style{BG: cGray}}}
	if c := cellAt(t, p.Render(20, 1), 12, 0); bgOf(c) != cGray {
		t.Errorf("second word bg = %v, want the occurrence", bgOf(c))
	}
}

func TestOccurrencesOffWhileTextIsSelected(t *testing.T) {
	d := newDoc("foo foo")
	d.cursor = buffer.Pos{Col: 1}
	d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Col: 3}
	p := Pane{Doc: d, Occurrences: true, Styles: Styles{Occurrence: grid.Style{BG: cGray}, Selection: grid.Style{BG: cBlue}}}
	g := p.Render(20, 1)
	if c := cellAt(t, g, 12, 0); bgOf(c) == cGray {
		t.Error("occurrence painted while text is selected")
	}
}

func TestOccurrenceNeedsAWordAtTheCaret(t *testing.T) {
	d := newDoc("foo  foo")
	d.cursor = buffer.Pos{Col: 4}
	p := Pane{Doc: d, Occurrences: true, Styles: Styles{Occurrence: grid.Style{BG: cGray}}}
	g := p.Render(20, 1)
	for x := 6; x < 15; x++ {
		if bgOf(cellAt(t, g, x, 0)) == cGray {
			t.Errorf("column %d highlighted with no word at the caret", x)
		}
	}
}

func TestSearchMatchWinsOverAnOccurrence(t *testing.T) {
	d := newDoc("foo foo")
	d.cursor = buffer.Pos{Col: 1}
	p := Pane{
		Doc:         d,
		Occurrences: true,
		Styles:      Styles{Occurrence: grid.Style{BG: cGray}, Match: grid.Style{BG: cBlue}},
		Highlights: func(line int) []Highlight {
			return []Highlight{{Start: 0, End: 3}}
		},
	}
	if c := cellAt(t, p.Render(20, 1), 6, 0); bgOf(c) != cBlue {
		t.Errorf("search match bg = %v, want blue over the occurrence", bgOf(c))
	}
}

func TestWordAtTakesTheWordTheCaretIsInOrJustAfter(t *testing.T) {
	tests := []struct {
		name string
		text string
		col  int
		want string
	}{
		{"inside", "foo bar", 1, "foo"},
		{"just after", "foo bar", 3, "foo"},
		{"start of the line", "foo", 0, "foo"},
		{"end of the line", "foo", 3, "foo"},
		{"on a space between words", "foo bar", 3, "foo"},
		{"blank with no word beside it", "a  b", 2, ""},
		{"empty line", "", 0, ""},
		{"underscore is a word character", "a_b c", 2, "a_b"},
		{"digits are word characters", "x42", 2, "x42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wordAt([]rune(tc.text), tc.col); got != tc.want {
				t.Errorf("wordAt(%q, %d) = %q, want %q", tc.text, tc.col, got, tc.want)
			}
		})
	}
}
