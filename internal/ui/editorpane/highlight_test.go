package editorpane

import (
	"image/color"
	"slices"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// oneLineGutter is the gutter width of a document with one line: the number
// field of three digits, and the separator " │ ".
const oneLineGutter = 6

var (
	matchBG     = color.RGBA{R: 100, G: 100, B: 20, A: 255}
	currentBG   = color.RGBA{R: 64, G: 170, B: 230, A: 255}
	currentFG   = color.RGBA{A: 255}
	selectionBG = color.RGBA{R: 50, G: 50, B: 60, A: 255}
)

// highlightsOn gives hs on line line and nothing on the other lines.
func highlightsOn(line int, hs ...Highlight) func(int) []Highlight {
	return func(i int) []Highlight {
		if i == line {
			return hs
		}
		return nil
	}
}

// assertBG checks the background of the cell at (x, y).
func assertBG(t *testing.T, g *grid.Grid, x, y int, want color.Color) {
	t.Helper()
	if got := g.At(x, y).Style.BG; got != want {
		t.Errorf("cell (%d, %d) bg = %v, want %v", x, y, got, want)
	}
}

func TestSearchMatchPaintsItsBackground(t *testing.T) {
	p := Pane{
		Doc:        newDoc("hello world"),
		Styles:     Styles{Match: grid.Style{BG: matchBG}},
		Highlights: highlightsOn(0, Highlight{Start: 6, End: 11}),
	}
	g := p.Render(20, 1)
	assertRows(t, g, []string{"  1 │ hello world"})
	// "world" is rune columns 6 to 10, which are cells 12 to 16.
	assertBG(t, g, oneLineGutter+5, 0, nil)
	for x := oneLineGutter + 6; x < oneLineGutter+11; x++ {
		assertBG(t, g, x, 0, matchBG)
	}
	assertBG(t, g, oneLineGutter+11, 0, nil)
}

func TestCurrentSearchMatchUsesItsOwnColors(t *testing.T) {
	p := Pane{
		Doc: newDoc("hello world"),
		Styles: Styles{
			Match:        grid.Style{BG: matchBG},
			CurrentMatch: grid.Style{FG: currentFG, BG: currentBG, Bold: true},
		},
		Highlights: highlightsOn(0,
			Highlight{Start: 0, End: 5},
			Highlight{Start: 6, End: 11, Current: true},
		),
	}
	g := p.Render(20, 1)
	// "hello" is an ordinary match: the match background, and no other attribute.
	ordinary := g.At(oneLineGutter, 0).Style
	if ordinary.BG != matchBG || ordinary.FG != nil || ordinary.Bold {
		t.Errorf("ordinary match = %+v, want the match background only", ordinary)
	}
	// "world" is the current match: its own foreground, background and bold.
	current := g.At(oneLineGutter+6, 0).Style
	if current.BG != currentBG || current.FG != currentFG || !current.Bold {
		t.Errorf("current match = %+v, want the current colors in bold", current)
	}
}

func TestCurrentMatchWinsOverAnOverlappingMatch(t *testing.T) {
	// The current match is listed first, so a cell that the ordinary match
	// covers last must still take the current colors.
	p := Pane{
		Doc: newDoc("hello world"),
		Styles: Styles{
			Match:        grid.Style{BG: matchBG},
			CurrentMatch: grid.Style{BG: currentBG},
		},
		Highlights: highlightsOn(0,
			Highlight{Start: 6, End: 11, Current: true},
			Highlight{Start: 0, End: 11},
		),
	}
	g := p.Render(20, 1)
	assertBG(t, g, oneLineGutter, 0, matchBG)
	for x := oneLineGutter + 6; x < oneLineGutter+11; x++ {
		assertBG(t, g, x, 0, currentBG)
	}
}

func TestSearchMatchAfterATabUsesTheExpandedCells(t *testing.T) {
	// "a\tbc" is shown as "a   bc": the tab fills cells 1 to 3, so "b" is cell 4
	// and "c" is cell 5.
	cases := []struct {
		name string
		hl   Highlight
		hit  []int // the cells that take the match background
	}{
		{name: "match on the letter after the tab", hl: Highlight{Start: 2, End: 3}, hit: []int{4}},
		{name: "match on the tab paints all its blanks", hl: Highlight{Start: 1, End: 2}, hit: []int{1, 2, 3}},
		{name: "match over the tab and the letter after", hl: Highlight{Start: 1, End: 3}, hit: []int{1, 2, 3, 4}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{
				Doc:        newDoc("a\tbc"),
				Styles:     Styles{Match: grid.Style{BG: matchBG}},
				Highlights: highlightsOn(0, tc.hl),
			}
			g := p.Render(14, 1)
			assertRows(t, g, []string{"  1 │ a   bc"})
			for k := range 6 {
				var want color.Color
				if slices.Contains(tc.hit, k) {
					want = matchBG
				}
				assertBG(t, g, oneLineGutter+k, 0, want)
			}
		})
	}
}

