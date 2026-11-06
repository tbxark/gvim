package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// TestRevealTopShowsTheCaretRowAcrossWrappedLines uses three lines that wrap at
// W=14 (wrap width 6): "hello world" and "second line" take two rows each, and
// "third" takes one.
func TestRevealTopShowsTheCaretRowAcrossWrappedLines(t *testing.T) {
	tests := []struct {
		name      string
		line, col int
		top, h    int
		want      int
	}{
		{"a caret on screen keeps the top", 0, 0, 0, 4, 0},
		{"a caret below takes the fewest lines that fit", 2, 0, 0, 4, 1},
		{"a caret on a wrapped row counts its own row", 1, 7, 0, 2, 1},
		{"a caret row alone can fill the body", 2, 0, 0, 1, 2},
		{"a caret above the window takes its own line", 0, 0, 2, 4, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc("hello world", "second line", "third")
			d.cursor = buffer.Pos{Line: tc.line, Col: tc.col}
			p := Pane{Doc: d, Wrap: true, Top: tc.top}
			got := p.RevealTop(14, tc.h)
			if got != tc.want {
				t.Fatalf("RevealTop = %d, want %d", got, tc.want)
			}
			shown := p
			shown.Top = got
			if _, _, ok := shown.Caret(14, tc.h); !ok {
				t.Errorf("caret is off the body with the top at %d", got)
			}
		})
	}
}

func TestBodyRowsGivesTheHorizontalBarItsRow(t *testing.T) {
	// The line is 16 cells wide and a 12-wide pane has 5 text columns.
	long := "0123456789abcdef"
	tests := []struct {
		name  string
		wrap  bool
		lines []string
		want  int
	}{
		{"wrap on keeps every row for text", true, []string{long}, 4},
		{"wrap off with a wide line gives up the last row", false, []string{long}, 3},
		{"wrap off with lines that fit keeps every row", false, []string{"ab"}, 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Doc: newDoc(tc.lines...), Wrap: tc.wrap}
			if got := p.BodyRows(12, 4); got != tc.want {
				t.Errorf("BodyRows(12, 4) = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestHorizontalBarRowIsNotTextAndCountsForTheVerticalBar(t *testing.T) {
	// Four lines in a four-row pane. The bar takes the last row, so three text rows
	// hold the lines, and the document overflows: the last column is the scrollbar.
	g := Pane{Doc: newDoc("0123456789abcdef", "b", "c", "d")}.Render(12, 4)
	if got := cellAt(t, g, 11, 0).Text; got != thumbGlyph && got != trackGlyph {
		t.Errorf("scrollbar at (11, 0) = %q, want the vertical bar", got)
	}
	if got := barRow(t, g, 1); got != "b" {
		t.Errorf("row 1 = %q, want line b", got)
	}
	if got := barRow(t, g, 2); got != "c" {
		t.Errorf("row 2 = %q, want line c", got)
	}
	if got := cellAt(t, g, 6, 3).Text; got != thumbHGlyph {
		t.Errorf("row 3 at the gutter = %q, want the horizontal bar thumb", got)
	}
}
