package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
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
