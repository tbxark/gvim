package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// crlfDoc is a fakeDoc whose saved text has CRLF breaks.
type crlfDoc struct{ *fakeDoc }

func (crlfDoc) LineEnding() string { return "CRLF" }

// selectedAt reports whether the cell at (x, y) has the selection background.
func selectedAt(t *testing.T, g *grid.Grid, x, y int) bool {
	t.Helper()
	return bgOf(cellAt(t, g, x, y)) == cBlue
}

func TestSelectedLineBreaksHighlightTheColumnTheyOccupy(t *testing.T) {
	tests := []struct {
		name     string
		sel      [2]buffer.Pos
		selected [][2]int // text columns (from 6) of rows 0..2 that carry the selection
		clear    [][2]int
	}{
		{
			name:     "a selection over an empty line",
			sel:      [2]buffer.Pos{{Line: 0, Col: 0}, {Line: 2, Col: 1}},
			selected: [][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {2, 0}},
			clear:    [][2]int{{2, 1}, {0, 3}},
		},
		{
			name:     "a selection that ends at the end of a line leaves its break out",
			sel:      [2]buffer.Pos{{Line: 0, Col: 0}, {Line: 0, Col: 2}},
			selected: [][2]int{{0, 0}, {0, 1}},
			clear:    [][2]int{{0, 2}, {1, 0}},
		},
		{
			name:     "a selection that ends at the start of the next line takes the break",
			sel:      [2]buffer.Pos{{Line: 0, Col: 1}, {Line: 1, Col: 0}},
			selected: [][2]int{{0, 1}, {0, 2}},
			clear:    [][2]int{{1, 0}, {0, 0}},
		},
		{
			name:  "the last line has no break after it",
			sel:   [2]buffer.Pos{{Line: 2, Col: 0}, {Line: 2, Col: 2}},
			clear: [][2]int{{2, 2}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc("ab", "", "cd")
			d.selected, d.anchor, d.head = true, tc.sel[0], tc.sel[1]
			d.cursor = tc.sel[1]
			g := Pane{Doc: d, Styles: Styles{Selection: grid.Style{BG: cBlue}}}.Render(20, 3)
			for _, c := range tc.selected {
				if !selectedAt(t, g, 6+c[1], c[0]) {
					t.Errorf("row %d column %d is not selected", c[0], c[1])
				}
			}
			for _, c := range tc.clear {
				if selectedAt(t, g, 6+c[1], c[0]) {
					t.Errorf("row %d column %d is selected", c[0], c[1])
				}
			}
		})
	}
}

func TestSelectedLineBreakOfAWrappedLineIsOnItsLastRow(t *testing.T) {
	d := newDoc("aaaa bbbb cccc", "x")
	d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Line: 1, Col: 1}
	// A 13-cell text area wraps "aaaa bbbb cccc" after "bbbb " (wrap width 11).
	g := Pane{Doc: d, Wrap: true, Styles: Styles{Selection: grid.Style{BG: cBlue}}}.Render(20, 3)
	if got := strings.TrimRight(rowsOf(g)[1], " "); got != "    │ cccc" {
		t.Fatalf("setup: row 1 = %q", got)
	}
	if selectedAt(t, g, 6+4, 0) && !selectedAt(t, g, 6+5, 0) {
		t.Error("the break is drawn on the first row")
	}
	if !selectedAt(t, g, 6+4, 1) {
		t.Error("the break is not drawn after the text of the last row")
	}
}

func TestLineEndMarks(t *testing.T) {
	tests := []struct {
		name string
		doc  Document
		ws   Whitespace
		want string // the row 0 text cells from column 6
	}{
		{"newline mark", newDoc("ab", "cd"), Whitespace{Newlines: true}, "ab↵"},
		{"carriage return mark needs a CRLF document", newDoc("ab", "cd"), Whitespace{CarriageReturns: true}, "ab"},
		{"carriage return mark in a CRLF document", crlfDoc{newDoc("ab", "cd")}, Whitespace{CarriageReturns: true}, "ab␍"},
		{"both marks in a CRLF document", crlfDoc{newDoc("ab", "cd")}, Whitespace{CarriageReturns: true, Newlines: true}, "ab␍↵"},
		{"newline mark in a CRLF document", crlfDoc{newDoc("ab", "cd")}, Whitespace{Newlines: true}, "ab↵"},
		{"marks off", newDoc("ab", "cd"), Whitespace{}, "ab"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ws := tc.ws
			g := Pane{Doc: tc.doc, Opts: Options{Whitespace: &ws}, Styles: Styles{Whitespace: grid.Style{FG: cGray}}}.Render(20, 2)
			if got := strings.TrimRight(textFrom(t, g, 0, 6, 8), " "); got != tc.want {
				t.Errorf("row 0 = %q, want %q", got, tc.want)
			}
			// The last line has no break after it.
			if got := strings.TrimRight(textFrom(t, g, 1, 6, 8), " "); got != "cd" {
				t.Errorf("row 1 = %q, want %q", got, "cd")
			}
		})
	}
}

func TestLineEndMarkUsesTheWhitespaceColor(t *testing.T) {
	ws := Whitespace{Newlines: true}
	g := Pane{Doc: newDoc("ab", "cd"), Opts: Options{Whitespace: &ws}, Styles: Styles{Whitespace: grid.Style{FG: cGray}}}.Render(20, 2)
	if c := cellAt(t, g, 8, 0); c.Text != "↵" || c.Style.FG != cGray {
		t.Errorf("mark = %q fg %v, want a gray ↵", c.Text, c.Style.FG)
	}
}

// textFrom returns the text of n cells of row y from column x.
func textFrom(t *testing.T, g *grid.Grid, y, x, n int) string {
	t.Helper()
	var b strings.Builder
	for i := range n {
		b.WriteString(cellAt(t, g, x+i, y).Text)
	}
	return b.String()
}
