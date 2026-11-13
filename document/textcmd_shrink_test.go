package document

import (
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// The selection history outlives edits, so a shrink must not put the caret past
// the end of a text that has become shorter.
func TestShrinkSelectionClampsToShorterText(t *testing.T) {
	t.Parallel()
	d := newDocument("", "foo bar baz")
	d.Cursor = buffer.Pos{Col: 5}
	d.ExpandSelection()
	if !d.HasSelection() {
		t.Fatal("ExpandSelection selected nothing")
	}
	d.Buffer.Delete(buffer.Pos{Col: 1}, d.Buffer.End())
	d.ShrinkSelection()
	if got := d.Buffer.Clamp(d.Cursor); got != d.Cursor {
		t.Errorf("Cursor = %v after shrink, want it inside the text (%v)", d.Cursor, got)
	}
	if d.Sel != nil {
		if got := d.Buffer.Clamp(d.Sel.Anchor); got != d.Sel.Anchor {
			t.Errorf("Sel.Anchor = %v after shrink, want it inside the text", d.Sel.Anchor)
		}
	}
}

// A caret move after an expand ends the chain: a later shrink must not bring back a
// selection from before the move.
func TestShrinkSelectionAfterTheCaretMovedDoesNothing(t *testing.T) {
	t.Parallel()
	d := newDocument("", "foo bar baz")
	d.Cursor = buffer.Pos{Col: 5}
	d.ExpandSelection()
	if !d.HasSelection() {
		t.Fatal("ExpandSelection selected nothing")
	}
	d.Move(buffer.Pos{Col: 9}, false)
	d.ShrinkSelection()
	if d.Cursor != (buffer.Pos{Col: 9}) || d.HasSelection() {
		t.Fatalf("Cursor = %v, selection = %v; want the moved caret kept and no selection", d.Cursor, d.Sel)
	}
	// A chain that continues from its own result still shrinks.
	d.Cursor = buffer.Pos{Col: 5}
	d.ExpandSelection()
	d.ExpandSelection()
	d.ShrinkSelection()
	if from, to := d.Sel.Ordered(); from != (buffer.Pos{Col: 4}) || to != (buffer.Pos{Col: 7}) {
		t.Fatalf("shrink = %v..%v, want the word 4..7", from, to)
	}
}
