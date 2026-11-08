package editorpane

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// The text area of a 20-column pane with the default gutter starts at column 6.
func TestSecondaryCaretPaintsItsCellInTheSecondaryStyle(t *testing.T) {
	d := caretDoc{fakeDoc: newDoc("abc", "def"), extra: []editor.Caret{{Cursor: buffer.Pos{Line: 1, Col: 2}}}}
	p := Pane{Doc: d, Styles: Styles{SecondaryCaret: grid.Style{FG: cBlue, BG: cRed}}}
	g := p.Render(20, 3)
	c := cellAt(t, g, 8, 1)
	if c.Text != "f" || bgOf(c) != cRed || c.Style.FG != cBlue {
		t.Errorf("secondary caret cell = %q bg %v fg %v, want f on red with blue fg", c.Text, bgOf(c), c.Style.FG)
	}
	if c.Style.Reverse {
		t.Error("secondary caret is reversed, want the secondary style")
	}
}

func TestSecondaryCaretPastTheTextIsABlankCell(t *testing.T) {
	d := caretDoc{fakeDoc: newDoc("abc"), extra: []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 3}}}}
	p := Pane{Doc: d, Styles: Styles{SecondaryCaret: grid.Style{BG: cRed}}}
	c := cellAt(t, p.Render(20, 2), 9, 0)
	if c.Text != " " || bgOf(c) != cRed {
		t.Errorf("cell past the text = %q bg %v, want a blank on red", c.Text, bgOf(c))
	}
}

func TestPrimaryCaretStaysReversedWithSecondaryCarets(t *testing.T) {
	d := caretDoc{fakeDoc: newDoc("abc", "def"), extra: []editor.Caret{{Cursor: buffer.Pos{Line: 1, Col: 0}}}}
	p := Pane{Doc: d, Styles: Styles{SecondaryCaret: grid.Style{BG: cRed}}}
	g := p.Render(20, 2)
	if c := cellAt(t, g, 6, 0); !c.Style.Reverse {
		t.Errorf("primary caret cell is not reversed: %+v", c.Style)
	}
	if c := cellAt(t, g, 6, 1); bgOf(c) != cRed || c.Style.Reverse {
		t.Errorf("secondary caret cell = %+v, want red and not reversed", c.Style)
	}
}

func TestNoSecondaryCaretsWithoutCarets(t *testing.T) {
	p := Pane{Doc: newDoc("abc", "def"), Styles: Styles{SecondaryCaret: grid.Style{BG: cRed}}}
	g := p.Render(20, 2)
	if c := cellAt(t, g, 6, 1); bgOf(c) != nil {
		t.Errorf("plain cell has bg %v", bgOf(c))
	}
}

func TestSecondaryCaretOffTheWindowIsNotPainted(t *testing.T) {
	d := caretDoc{fakeDoc: newDoc("a", "b", "c", "d", "e"), extra: []editor.Caret{{Cursor: buffer.Pos{Line: 4, Col: 0}}}}
	p := Pane{Doc: d, Styles: Styles{SecondaryCaret: grid.Style{BG: cRed}}}
	g := p.Render(20, 2)
	for y := range 2 {
		if c := cellAt(t, g, 6, y); bgOf(c) == cRed {
			t.Errorf("row %d shows a secondary caret that is off the window", y)
		}
	}
}

func TestExtraCaretSelectionIsPainted(t *testing.T) {
	sel := buffer.Selection{Anchor: buffer.Pos{Line: 1, Col: 0}, Head: buffer.Pos{Line: 1, Col: 2}}
	d := caretDoc{fakeDoc: newDoc("abc", "def"), extra: []editor.Caret{{Cursor: sel.Head, Sel: &sel}}}
	p := Pane{Doc: d, Styles: Styles{Selection: grid.Style{BG: cBlue}}}
	g := p.Render(20, 2)
	if bgOf(cellAt(t, g, 6, 1)) != cBlue || bgOf(cellAt(t, g, 7, 1)) != cBlue {
		t.Errorf("extra caret selection bg = %v, %v, want blue", bgOf(cellAt(t, g, 6, 1)), bgOf(cellAt(t, g, 7, 1)))
	}
	if bgOf(cellAt(t, g, 8, 1)) == cBlue {
		t.Error("cell past the extra selection is painted as selected")
	}
}

func TestCaretLineTintCoversEveryWrappedRow(t *testing.T) {
	d := newDoc("0123456789abcdef", "x")
	d.cursor = buffer.Pos{Line: 0, Col: 0}
	p := Pane{Doc: d, Wrap: true, CurrentLine: true, Styles: Styles{CurrentLine: grid.Style{BG: cGray}}}
	// The first line wraps to four rows at this width, so rows 0 to 3 are tinted
	// and row 4 is the next line.
	g := p.Render(12, 6)
	for y := range 4 {
		if bgOf(cellAt(t, g, 6, y)) != cGray {
			t.Errorf("wrapped row %d is not tinted", y)
		}
	}
	if bgOf(cellAt(t, g, 6, 4)) == cGray {
		t.Error("row of the next line is tinted")
	}
}

// A caret on a wide grapheme restyles its two cells and moves nothing after it.
func TestCaretOnWideGraphemeDoesNotShiftTheRow(t *testing.T) {
	st := Styles{SecondaryCaret: grid.Style{BG: cRed}}
	for _, tc := range []struct {
		name string
		doc  Document
	}{
		{"primary", newDoc("日本語 end")},
		{"selection", &fakeDoc{lines: []string{"日本語 end"}, selected: true, head: buffer.Pos{Col: 1}, cursor: buffer.Pos{Col: 1}}},
		{"secondary", caretDoc{fakeDoc: newDoc("日本語 end"), extra: []editor.Caret{{Cursor: buffer.Pos{Line: 0, Col: 0}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := Pane{Doc: tc.doc, Styles: st}
			g := p.Render(30, 2)
			row := g.Plain()[0]
			if want := "日本語 end"; !strings.Contains(row, want) {
				t.Errorf("row = %q, want it to contain %q", row, want)
			}
			if c := cellAt(t, g, 7, 0); c.Text != "" {
				t.Errorf("continuation cell = %q, want empty", c.Text)
			}
			if c := cellAt(t, g, 8, 0); c.Text != "本" {
				t.Errorf("next grapheme = %q, want 本", c.Text)
			}
		})
	}
}
