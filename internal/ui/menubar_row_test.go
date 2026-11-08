package ui

import "testing"

// TestMenuRowHasNoUnderlinedMnemonics paints the root frame at 100 columns and checks that
// no cell of the menu bar row, nor of an open dropdown, is underlined: the menus have no
// mnemonics. It replaces a test that pinned an underlined letter in each label.
func TestMenuRowHasNoUnderlinedMnemonics(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 100, 30)
	check := func(when string) {
		t.Helper()
		g := m.paint()
		for y := range g.Height() {
			for x := range g.Width() {
				if c := g.At(x, y); c.Style.Underline && (y == 0 || m.menu.IsOpen()) {
					t.Fatalf("%s: cell (%d,%d) %q is underlined", when, x, y, c.Text)
				}
			}
		}
	}
	check("closed bar")
	if err := m.Run("menu_activate"); err != nil {
		t.Fatal(err)
	}
	if !m.menu.IsOpen() {
		t.Fatal("menu_activate did not open the menu")
	}
	check("open dropdown")
}
