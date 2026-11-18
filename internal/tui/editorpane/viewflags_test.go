package editorpane

import (
	"image/color"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
	"github.com/tbxark/gvim/internal/tui/grid"
	"github.com/tbxark/gvim/syntax"
)

func TestHideTabArrowsLeavesTabsAsBlanks(t *testing.T) {
	tests := []struct {
		name string
		line string
		hide bool
		want string // the text cells from column 6
	}{
		{"arrows shown", "a b\tc", false, "a·b→c"},
		{"arrows hidden keep the spaces as dots", "a b\tc", true, "a·b c"},
		{"hidden tab of four cells", "\tx", true, "    x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{
				Doc:               newDoc(tc.line),
				VisibleWhitespace: true,
				HideTabArrows:     tc.hide,
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

func TestHideFoldMarkersLeavesTheGutterEdgeBlank(t *testing.T) {
	tests := []struct {
		name string
		hide bool
		want string
	}{
		{"shown", false, "▾"},
		{"hidden", true, " "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := foldFixture(nil)
			g := Pane{Doc: d, HideFoldMarkers: tc.hide}.Render(20, 5)
			if got := cellAt(t, g, 5, 0).Text; got != tc.want {
				t.Errorf("fold marker cell = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHideVerticalScrollbarLeavesTheColumnBlank(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "x"
	}
	tests := []struct {
		name string
		hide bool
		want string // a non-blank glyph when the scrollbar shows, a blank when hidden
	}{
		{"shown", false, ""},
		{"hidden", true, " "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := Pane{Doc: newDoc(lines...), HideVerticalScrollbar: tc.hide}.Render(12, 5)
			got := cellAt(t, g, 11, 0).Text
			switch {
			case tc.hide && got != " ":
				t.Errorf("scrollbar cell = %q, want a blank", got)
			case !tc.hide && got == " ":
				t.Errorf("scrollbar cell is blank, want the thumb or track")
			}
		})
	}
}

func TestHideHorizontalScrollbarGivesItsRowToTheText(t *testing.T) {
	doc := newDoc("abcdefghijklmnopqrst", "b", "c")
	shown := Pane{Doc: doc}.Render(12, 3)
	if got := cellAt(t, shown, 6, 2).Text; got == "c" {
		t.Fatalf("setup: the bar row shows text %q, want the horizontal bar", got)
	}
	hidden := Pane{Doc: doc, HideHorizontalScrollbar: true}.Render(12, 3)
	if got := cellAt(t, hidden, 6, 2).Text; got != "c" {
		t.Errorf("last row with the bar hidden shows %q, want the text of line 3", got)
	}
}

func TestCaretShapeChangesTheCaretCell(t *testing.T) {
	tests := []struct {
		name  string
		shape CaretShape
		text  string
		rev   bool
		under bool
	}{
		{"block reverses the cell", CaretBlock, "b", true, false},
		{"bar draws a thin glyph", CaretBar, "▏", false, false},
		{"underline underlines the cell", CaretUnderline, "b", false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc := newDoc("abc")
			doc.cursor = buffer.Pos{Line: 0, Col: 1}
			g := Pane{Doc: doc, CaretShape: tc.shape}.Render(12, 2)
			c := cellAt(t, g, 7, 0)
			if c.Text != tc.text || c.Style.Reverse != tc.rev || c.Style.Underline != tc.under {
				t.Errorf("caret cell = %q reverse %v underline %v, want %q reverse %v underline %v",
					c.Text, c.Style.Reverse, c.Style.Underline, tc.text, tc.rev, tc.under)
			}
		})
	}
}

func TestHiddenVerticalScrollbarIsNotAHitTarget(t *testing.T) {
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "x"
	}
	tests := []struct {
		name string
		hide bool
		want HitKind
	}{
		{"shown is the thumb", false, HitScrollbarThumb},
		{"hidden is text", true, HitText},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Doc: newDoc(lines...), HideVerticalScrollbar: tc.hide}
			if got := p.Hit(12, 5, 11, 0).Kind; got != tc.want {
				t.Errorf("hit on the last column = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDisplayOptionsV010(t *testing.T) {
	colBG := color.RGBA{R: 30, G: 35, B: 45, A: 255}
	kwFG := color.RGBA{R: 255, G: 100, B: 100, A: 255}
	txtFG := color.RGBA{R: 220, G: 220, B: 220, A: 255}

	t.Run("FoldLineCount", func(t *testing.T) {
		g := Pane{
			Doc:  foldFixture(map[int]bool{0: true}),
			Opts: Options{FoldLineCount: true},
		}.Render(32, 4)
		row := g.Plain()[0]
		if !strings.Contains(row, "⋯ 2 lines") {
			t.Fatalf("folded row = %q, want '⋯ 2 lines'", row)
		}
		nested := foldDoc{
			fakeDoc: newDoc("0", "1", "2", "3", "4", "5"),
			ranges:  []fold.Range{{Start: 0, End: 2}, {Start: 0, End: 5}},
			folded:  map[int]bool{0: true},
		}
		g2 := Pane{
			Doc:  nested,
			Opts: Options{FoldLineCount: true},
		}.Render(32, 4)
		if row2 := g2.Plain()[0]; !strings.Contains(row2, "⋯ 5 lines") {
			t.Fatalf("folded row with overlapping start = %q, want '⋯ 5 lines'", row2)
		}
	})

	t.Run("HideTilde", func(t *testing.T) {
		g := Pane{
			Doc:  newDoc("only one line"),
			Opts: Options{HideTilde: true},
		}.Render(20, 4)
		if c := cellAt(t, g, 0, 2).Text; c == tildeText {
			t.Fatalf("row 2 still drew %q with HideTilde=true", c)
		}
	})

	t.Run("HideSyntax", func(t *testing.T) {
		d := newDoc("func a() {}")
		d.spans = map[int][]syntax.Span{0: {{Start: 0, End: 4, Kind: syntax.KindKeyword}}}
		var sy [syntax.KindCount]grid.Style
		for i := range sy {
			sy[i] = grid.Style{FG: txtFG}
		}
		sy[syntax.KindKeyword] = grid.Style{FG: kwFG}
		g := Pane{
			Doc:    d,
			Styles: Styles{Text: grid.Style{FG: txtFG}, Syntax: sy},
			Opts:   Options{HideSyntax: true},
		}.Render(25, 2)
		if got := cellAt(t, g, oneLineGutter, 0).Style.FG; got != txtFG {
			t.Fatalf("keyword FG with HideSyntax = %v, want %v", got, txtFG)
		}
	})

	t.Run("HideCurrentLineOnSelection", func(t *testing.T) {
		d := newDoc("hello", "world")
		d.anchor = buffer.Pos{Line: 0, Col: 0}
		d.head = buffer.Pos{Line: 0, Col: 3}
		d.selected = true
		g := Pane{
			Doc:         d,
			CurrentLine: true,
			Styles:      Styles{CurrentLine: grid.Style{BG: colBG}, Selection: grid.Style{BG: kwFG}},
			Opts:        Options{HideCurrentLineOnSelection: true},
		}.Render(20, 2)
		if got := cellAt(t, g, oneLineGutter+4, 0).Style.BG; got == colBG {
			t.Fatalf("current line BG painted while selection is active: %v", got)
		}
	})

	t.Run("HighlightCurrentColumn", func(t *testing.T) {
		d := newDoc("alpha", "beta")
		d.cursor = buffer.Pos{Line: 0, Col: 2}
		g := Pane{
			Doc:    d,
			Styles: Styles{CurrentLine: grid.Style{BG: colBG}},
			Opts:   Options{HighlightCurrentColumn: true},
		}.Render(20, 2)
		if got := cellAt(t, g, oneLineGutter+2, 1).Style.BG; got != colBG {
			t.Fatalf("column cell on row 1 BG = %v, want %v", got, colBG)
		}
	})

	t.Run("ScrollbarThumbWinsOverChangeMarks", func(t *testing.T) {
		lines := make([]string, 30)
		saved := make([]string, 30)
		for i := range lines {
			lines[i] = "modified"
			saved[i] = "saved"
		}
		d := newDoc(lines...)
		d.saved = saved
		d.modified = true
		g := Pane{Doc: d}.Render(16, 6)
		if got := cellAt(t, g, 15, 0).Text; got != thumbGlyph {
			t.Fatalf("scrollbar thumb cell = %q, want %q", got, thumbGlyph)
		}
	})
}
