package ui

import (
	"maps"
	"testing"
)

// TestMenuRowUnderlinesOnlyAcceleratorsInTheFrame paints the root frame at 100
// columns and checks row 0. Only the accelerator letters of the menu labels may
// be underlined, and the blank cell at column 0 must not be.
func TestMenuRowUnderlinesOnlyAcceleratorsInTheFrame(t *testing.T) {
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 100, 30)
	g := m.paint()
	got := map[[2]int]string{}
	for x := range g.Width() {
		if c := g.At(x, 0); c.Style.Underline {
			got[[2]int{x, 0}] = c.Text
		}
	}
	want := map[[2]int]string{
		{1, 0}: "F", {8, 0}: "E", {15, 0}: "V", {22, 0}: "S",
		{34, 0}: "G", {39, 0}: "L", {45, 0}: "H",
	}
	if !maps.Equal(got, want) {
		t.Fatalf("row 0 underlined cells = %v, want %v (row = %q)", got, want, g.Plain()[0])
	}
}
