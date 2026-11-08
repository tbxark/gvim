package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/internal/tui/grid"
)

func TestRelativeLineNumbers(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e"}
	tests := []struct {
		name     string
		relative bool
		caret    int
		want     []string
	}{
		{"absolute", false, 2, []string{"  1 │ a", "  2 │ b", "  3 │ c", "  4 │ d", "  5 │ e"}},
		{"caret in the middle", true, 2, []string{"  2 │ a", "  1 │ b", "  3 │ c", "  1 │ d", "  2 │ e"}},
		{"caret on the first line", true, 0, []string{"  1 │ a", "  1 │ b", "  2 │ c", "  3 │ d", "  4 │ e"}},
		{"caret on the last line", true, 4, []string{"  4 │ a", "  3 │ b", "  2 │ c", "  1 │ d", "  5 │ e"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc(lines...)
			d.cursor = buffer.Pos{Line: tc.caret}
			g := Pane{Doc: d, Opts: Options{RelativeLineNumbers: tc.relative}}.Render(20, 5)
			assertRows(t, g, tc.want)
		})
	}
}

func TestRelativeLineNumbersNumberOnlyTheFirstRowOfAWrappedLine(t *testing.T) {
	d := newDoc("a", "aaaa bbbb cccc dddd")
	d.cursor = buffer.Pos{Line: 0}
	g := Pane{Doc: d, Wrap: true, Opts: Options{RelativeLineNumbers: true}}.Render(20, 4)
	assertRows(t, g, []string{"  1 │ a", "  1 │ aaaa bbbb", "    │ cccc dddd", "~"})
}

func TestRelativeLineNumberOfTheCaretLineTakesTheTextColor(t *testing.T) {
	d := newDoc("a", "b")
	st := Styles{Text: grid.Style{FG: cGreen}, Gutter: grid.Style{FG: cGray}}
	g := Pane{Doc: d, Styles: st, Opts: Options{RelativeLineNumbers: true}}.Render(20, 2)
	if got := cellAt(t, g, 2, 0).Style.FG; got != cGreen {
		t.Errorf("caret line number fg = %v, want the text color", got)
	}
	if got := cellAt(t, g, 2, 1).Style.FG; got != cGray {
		t.Errorf("other line number fg = %v, want the gutter color", got)
	}
}

func TestCollapsedFoldShowsAPlaceholderAfterItsHeader(t *testing.T) {
	st := Styles{Decor: DecorStyles{FoldPlaceholder: grid.Style{FG: cGray, Italic: true}}}
	tests := []struct {
		name       string
		folded     map[int]bool
		hideMarker bool
		want       string // the text cells from column 6 of row 0
	}{
		{"collapsed", map[int]bool{0: true}, false, "func a() { ..."},
		{"expanded", nil, false, "func a() {"},
		{"collapsed with the fold arrows hidden", map[int]bool{0: true}, true, "func a() { ..."},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: foldFixture(tc.folded), Styles: st, HideFoldMarkers: tc.hideMarker}.Render(30, 3)
			if got := trimRight(textFrom(t, g, 0, 6, 14)); got != tc.want {
				t.Errorf("row 0 = %q, want %q", got, tc.want)
			}
			if tc.folded[0] {
				c := cellAt(t, g, 6+10+1, 0)
				if c.Style.FG != cGray || !c.Style.Italic {
					t.Errorf("placeholder style = %+v, want the placeholder style", c.Style)
				}
				if c := cellAt(t, g, 6+3, 0); c.Style.Italic {
					t.Error("header text takes the placeholder style")
				}
			}
		})
	}
}

func TestFoldPlaceholderIsNotPartOfTheText(t *testing.T) {
	p := Pane{Doc: foldFixture(map[int]bool{0: true})}
	// A click on the placeholder is the end of the header line.
	if got := p.PosAt(30, 3, 6+12, 0); got != (buffer.Pos{Line: 0, Col: 10}) {
		t.Errorf("PosAt(placeholder) = %+v, want the end of line 0", got)
	}
}
