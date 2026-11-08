package editorpane

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// linesDoc is a document whose caret sits on a virtual line below the last line.
type linesDoc struct {
	*fakeDoc
	lines, col int
}

func (d linesDoc) VirtualLines() int      { return d.lines }
func (d linesDoc) VirtualLineColumn() int { return d.col }

// vlinesPane returns a pane over doc with the caret parked below the text.
func vlinesPane(doc *fakeDoc, lines, col int) Pane {
	doc.cursor = buffer.Pos{Line: doc.LineCount() - 1, Col: len(doc.Line(doc.LineCount() - 1))}
	return Pane{Doc: linesDoc{fakeDoc: doc, lines: lines, col: col}, HideGutter: true, Wrap: false}
}

func TestVirtualLineCaretDrawnBelowTheLastLine(t *testing.T) {
	t.Parallel()
	doc := newDoc("ab", "cd")
	p := vlinesPane(doc, 1, 2)
	g := p.Render(10, 4)
	rows := rowsOf(g)
	if rows[0] != "ab" || rows[1] != "cd" {
		t.Fatalf("rows = %q, want the text", rows)
	}
	// The caret is on the empty row below the last line, at display column 2.
	x, y, ok := p.Caret(10, 4)
	if !ok || x != 2 || y != 2 {
		t.Fatalf("Caret = (%d,%d,%v), want (2,2,true)", x, y, ok)
	}
	if !g.At(2, 2).Style.Reverse {
		t.Fatalf("cell (2,2) = %+v, want the caret", g.At(2, 2))
	}
	if g.At(2, 1).Style.Reverse {
		t.Fatal("the caret is drawn on the last text row too")
	}
}

func TestVirtualLineCaretClampsToTheBodyBottom(t *testing.T) {
	t.Parallel()
	doc := newDoc("ab", "cd")
	p := vlinesPane(doc, 8, 0)
	if _, y, ok := p.Caret(10, 3); !ok || y != 2 {
		t.Fatalf("Caret row = %d (%v), want the last body row 2", y, ok)
	}
}

func TestVirtualLineCaretScrollsItsColumnIntoView(t *testing.T) {
	t.Parallel()
	doc := newDoc("ab")
	p := vlinesPane(doc, 1, 20)
	x, y, ok := p.Caret(8, 3)
	if !ok || y != 1 || x < 0 || x >= 7 {
		t.Fatalf("Caret = (%d,%d,%v), want it inside the text area", x, y, ok)
	}
	if left, most := p.HScroll(8, 3); left == 0 || most == 0 {
		t.Fatalf("HScroll = %d of %d, want the window scrolled to the virtual line's column", left, most)
	}
}

func TestHitBelowTheLastLine(t *testing.T) {
	t.Parallel()
	doc := newDoc("ab", "cd")
	doc.cursor = buffer.Pos{Line: 1, Col: 2}
	p := Pane{Doc: doc, HideGutter: true, Wrap: false}
	hit := p.Hit(10, 4, 3, 3)
	if hit.Kind != HitText || hit.Below != 2 || hit.LineCol != 3 {
		t.Fatalf("hit below = %+v, want a text hit 2 rows below at column 3", hit)
	}
	if hit.Pos != (buffer.Pos{Line: 1, Col: 2}) {
		t.Fatalf("hit position = %+v, want the buffer end", hit.Pos)
	}
	if onText := p.Hit(10, 4, 1, 1); onText.Below != 0 || onText.LineCol != 0 {
		t.Fatalf("hit on text = %+v, want no virtual line", onText)
	}
}

func TestHitBelowNeedsTheBufferEndOnScreen(t *testing.T) {
	t.Parallel()
	doc := newDoc("a", "b", "c", "d")
	doc.cursor = buffer.Pos{Line: 3, Col: 1}
	p := Pane{Doc: doc, HideGutter: true, Wrap: false, Top: 0}
	// Four lines in a two-row body: the buffer end is not visible.
	hit := p.Hit(10, 2, 2, 1)
	if hit.Below != 0 {
		t.Fatalf("hit below = %+v, want no virtual line when the buffer end is off screen", hit)
	}
}

func TestHitBelowIsOffWithWrap(t *testing.T) {
	t.Parallel()
	doc := newDoc("ab")
	doc.cursor = buffer.Pos{Line: 0, Col: 2}
	p := Pane{Doc: doc, HideGutter: true, Wrap: true}
	if hit := p.Hit(10, 4, 2, 1); hit.Below != 0 {
		t.Fatalf("hit = %+v, want no virtual line with wrap on", hit)
	}
}
