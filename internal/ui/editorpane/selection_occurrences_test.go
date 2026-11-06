package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

func TestSelectedTextOccurrencesAreHighlighted(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		sel   [2]buffer.Pos
		on    [][2]int // text column and row of the cells that carry the occurrence
		off   [][2]int
	}{
		{
			name:  "matches inside longer words too",
			lines: []string{"bet beta", "abet"},
			sel:   [2]buffer.Pos{{Line: 0, Col: 0}, {Line: 0, Col: 3}},
			on:    [][2]int{{4, 0}, {6, 0}, {1, 1}, {3, 1}},
			off:   [][2]int{{3, 0}, {0, 1}},
		},
		{
			name:  "the selection itself keeps the selection color",
			lines: []string{"bet bet"},
			sel:   [2]buffer.Pos{{Line: 0, Col: 0}, {Line: 0, Col: 3}},
			on:    [][2]int{{4, 0}},
			off:   [][2]int{{0, 0}, {2, 0}},
		},
		{
			name:  "a selection of one character is too short",
			lines: []string{"abab"},
			sel:   [2]buffer.Pos{{Line: 0, Col: 0}, {Line: 0, Col: 1}},
			off:   [][2]int{{2, 0}},
		},
		{
			name:  "a selection of blanks is not matched",
			lines: []string{"a  b  c"},
			sel:   [2]buffer.Pos{{Line: 0, Col: 1}, {Line: 0, Col: 3}},
			off:   [][2]int{{4, 0}, {5, 0}},
		},
		{
			name:  "a selection over several lines marks nothing",
			lines: []string{"ab ab", "ab"},
			sel:   [2]buffer.Pos{{Line: 0, Col: 0}, {Line: 1, Col: 2}},
			off:   [][2]int{{3, 0}},
		},
		{
			name:  "a backward selection is the same",
			lines: []string{"foo bar foo"},
			sel:   [2]buffer.Pos{{Line: 0, Col: 3}, {Line: 0, Col: 0}},
			on:    [][2]int{{8, 0}, {10, 0}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoc(tc.lines...)
			d.selected, d.anchor, d.head = true, tc.sel[0], tc.sel[1]
			d.cursor = tc.sel[1]
			p := Pane{Doc: d, Occurrences: true, Styles: Styles{Occurrence: grid.Style{BG: cGray}, Selection: grid.Style{BG: cBlue}}}
			g := p.Render(20, len(tc.lines))
			for _, c := range tc.on {
				if got := bgOf(cellAt(t, g, 6+c[0], c[1])); got != cGray {
					t.Errorf("column %d of row %d bg = %v, want the occurrence", c[0], c[1], got)
				}
			}
			for _, c := range tc.off {
				if got := bgOf(cellAt(t, g, 6+c[0], c[1])); got == cGray {
					t.Errorf("column %d of row %d is marked as an occurrence", c[0], c[1])
				}
			}
		})
	}
}

func TestSelectedTextOccurrencesNeedTheOption(t *testing.T) {
	d := newDoc("foo foo")
	d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Col: 3}
	g := Pane{Doc: d, Styles: Styles{Occurrence: grid.Style{BG: cGray}}}.Render(20, 1)
	if got := bgOf(cellAt(t, g, 10, 0)); got == cGray {
		t.Error("occurrence painted with the option off")
	}
}

func TestMatchableSelection(t *testing.T) {
	long := make([]byte, maxSelectionMatch+1)
	for i := range long {
		long[i] = 'x'
	}
	for text, want := range map[string]bool{
		"ab": true, "a": false, "": false, "  ": false, " a": true, "a\nb": false, "a\rb": false,
		string(long): false, string(long[:maxSelectionMatch]): true, "é": true,
	} {
		if got := matchableSelection(text); got != want {
			t.Errorf("matchableSelection(%q) = %v, want %v", text[:min(len(text), 8)], got, want)
		}
	}
}
