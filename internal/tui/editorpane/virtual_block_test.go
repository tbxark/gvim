package editorpane

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui/style"
)

// blockDoc is a document with a block selection's rectangle.
type blockDoc struct {
	*fakeDoc
	rect editor.BlockRect
}

func (d blockDoc) BlockSelection() (editor.BlockRect, bool) { return d.rect, true }

// blockPane returns a pane over doc that paints the selection background, with
// the block rect of test.
func blockPane(t *testing.T, doc *fakeDoc, rect editor.BlockRect) (Pane, Styles) {
	t.Helper()
	doc.cursor = buffer.Pos{Line: doc.cursor.Line, Col: doc.cursor.Col}
	styles := Styles{Selection: style.Cell(lipgloss.NewStyle().Background(lipgloss.Color("2")))}
	return Pane{Doc: blockDoc{fakeDoc: doc, rect: rect}, HideGutter: true, Wrap: false, Styles: styles}, styles
}

func TestBlockVirtualStripPainted(t *testing.T) {
	t.Parallel()
	doc := newDoc("abc")
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	p, styles := blockPane(t, doc, editor.BlockRect{
		Top: 0, Bottom: 0, Left: 2, Right: 5,
		Head: buffer.Pos{Line: 0, Col: 5}, Virtual: true,
	})
	g := p.Render(8, 2)
	if got := rowsOf(g)[0]; got != "abc" {
		t.Fatalf("row = %q, want the text", got)
	}
	// The real part of the rectangle (column 2) is not painted because no
	// per-caret selection exists in this fixture; the virtual strip right of the
	// text is painted as the selection background.
	for _, x := range []int{3, 4} {
		if bg := g.At(x, 0).Style.BG; bg == nil || bg != styles.Selection.BG {
			t.Fatalf("cell %d = %+v, want the selection background", x, g.At(x, 0))
		}
	}
	// The caret (head) sits at the rectangle's right edge, past the text.
	if x, y, ok := p.Caret(8, 2); !ok || x != 5 || y != 0 {
		t.Fatalf("Caret = (%d,%d,%v), want (5,0,true)", x, y, ok)
	}
	if !g.At(5, 0).Style.Reverse {
		t.Fatalf("no caret at the virtual head: %+v", g.At(5, 0))
	}
}

func TestBlockVirtualRectWithinTextPaintsNoStrip(t *testing.T) {
	t.Parallel()
	doc := newDoc("abcdef")
	doc.cursor = buffer.Pos{Line: 0, Col: 4}
	p, styles := blockPane(t, doc, editor.BlockRect{
		Top: 0, Bottom: 0, Left: 1, Right: 4,
		Head: buffer.Pos{Line: 0, Col: 4}, Virtual: true,
	})
	g := p.Render(10, 2)
	for x := 0; x < 6; x++ {
		if bg := g.At(x, 0).Style.BG; bg == styles.Selection.BG {
			t.Fatalf("cell %d painted as the virtual strip, want nothing", x)
		}
	}
	if x, _, _ := p.Caret(10, 2); x != 4 {
		t.Fatalf("caret x = %d, want 4 (on the text)", x)
	}
}

func TestBlockVirtualHeadScrollsIntoView(t *testing.T) {
	t.Parallel()
	doc := newDoc("abc")
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	p, _ := blockPane(t, doc, editor.BlockRect{
		Top: 0, Bottom: 0, Left: 3, Right: 23,
		Head: buffer.Pos{Line: 0, Col: 23}, Virtual: true,
	})
	x, y, ok := p.Caret(8, 3)
	if !ok || y != 0 || x < 0 || x >= 7 {
		t.Fatalf("Caret = (%d,%d,%v), want it inside the text area", x, y, ok)
	}
	if left, most := p.HScroll(8, 3); left == 0 || most == 0 {
		t.Fatalf("HScroll = %d of %d, want the window scrolled to the block head", left, most)
	}
}

func TestBlockVirtualStripRespectsTheWindow(t *testing.T) {
	t.Parallel()
	doc := newDoc("abc")
	doc.cursor = buffer.Pos{Line: 0, Col: 3}
	p, styles := blockPane(t, doc, editor.BlockRect{
		Top: 0, Bottom: 0, Left: 3, Right: 60,
		Head: buffer.Pos{Line: 0, Col: 60}, Virtual: true,
	})
	// The rect's virtual head makes the window scroll; the strip is capped at the
	// last text column.
	g := p.Render(8, 2)
	x, _, _ := p.Caret(8, 2)
	if got := g.At(7, 0).Style.BG; got == styles.Selection.BG {
		t.Fatal("the strip painted over the scrollbar column")
	}
	if !g.At(x, 0).Style.Reverse {
		t.Fatalf("no caret at (%d,0)", x)
	}
}
