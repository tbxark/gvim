package editor

import (
	"slices"
	"testing"
)

// TestSetRecentTabIndicesRestoresOrder checks session restore's setter: the named tabs
// become the MRU history, invalid indices and repeats are ignored, and tabs that are
// not named fall back to tab order. The active tab still leads RecentTabIndices
// because that is the accessor's contract.
func TestSetRecentTabIndicesRestoresOrder(t *testing.T) {
	t.Parallel()
	e := withDabbrevDocs(0, "a", "b", "c")
	e.switchTo(1)
	e.switchTo(2)
	e.switchTo(0)
	if got, want := e.RecentTabIndices(), []int{0, 2, 1}; !slices.Equal(got, want) {
		t.Fatalf("before restore RecentTabIndices = %v, want %v", got, want)
	}

	// Saved MRU: A, C, B (active A). The saved order is applied verbatim.
	e.SetRecentTabIndices([]int{0, 2, 1})
	if got, want := e.RecentTabIndices(), []int{0, 2, 1}; !slices.Equal(got, want) {
		t.Fatalf("restored RecentTabIndices = %v, want %v", got, want)
	}

	// Out-of-range and repeated indices are ignored; an unnamed tab follows tab order.
	e.SetRecentTabIndices([]int{2, 2, 9, -1, 0})
	if got, want := e.RecentTabIndices(), []int{0, 2, 1}; !slices.Equal(got, want) {
		t.Fatalf("sanitized RecentTabIndices = %v, want %v", got, want)
	}

	// An empty order clears the history; the accessor then falls back to tab order.
	e.SetRecentTabIndices(nil)
	e.switchTo(0)
	if got, want := e.RecentTabIndices(), []int{0, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("cleared RecentTabIndices = %v, want tab order %v", got, want)
	}
}
