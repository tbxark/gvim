package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/ui/grid"
)

func TestRulerPaintsItsColumnOnEveryBodyRow(t *testing.T) {
	// Ruler 4 is the fourth text column, which is cell 3 after the gutter.
	p := Pane{Doc: newDoc("ab", ""), Rulers: []int{4}, Styles: Styles{Ruler: grid.Style{BG: cGray}}}
	g := p.Render(12, 3)
	for y := range 3 {
		if c := cellAt(t, g, 9, y); bgOf(c) != cGray {
			t.Errorf("row %d column 9 bg = %v, want the ruler", y, bgOf(c))
		}
		if c := cellAt(t, g, 8, y); bgOf(c) != nil {
			t.Errorf("row %d column 8 has bg %v, want none", y, bgOf(c))
		}
	}
}

func TestRulerKeepsTheGlyphOfTheCellUnderIt(t *testing.T) {
	p := Pane{Doc: newDoc("abcdef"), Rulers: []int{2}, Styles: Styles{Ruler: grid.Style{BG: cGray}}}
	c := cellAt(t, p.Render(12, 2), 7, 0)
	if c.Text != "b" || bgOf(c) != cGray {
		t.Errorf("ruler cell = %q bg %v, want b on the ruler", c.Text, bgOf(c))
	}
}

func TestRulerLosesToTheSelectionBackground(t *testing.T) {
	d := newDoc("abcdef")
	d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Col: 3}
	p := Pane{Doc: d, Rulers: []int{2}, Styles: Styles{Ruler: grid.Style{BG: cGray}, Selection: grid.Style{BG: cBlue}}}
	if c := cellAt(t, p.Render(12, 2), 7, 0); bgOf(c) != cBlue {
		t.Errorf("selected ruler cell bg = %v, want the selection", bgOf(c))
	}
}

func TestRulerBeyondTheTextAreaIsNotPainted(t *testing.T) {
	p := Pane{Doc: newDoc("ab"), Rulers: []int{50}, Styles: Styles{Ruler: grid.Style{BG: cGray}}}
	g := p.Render(12, 2)
	for x := range g.Width() {
		if bgOf(cellAt(t, g, x, 0)) == cGray {
			t.Errorf("column %d painted with the ruler", x)
		}
	}
}

func TestCurrentLineTintsTheCaretRowOnly(t *testing.T) {
	p := Pane{Doc: newDoc("ab", "cd"), CurrentLine: true, Styles: Styles{CurrentLine: grid.Style{BG: cGray}}}
	g := p.Render(12, 2)
	for x := 6; x < 11; x++ {
		if bgOf(cellAt(t, g, x, 0)) != cGray {
			t.Errorf("caret row column %d bg = %v, want the current line", x, bgOf(cellAt(t, g, x, 0)))
		}
	}
	if c := cellAt(t, g, 6, 1); bgOf(c) != nil {
		t.Errorf("other row has bg %v", bgOf(c))
	}
	if c := cellAt(t, g, 11, 0); bgOf(c) != nil {
		t.Errorf("change column has bg %v, want none", bgOf(c))
	}
}

func TestCurrentLineOffWithoutTheOption(t *testing.T) {
	p := Pane{Doc: newDoc("ab"), Styles: Styles{CurrentLine: grid.Style{BG: cGray}}}
	if c := cellAt(t, p.Render(12, 2), 7, 0); bgOf(c) != nil {
		t.Errorf("current line painted without CurrentLine: bg %v", bgOf(c))
	}
}

func TestCurrentLineKeepsTheSelectionBackground(t *testing.T) {
	d := newDoc("abcdef")
	d.selected, d.anchor, d.head = true, buffer.Pos{}, buffer.Pos{Col: 3}
	p := Pane{Doc: d, CurrentLine: true, Styles: Styles{CurrentLine: grid.Style{BG: cGray}, Selection: grid.Style{BG: cBlue}}}
	g := p.Render(12, 2)
	if bgOf(cellAt(t, g, 6, 0)) != cBlue || bgOf(cellAt(t, g, 9, 0)) != cGray {
		t.Errorf("selected bg %v, unselected bg %v, want blue then gray", bgOf(cellAt(t, g, 6, 0)), bgOf(cellAt(t, g, 9, 0)))
	}
}
