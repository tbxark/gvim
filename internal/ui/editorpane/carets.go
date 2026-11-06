package editorpane

import (
	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/ui/grid"
)

// CaretLister is implemented by a Document that has extra carets, as
// *editor.Document does. The pane paints each extra caret in the secondary
// caret style, and it paints the selections of the extra carets too.
type CaretLister interface {
	Cursors() []editor.Caret
}

// setSelections collects the selections of the primary caret and of the extra
// carets, and the positions of the extra carets. A document with one caret
// leaves the extra list empty.
func (f *frame) setSelections(doc Document) {
	if a, h, ok := doc.Selection(); ok {
		f.sels = append(f.sels, buffer.Selection{Anchor: a, Head: h})
	}
	cl, ok := doc.(CaretLister)
	if !ok {
		return
	}
	for _, c := range cl.Cursors() {
		f.extra = append(f.extra, c.Cursor)
		if c.Sel != nil {
			f.sels = append(f.sels, *c.Sel)
		}
	}
}

// paintSecondaryCarets draws each extra caret in the secondary caret style. A
// caret whose cell is off the window is not drawn. When the theme gives the
// style no colors, the cell is reversed, as the primary caret is.
func (f frame) paintSecondaryCarets(g *grid.Grid) {
	st := f.styles.SecondaryCaret
	for _, p := range f.extra {
		x, y, ok := f.cellOf(p)
		if !ok || x < f.gutter || x >= f.w-1 {
			continue
		}
		c := g.At(x, y)
		text := c.Text
		if text == "" {
			text = " "
		}
		if st.FG == nil && st.BG == nil {
			rev := c.Style
			rev.Reverse = true
			g.Set(x, y, text, rev)
			continue
		}
		g.Set(x, y, text, st)
	}
}
