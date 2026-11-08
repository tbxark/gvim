package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/internal/tui/editorpane"
	"github.com/tbxark/gvim/theme"
)

// fakeVirtualLinesEditor is the fake editor with the vertical virtual space port: it
// records the virtual line a click asked for.
type fakeVirtualLinesEditor struct {
	*fakeEditor
	lines, col int
	took       bool
}

func (v *fakeVirtualLinesEditor) SetVirtualLines(lines, col int) bool {
	v.lines, v.col, v.took = lines, col, true
	return true
}

func TestClickBelowTheLastLineParksOnAVirtualLine(t *testing.T) {
	ed := &fakeVirtualLinesEditor{fakeEditor: newFakeEditor("ab")}
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.view.lineWrap = false // vertical virtual space needs unwrapped lines
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p := m.layout().FocusedPane()
	gutter := editorpane.GutterWidth(1)
	x, y := p.X+gutter+3, p.Y+2
	if _, cmd := m.Update(click(x, y, tea.MouseLeft)); cmd != nil {
		// A press may start commands, but it must not be an error; nothing to run.
		_ = cmd
	}
	if !ed.took {
		t.Fatalf("SetVirtualLines was not called for a press below the last line (pane %+v)", p)
	}
	if ed.lines != 2 || ed.col != 3 {
		t.Fatalf("parked on %d lines at column %d, want 2 and 3", ed.lines, ed.col)
	}
	if !m.drag.active {
		t.Fatal("the press did not start the drag")
	}
}

func TestClickBelowWithoutThePortKeepsTheClampedCaret(t *testing.T) {
	ed := newFakeEditor("ab")
	m := New(Deps{Editor: ed, Explorer: newFakeTree(3), Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock})
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	p := m.layout().FocusedPane()
	gutter := editorpane.GutterWidth(1)
	m.Update(click(p.X+gutter+3, p.Y+2, tea.MouseLeft))
	// The fake editor has no virtual line port: the caret goes to the buffer end,
	// as it did before the feature.
	if got := ed.doc.cursor; got.Line != 0 || got.Col != 2 {
		t.Fatalf("caret = %+v, want the buffer end", got)
	}
}
