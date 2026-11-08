package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestVisibleWhitespaceShowsDotsAndArrows(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string // the text cells from column 6
	}{
		{"space", "a b", "a·b"},
		{"tab at column 0 takes four cells", "\tx", "→   x"},
		{"tab at column 3 takes one cell", "a b\tc", "a·b→c"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Doc: newDoc(tc.line), VisibleWhitespace: true, Styles: Styles{Whitespace: grid.Style{FG: cGray}}}
			g := p.Render(20, 1)
			var got []rune
			for i := range len([]rune(tc.want)) {
				got = append(got, []rune(cellAt(t, g, 6+i, 0).Text)...)
			}
			if string(got) != tc.want {
				t.Errorf("cells = %q, want %q", string(got), tc.want)
			}
		})
	}
}

func TestVisibleWhitespaceMarksPaintInTheWhitespaceColor(t *testing.T) {
	p := Pane{Doc: newDoc("a b"), VisibleWhitespace: true, Styles: Styles{Whitespace: grid.Style{FG: cGray}}}
	g := p.Render(20, 1)
	if c := cellAt(t, g, 7, 0); c.Text != "·" || c.Style.FG != cGray {
		t.Errorf("space cell = %q fg %v, want a gray middle dot", c.Text, c.Style.FG)
	}
	if c := cellAt(t, g, 6, 0); c.Style.FG == cGray {
		t.Error("letter takes the whitespace color")
	}
}

func TestVisibleWhitespaceOffKeepsBlanks(t *testing.T) {
	g := Pane{Doc: newDoc("a b")}.Render(20, 1)
	if c := cellAt(t, g, 7, 0); c.Text != " " {
		t.Errorf("blank = %q, want a space", c.Text)
	}
}

func TestIndentGuidesMarkEachLevelInTheLeadingBlanks(t *testing.T) {
	p := Pane{
		Doc:          newDoc("    a", "        b", "c", "  d"),
		IndentGuides: true,
		Styles:       Styles{Whitespace: grid.Style{FG: cGray}},
	}
	g := p.Render(20, 4)
	tests := []struct {
		name string
		x, y int
		want string
	}{
		{"one level, first column", 6, 0, "│"},
		{"one level, the blank before the text is not a guide", 8, 0, " "},
		{"two levels, first column", 6, 1, "│"},
		{"two levels, second level", 10, 1, "│"},
		{"no indent, no guide", 6, 2, "c"},
		{"two blanks under a line at column 0 get its guide", 6, 3, "│"},
		{"the second blank is not a guide", 7, 3, " "},
	}
	for _, tc := range tests {
		if got := cellAt(t, g, tc.x, tc.y).Text; got != tc.want {
			t.Errorf("%s: cell = %q, want %q", tc.name, got, tc.want)
		}
	}
	if c := cellAt(t, g, 10, 1); c.Style.FG != cGray {
		t.Errorf("guide fg = %v, want the whitespace color", c.Style.FG)
	}
}

func TestIndentGuidesOffByDefault(t *testing.T) {
	g := Pane{Doc: newDoc("    a")}.Render(20, 1)
	if c := cellAt(t, g, 6, 0); c.Text != " " {
		t.Errorf("cell = %q, want a blank", c.Text)
	}
}

func TestIndentGuideKeepsTheTabStopAndMovesTheTabArrowRight(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string // the text cells from column 6
	}{
		{"one tab", "\tx", "│→  x"},
		{"two tabs", "\t\tx", "│→  │→  x"},
		{"a tab of one cell keeps its arrow", "   \t\tx", "│··→│→  x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{
				Doc:               newDoc(tc.line),
				IndentGuides:      true,
				VisibleWhitespace: true,
				Styles:            Styles{Whitespace: grid.Style{FG: cGray}},
			}
			g := p.Render(20, 1)
			var got []rune
			for i := range len([]rune(tc.want)) {
				got = append(got, []rune(cellAt(t, g, 6+i, 0).Text)...)
			}
			if string(got) != tc.want {
				t.Errorf("cells = %q, want %q", string(got), tc.want)
			}
		})
	}
}
