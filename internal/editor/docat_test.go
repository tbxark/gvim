package editor

import "testing"

// TestDocAtReturnsEachTabWithoutMovingTheActiveTab checks the tab accessor that a
// split pane uses for a tab that is not active.
func TestDocAtReturnsEachTabWithoutMovingTheActiveTab(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a, b := writeFile(t, dir, "a.txt"), writeFile(t, dir, "b.txt")
	e := openAll(t, a, b)
	active := e.Active()
	if got := e.DocAt(0); got == nil || got.Path != a {
		t.Fatalf("DocAt(0) = %v, want the document of %s", got, a)
	}
	if got := e.DocAt(1); got == nil || got.Path != b {
		t.Fatalf("DocAt(1) = %v, want the document of %s", got, b)
	}
	if e.Active() != active {
		t.Fatal("DocAt changed the active tab")
	}
	for _, i := range []int{-1, 2} {
		if got := e.DocAt(i); got != nil {
			t.Fatalf("DocAt(%d) = %v, want nil", i, got)
		}
	}
}
