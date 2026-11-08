package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/grid"
)

// guideRow returns the text cells of row y from column 6, for the guide tests.
func guideRow(t *testing.T, g *grid.Grid, y, n int) string { return textFrom(t, g, y, 6, n) }

func TestGuidesFollowTheIndentOfTheFile(t *testing.T) {
	// A file indented by two, with a tab size of four.
	lines := []string{"if a {", "  b()", "  if c {", "    d()", "  }", "}"}
	tests := []struct {
		row  int
		want string
	}{
		{0, "if a {"},
		{1, "│ b()"},
		{2, "│ if c {"},
		{3, "│ │ d()"},
		{4, "│ }"},
		{5, "}"},
	}
	g := Pane{Doc: newDoc(lines...), IndentGuides: true}.Render(20, 6)
	for _, tc := range tests {
		if got := guideRow(t, g, tc.row, len([]rune(tc.want))); got != tc.want {
			t.Errorf("row %d = %q, want %q", tc.row, got, tc.want)
		}
	}
}

func TestGuidesRunThroughBlankLines(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		row   int
		want  string
	}{
		{"an empty line between lines of a block", []string{"a {", "    x", "", "    y"}, 2, "│"},
		{"an empty line before a nested body", []string{"a {", "    b {", "", "        c"}, 2, "│   │"},
		{"a line of blanks shorter than the guides", []string{"a {", "    b {", "  ", "        c"}, 2, "│   │"},
		{"an empty line before a shallower line", []string{"a {", "    x", "", "y"}, 2, "│"},
		{"no guide at the top level", []string{"x", "", "y"}, 1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: newDoc(tc.lines...), IndentGuides: true}.Render(20, len(tc.lines))
			if got := trimRight(guideRow(t, g, tc.row, 12)); got != tc.want {
				t.Errorf("row %d = %q, want %q", tc.row, got, tc.want)
			}
		})
	}
}

func TestGuidesStartFromTheBlocksOpenAboveTheWindow(t *testing.T) {
	d := newDoc("a {", "    b {", "        c", "        d")
	g := Pane{Doc: d, Top: 3, IndentGuides: true}.Render(20, 1)
	if got := guideRow(t, g, 0, 9); got != "│   │   d" {
		t.Errorf("row = %q, want the guides of both enclosing blocks", got)
	}
}

func TestActiveGuideIsTheInnermostBlockOfTheCaret(t *testing.T) {
	lines := []string{"func f() {", "    if a {", "        x", "        y", "    }", "}"}
	d := foldDoc{fakeDoc: newDoc(lines...), ranges: fold.Ranges(lines, 4)}
	tests := []struct {
		name  string
		caret int
		rows  map[int]string
	}{
		{"caret in the inner block", 2, map[int]string{1: "    if a {", 2: "    │   x", 3: "    │   y", 4: "    }"}},
		{"caret on the inner header", 1, map[int]string{1: "    if a {", 2: "    │   x", 3: "    │   y"}},
		{"caret in the outer block only", 4, map[int]string{1: "│   if a {", 2: "│       x", 5: "}"}},
		{"caret outside any block", 5, map[int]string{1: "    if a {", 2: "        x"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d.cursor = buffer.Pos{Line: tc.caret}
			g := Pane{Doc: d, IndentGuides: true, Opts: Options{GuideActive: true}}.Render(20, 6)
			for row, want := range tc.rows {
				if got := trimRight(guideRow(t, g, row, 12)); got != want {
					t.Errorf("row %d = %q, want %q", row, got, want)
				}
			}
		})
	}
}

func TestGuideGlyphAndColors(t *testing.T) {
	blue, red := grid.Style{FG: cBlue}, grid.Style{FG: cRed}
	styles := Styles{
		Whitespace: grid.Style{FG: cGray},
		Decor: DecorStyles{
			IndentGuide:   grid.Style{FG: cGreen},
			IndentRainbow: [6]grid.Style{red, blue},
		},
	}
	d := newDoc("a {", "    b {", "        c")
	tests := []struct {
		name  string
		opts  Options
		glyph string
		fg0   grid.Style // the guide at column 0
		fg4   grid.Style // the guide at column 4
	}{
		{"defaults", Options{}, "│", grid.Style{FG: cGreen}, grid.Style{FG: cGreen}},
		{"glyph", Options{GuideGlyph: '┊'}, "┊", grid.Style{FG: cGreen}, grid.Style{FG: cGreen}},
		{"a wide glyph falls back", Options{GuideGlyph: '中'}, "▏", grid.Style{FG: cGreen}, grid.Style{FG: cGreen}},
		{"rainbow", Options{RainbowGuides: true}, "│", red, blue},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: d, IndentGuides: true, Styles: styles, Opts: tc.opts}.Render(20, 3)
			if c := cellAt(t, g, 6, 2); c.Text != tc.glyph || c.Style.FG != tc.fg0.FG {
				t.Errorf("column 0 guide = %q fg %v, want %q fg %v", c.Text, c.Style.FG, tc.glyph, tc.fg0.FG)
			}
			if c := cellAt(t, g, 10, 2); c.Text != tc.glyph || c.Style.FG != tc.fg4.FG {
				t.Errorf("column 4 guide = %q fg %v, want %q fg %v", c.Text, c.Style.FG, tc.glyph, tc.fg4.FG)
			}
		})
	}
}

func TestGuideFallsBackToTheWhitespaceColor(t *testing.T) {
	g := Pane{Doc: newDoc("a {", "    b"), IndentGuides: true, Styles: Styles{Whitespace: grid.Style{FG: cGray}}}.Render(20, 2)
	if c := cellAt(t, g, 6, 1); c.Text != "│" || c.Style.FG != cGray {
		t.Errorf("guide = %q fg %v, want a gray guide", c.Text, c.Style.FG)
	}
}

func TestGuidesRunThroughTheHangingIndentOfWrappedRows(t *testing.T) {
	d := newDoc("a {", "    b {", "        one two three four five six seven")
	// The text area is 32 wide, so the wrap width is 31 and the hanging indent 8.
	g := Pane{Doc: d, Wrap: true, IndentGuides: true, Opts: Options{WrapIndent: true}}.Render(39, 6)
	if got := trimRight(guideRow(t, g, 2, 32)); got != "│   │   one two three four" {
		t.Errorf("first row = %q", got)
	}
	if got := trimRight(guideRow(t, g, 3, 32)); got != "│   │   five six seven" {
		t.Errorf("continuation row = %q, want the guides inside the hanging indent", got)
	}
}

func trimRight(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		s = s[:len(s)-1]
	}
	return s
}
