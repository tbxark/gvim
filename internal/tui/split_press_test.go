package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/buffer"
)

// TestPressOnAnUnfocusedPaneLandsOnTheLineUnderThePointer checks that a press is
// resolved against the pane as it is shown, before the focus moves. Focusing the pane
// reveals its caret, so a press resolved after the focus would land on another line.
func TestPressOnAnUnfocusedPaneLandsOnTheLineUnderThePointer(t *testing.T) {
	f := newMultiFake(1, 30)
	m := newSplitModel(t, f, 80, 24)
	runAction(t, m, "split_vertical") // the right pane has the focus
	m.ed.ScrollTo(10)                 // the right pane shows line 10 at its top
	runAction(t, m, "next_split")     // the left pane takes the focus; the right keeps top 10

	ps := panesOf(t, m, 2)
	m.Update(click(ps[1].x+10, ps[1].y+2, tea.MouseLeft)) // row 2 of the right pane
	if got := m.layout().focus; got != 1 {
		t.Fatalf("focus %d after the press, want 1", got)
	}
	// The pointer is 10 cells into the pane: a 6-cell gutter, so text column 4.
	if got := f.docs[0].cursor; got != (buffer.Pos{Line: 12, Col: 4}) {
		t.Errorf("caret %+v after the press, want line 12, the line under the pointer", got)
	}
	if got := panesOf(t, m, 2)[1].top; got != 10 {
		t.Errorf("right pane top %d after the press, want 10, the view it was pressed in", got)
	}
}
