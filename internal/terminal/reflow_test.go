package terminal

import (
	"strings"
	"testing"

	"image/color"
)

func linesOf(rows ...string) []Line {
	out := make([]Line, len(rows))
	for i, s := range rows {
		out[i] = cellsFromText(s)
	}
	return out
}

func texts(lines []Line) []string {
	out := make([]string, len(lines))
	for i := range lines {
		out[i] = lines[i].Text()
	}
	return out
}

func TestReflowLinesWrapsAtTheNewWidth(t *testing.T) {
	t.Parallel()
	lines := linesOf("abcdef", "ghi")
	got := texts(ReflowLines(lines, 6, 3))
	want := []string{"abc", "def", "ghi"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("reflow = %q, want %q", got, want)
	}
	// Widening keeps the hard rows; the text is not padded.
	widened := texts(ReflowLines(linesOf("abc", "def"), 3, 6))
	if strings.Join(widened, "|") != "abc|def" {
		t.Fatalf("widened = %q", widened)
	}
}

func TestReflowLinesKeepsStyles(t *testing.T) {
	t.Parallel()
	red := color.RGBA{R: 255, A: 255}
	line := Line{Cells: []Cell{
		{Rune: 'a', Style: Style{Fg: red}},
		{Rune: 'b', Style: Style{Fg: red}},
		{Rune: 'c'},
	}}
	got := ReflowLines([]Line{line}, 3, 1)
	if len(got) != 3 {
		t.Fatalf("rows = %d, want 3", len(got))
	}
	if got[0].Cells[0].Style.Fg != red || got[1].Cells[0].Style.Fg != red {
		t.Fatalf("the style did not survive the reflow: %+v", got)
	}
	if got[2].Cells[0].Style.Fg == red {
		t.Fatalf("an unstyled cell took the red style: %+v", got[2])
	}
}

func TestReflowLinesNeverSplitsAWideGlyph(t *testing.T) {
	t.Parallel()
	// "名" is two cells: the rune and its continuation (Rune 0).
	line := Line{Cells: []Cell{
		{Rune: 'a'},
		{Rune: '名'},
		{Rune: 0},
	}}
	got := ReflowLines([]Line{line}, 3, 2)
	if len(got) != 2 {
		t.Fatalf("rows = %d, want 2", len(got))
	}
	if len(got[0].Cells) != 1 || got[0].Cells[0].Rune != 'a' {
		t.Fatalf("first row = %+v, want a alone", got[0].Cells)
	}
	if len(got[1].Cells) != 2 || got[1].Cells[0].Rune != '名' || got[1].Cells[1].Rune != 0 {
		t.Fatalf("second row = %+v, want the wide glyph kept whole", got[1].Cells)
	}
	// A narrow width below one glyph still keeps it whole on its own row.
	got = ReflowLines([]Line{line}, 3, 1)
	for _, row := range got {
		for i, c := range row.Cells {
			if c.Rune == 0 && (i == 0 || row.Cells[i-1].Rune != '名') {
				t.Fatalf("a lone continuation cell in %+v", row.Cells)
			}
		}
	}
}

func TestReflowRowsText(t *testing.T) {
	t.Parallel()
	got := ReflowRows([]string{"abcdef", "x"}, 6, 2)
	want := []string{"ab", "cd", "ef", "x"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	if got := ReflowRows(nil, 6, 2); got != nil {
		t.Fatalf("empty rows = %q", got)
	}
}
