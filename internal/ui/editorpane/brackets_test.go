package editorpane

import (
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/syntax"
	"github.com/tbxark/gvim/internal/ui/grid"
)

func bracketStyles() Styles {
	return Styles{Decor: DecorStyles{
		BracketMatch:   grid.Style{FG: cGreen},
		BracketRainbow: [6]grid.Style{{FG: cRed}, {FG: cBlue}, {FG: cGray}},
		BracketPair:    grid.Style{Underline: true},
	}}
}

// matched reports whether the cell at text column col of row y is drawn as part of the pair.
func matched(t *testing.T, g *grid.Grid, col, y int) bool {
	t.Helper()
	c := cellAt(t, g, 6+col, y)
	return c.Style.FG == cGreen
}

func TestMatchingBracketsAreHighlighted(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		caret buffer.Pos
		on    [][2]int // text column and row of the cells in the pair color
		off   [][2]int
	}{
		{"on an opener", []string{"f(a, b)"}, buffer.Pos{Col: 1}, [][2]int{{1, 0}, {6, 0}}, [][2]int{{0, 0}, {2, 0}}},
		{"on a closer", []string{"f(a, b)"}, buffer.Pos{Col: 6}, [][2]int{{1, 0}, {6, 0}}, nil},
		{"just after a closer", []string{"f(a, b)"}, buffer.Pos{Col: 7}, [][2]int{{1, 0}, {6, 0}}, nil},
		{"across lines", []string{"if x {", "  y", "}"}, buffer.Pos{Col: 5}, [][2]int{{5, 0}, {0, 2}}, [][2]int{{0, 0}}},
		{"nested pairs of one kind", []string{"((a))"}, buffer.Pos{Col: 0}, [][2]int{{0, 0}, {4, 0}}, [][2]int{{1, 0}, {3, 0}}},
		{"other kinds are ignored", []string{"( [ ) ]"}, buffer.Pos{Col: 0}, [][2]int{{0, 0}, {4, 0}}, [][2]int{{2, 0}, {6, 0}}},
		{"no bracket at or before the caret", []string{"f(a)"}, buffer.Pos{Col: 0}, nil, [][2]int{{1, 0}, {3, 0}}},
		{"an unmatched bracket is highlighted alone", []string{"f(a"}, buffer.Pos{Col: 1}, [][2]int{{1, 0}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc(tc.lines...)
			d.cursor = tc.caret
			g := Pane{Doc: d, Styles: bracketStyles(), Opts: Options{HighlightBrackets: true}}.Render(30, len(tc.lines))
			for _, c := range tc.on {
				if !matched(t, g, c[0], c[1]) {
					t.Errorf("column %d of row %d is not in the pair color", c[0], c[1])
				}
			}
			for _, c := range tc.off {
				if matched(t, g, c[0], c[1]) {
					t.Errorf("column %d of row %d is in the pair color", c[0], c[1])
				}
			}
		})
	}
}

func TestBracketsHighlightNeedsTheOption(t *testing.T) {
	d := newDoc("f(a)")
	d.cursor = buffer.Pos{Col: 1}
	g := Pane{Doc: d, Styles: bracketStyles()}.Render(20, 1)
	if matched(t, g, 1, 0) {
		t.Error("bracket highlighted with the option off")
	}
}

func TestBracketsInCommentsAndStringsAreNotStructure(t *testing.T) {
	d := newDoc(`f(")", a) // )`)
	d.spans = map[int][]syntax.Span{0: {
		{Start: 2, End: 5, Kind: syntax.KindString},
		{Start: 10, End: 14, Kind: syntax.KindComment},
	}}
	d.cursor = buffer.Pos{Col: 1}
	g := Pane{Doc: d, Styles: bracketStyles(), Opts: Options{HighlightBrackets: true}}.Render(30, 1)
	if !matched(t, g, 8, 0) {
		t.Error("the closer after the string is not the partner")
	}
	if matched(t, g, 3, 0) {
		t.Error("a bracket in a string is the partner")
	}
	// A bracket in a comment has no pair at all.
	d.cursor = buffer.Pos{Col: 13}
	g = Pane{Doc: d, Styles: bracketStyles(), Opts: Options{HighlightBrackets: true}}.Render(30, 1)
	if matched(t, g, 13, 0) {
		t.Error("a bracket in a comment is highlighted")
	}
}

func TestRainbowBracketsColorByDepth(t *testing.T) {
	d := newDoc("a(b[c{d}c]b)a", "(x)")
	d.cursor = buffer.Pos{Line: 1, Col: 1}
	g := Pane{Doc: d, Styles: bracketStyles(), Opts: Options{HighlightBrackets: true, RainbowBrackets: true}}.Render(30, 2)
	tests := []struct {
		col, row int
		fg       any
	}{
		{1, 0, cRed}, {3, 0, cBlue}, {5, 0, cGray}, {7, 0, cGray}, {9, 0, cBlue}, {11, 0, cRed},
		{0, 1, cRed}, {2, 1, cRed},
	}
	for _, tc := range tests {
		if got := cellAt(t, g, 6+tc.col, tc.row).Style.FG; got != tc.fg {
			t.Errorf("bracket at column %d of row %d fg = %v, want %v", tc.col, tc.row, got, tc.fg)
		}
	}
	// A letter keeps the text color.
	if got := cellAt(t, g, 6, 0).Style.FG; got != nil {
		t.Errorf("letter fg = %v, want none", got)
	}
}

func TestRainbowKeepsTheDepthColorAndMarksThePairWithAttributes(t *testing.T) {
	d := newDoc("(a)")
	d.cursor = buffer.Pos{}
	g := Pane{Doc: d, Styles: bracketStyles(), Opts: Options{HighlightBrackets: true, RainbowBrackets: true}}.Render(20, 1)
	for _, col := range []int{0, 2} {
		c := cellAt(t, g, 6+col, 0)
		if c.Style.FG != cRed || !c.Style.Underline {
			t.Errorf("pair bracket at %d: fg %v underline %v, want the depth color underlined", col, c.Style.FG, c.Style.Underline)
		}
	}
	if cellAt(t, g, 7, 0).Style.Underline {
		t.Error("the letter between the pair is underlined")
	}
}

func TestRainbowDepthCountsBracketsAboveTheWindow(t *testing.T) {
	d := newDoc("(", "  (", "  )", ")")
	g := Pane{Doc: d, Top: 2, Styles: bracketStyles(), Opts: Options{HighlightBrackets: true, RainbowBrackets: true}}.Render(20, 2)
	// The closer of line 2 closes the second level: blue.
	if got := cellAt(t, g, 8, 0).Style.FG; got != cBlue {
		t.Errorf("closer fg = %v, want the second depth color", got)
	}
	if got := cellAt(t, g, 6, 1).Style.FG; got != cRed {
		t.Errorf("outer closer fg = %v, want the first depth color", got)
	}
}

// The brackets of one very long line are marked in time that grows with the line, not
// with its square: a minified file is one line of 200 KB.
func TestRainbowBracketsOnOneHugeLineAreLinear(t *testing.T) {
	d := newDoc(strings.Repeat("[1,2],", 40000))
	d.cursor = buffer.Pos{Col: 3}
	p := Pane{Doc: d, Styles: bracketStyles(), Opts: Options{HighlightBrackets: true, RainbowBrackets: true}}
	start := time.Now()
	g := p.Render(60, 3)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("rendering took %v", took)
	}
	if got := cellAt(t, g, 6, 0).Style.FG; got != cRed {
		t.Errorf("first bracket fg = %v, want the depth color", got)
	}
}
