package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/fold"
	"github.com/tbxark/gvim/internal/ui/icons"
)

// foldFixture is a function with a body of two lines, and a plain line after it.
func foldFixture(folded map[int]bool) foldDoc {
	return foldDoc{
		fakeDoc: newDoc("func a() {", "\tx := 1", "\ty := 2", "}", "z"),
		ranges:  []fold.Range{{Start: 0, End: 2}},
		folded:  folded,
	}
}

func TestFoldMarkersShowOnTheFoldStartLine(t *testing.T) {
	tests := []struct {
		name   string
		folded map[int]bool
		want   string
	}{
		{"expanded", nil, "▾"},
		{"collapsed", map[int]bool{0: true}, "▸"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: foldFixture(tc.folded)}.Render(20, 5)
			if got := cellAt(t, g, 5, 0).Text; got != tc.want {
				t.Errorf("marker = %q, want %q", got, tc.want)
			}
			if got := cellAt(t, g, 5, 1).Text; got != " " {
				t.Errorf("body line marker = %q, want none", got)
			}
		})
	}
}

func TestFoldMarkersUseTheIconSet(t *testing.T) {
	tests := []struct {
		name   string
		folded map[int]bool
		want   string
	}{
		{"expanded", nil, icons.Nerd.Glyph(icons.FoldOpen)},
		{"collapsed", map[int]bool{0: true}, icons.Nerd.Glyph(icons.FoldClosed)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: foldFixture(tc.folded), Icons: icons.Nerd}.Render(20, 5)
			if got := cellAt(t, g, 5, 0).Text; got != tc.want {
				t.Errorf("marker = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExpandedFoldShowsEveryLine(t *testing.T) {
	g := Pane{Doc: foldFixture(nil)}.Render(20, 5)
	assertRows(t, g, []string{
		"  1 │▾func a() {",
		"  2 │     x := 1",
		"  3 │     y := 2",
		"  4 │ }",
		"  5 │ z",
	})
}

func TestCollapsedFoldHidesItsBodyAndNumbersTheRest(t *testing.T) {
	g := Pane{Doc: foldFixture(map[int]bool{0: true})}.Render(20, 4)
	assertRows(t, g, []string{
		"  1 │▸func a() { ..",
		"  4 │ }",
		"  5 │ z",
		"~",
	})
}

func TestHitOnACollapsedFoldMapsToTheLineAfterIt(t *testing.T) {
	p := Pane{Doc: foldFixture(map[int]bool{0: true})}
	if h := p.Hit(20, 4, 6, 1); h.Kind != HitText || h.Pos.Line != 3 {
		t.Errorf("hit on row 1 = %+v, want text on line 3", h)
	}
}

// TestFoldMarkerFollowsTheTextWhenTheGutterIsHidden checks the marker of a fold start
// line with the gutter hidden. The marker goes in the cell after the line's text, so
// no text cell is overwritten. A line that fills the row leaves no free cell before the
// scrollbar column, and then it gets no marker.
func TestFoldMarkerFollowsTheTextWhenTheGutterIsHidden(t *testing.T) {
	const text = "func a() {"
	tests := []struct {
		name   string
		width  int
		folded map[int]bool
		marker string
		col    int // the column of the marker, or -1 when the row has none
	}{
		{"expanded", 20, nil, "▾", 10},
		{"collapsed", 20, map[int]bool{0: true}, "▸", 14}, // after the " ..." placeholder
		{"full row has no free cell", 10, map[int]bool{0: true}, "", -1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: foldFixture(tc.folded), HideGutter: true}.Render(tc.width, 5)
			// Every text cell that fits before the scrollbar column keeps its letter.
			for x := range min(len(text), tc.width-1) {
				if got, want := cellAt(t, g, x, 0).Text, text[x:x+1]; got != want {
					t.Errorf("column %d of the fold line = %q, want the text %q", x, got, want)
				}
			}
			if tc.col >= 0 {
				if got := cellAt(t, g, tc.col, 0).Text; got != tc.marker {
					t.Errorf("column %d of the fold line = %q, want the marker %q", tc.col, got, tc.marker)
				}
				return
			}
			for x := range tc.width {
				if got := cellAt(t, g, x, 0).Text; got == "▾" || got == "▸" {
					t.Errorf("column %d of the fold line = %q, want no marker on a full row", x, got)
				}
			}
		})
	}
}

func TestFoldMarkersNeedAFoldingDocument(t *testing.T) {
	g := Pane{Doc: newDoc("func a() {", "\tx")}.Render(20, 2)
	if got := cellAt(t, g, 5, 0).Text; got != " " {
		t.Errorf("marker = %q on a document with no folds", got)
	}
}
