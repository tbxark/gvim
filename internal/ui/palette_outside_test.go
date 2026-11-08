package ui

import "testing"

// TestPressOutsideThePaletteReachesTheEditor: the palette is a card over the bottom of
// the frame, and it takes only the presses on that card. A press above it reaches the
// editor and moves the caret, and the palette stays open, as Fresh's prompt layer lets
// a press go through to what is under it.
func TestPressOutsideThePaletteReachesTheEditor(t *testing.T) {
	m, ed := viewModel(t, DefaultViewOptions(), "hello world\nsecond line\nthird\n", 80, 24)
	m.openPalette(">")
	if _, ok := m.overlays[len(m.overlays)-1].(paletteOverlay); !ok {
		t.Fatal("setup: the palette is not the top overlay")
	}
	before := ed.Active().Cursor
	mouseDownAt(m, 12, 3)
	mouseUpAt(m, 12, 3)
	if got := ed.Active().Cursor; got == before {
		t.Errorf("the caret stayed at %v after a press in the editor while the palette was open", got)
	}
	if _, ok := m.overlays[len(m.overlays)-1].(paletteOverlay); !ok {
		t.Error("the palette closed on a press outside it")
	}
}