func TestSearchMatchOnAWrappedRow(t *testing.T) {
	// At width 14 "hello world" wraps to "hello " and "world", so the match on
	// "world" is on row 1.
	p := Pane{
		Wrap:       true,
		Doc:        newDoc("hello world"),
		Styles:     Styles{Match: grid.Style{BG: matchBG}},
		Highlights: highlightsOn(0, Highlight{Start: 6, End: 11}),
	}
	g := p.Render(14, 2)
	assertRows(t, g, []string{"  1 │ hello", "    │ world"})
	for x := oneLineGutter; x < oneLineGutter+5; x++ {
		assertBG(t, g, x, 0, nil)
		assertBG(t, g, x, 1, matchBG)
	}
}

func TestSelectionPaintsOverSearchMatch(t *testing.T) {
	d := newDoc("a b c")
	d.selected = true
	d.anchor, d.head = buffer.Pos{Col: 1}, buffer.Pos{Col: 4}
	p := Pane{
		Doc: d,
		Styles: Styles{
			Selection: grid.Style{BG: selectionBG},
			Match:     grid.Style{BG: matchBG},
		},
		// The match covers "a b" and the selection covers " b ".
		Highlights: highlightsOn(0, Highlight{Start: 0, End: 3}),
	}
	g := p.Render(14, 1)
	assertBG(t, g, oneLineGutter, 0, matchBG)
	assertBG(t, g, oneLineGutter+1, 0, selectionBG)
	assertBG(t, g, oneLineGutter+2, 0, selectionBG)
	assertBG(t, g, oneLineGutter+3, 0, selectionBG)
	assertBG(t, g, oneLineGutter+4, 0, nil)
}

func TestNoHighlightsPaintsNothing(t *testing.T) {
	st := Styles{Match: grid.Style{BG: matchBG}, CurrentMatch: grid.Style{BG: currentBG}}
	g := Pane{Doc: newDoc("hello"), Styles: st}.Render(14, 2)
	for x := oneLineGutter; x < oneLineGutter+5; x++ {
		assertBG(t, g, x, 0, nil)
	}
	// A pane with no document paints no highlight either, and does not panic.
	g = Pane{Styles: st, Highlights: highlightsOn(0, Highlight{Start: 0, End: 5})}.Render(14, 2)
	assertBG(t, g, oneLineGutter, 0, nil)
}

func TestSearchHighlightsPastTheDocumentAreIgnored(t *testing.T) {
	// Every line index gets a match. The document has two lines and the pane
	// shows four rows, so rows 2 and 3 are "~" rows with no line behind them.
	all := func(int) []Highlight { return []Highlight{{Start: 0, End: 9, Current: true}} }
	p := Pane{
		Doc:        newDoc("ab", "cd"),
		Styles:     Styles{CurrentMatch: grid.Style{BG: currentBG}},
		Highlights: all,
	}
	g := p.Render(14, 4)
	for y := range 2 {
		// The match ends past the two-character line, so cell 8 stays plain.
		assertBG(t, g, oneLineGutter, y, currentBG)
		assertBG(t, g, oneLineGutter+1, y, currentBG)
		assertBG(t, g, oneLineGutter+2, y, nil)
	}
	for y := 2; y < 4; y++ {
		assertBG(t, g, oneLineGutter, y, nil)
	}
}
