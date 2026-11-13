package decorations

import "testing"

// A bulk edit that goes down the document moves a range-shaped Conceal like any
// range, whether the edits are applied one at a time or in one composed pass.
func TestConcealMovesThroughComposedEdits(t *testing.T) {
	t.Parallel()
	conceal := Item{Kind: Conceal, FromLine: 3, FromCol: 2, ToLine: 3, ToCol: 5}
	edits := []Edit{ins(0, 0, "a\n"), ins(1, 0, "b\n")}

	one := New()
	if err := one.Set(1, "p", "n", []Item{conceal}); err != nil {
		t.Fatal(err)
	}
	for _, e := range edits {
		one.Apply(1, []Edit{e}, false)
	}

	bulk := New()
	if err := bulk.Set(1, "p", "n", []Item{conceal}); err != nil {
		t.Fatal(err)
	}
	for _, e := range edits {
		bulk.Queue(1, e)
	}

	want := one.Items(1, "p", "n")
	got := bulk.Items(1, "p", "n")
	if len(want) != 1 || want[0].FromLine != 5 || want[0].FromCol != 2 || want[0].ToLine != 5 || want[0].ToCol != 5 {
		t.Fatalf("one at a time = %+v, want the conceal on line 5, columns 2..5", want)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("composed = %+v, want %+v", got, want)
	}
}

// ClearRange after Remove must not bring a removed item back, and must keep the
// counts right.
func TestClearRangeAfterRemoveKeepsRemovedItemsGone(t *testing.T) {
	t.Parallel()
	s := New()
	var ids []int
	for line := 0; line < 3; line++ {
		id, err := s.Add(1, "p", "n", lineItem(Gutter, line))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	if !s.Remove(1, "p", "n", ids[2]) {
		t.Fatal("Remove found no item")
	}
	s.ClearRange(1, "p", "n", 0, 0)
	if got := s.Count(1); got != 1 {
		t.Errorf("Count = %d, want 1 (line 1 only)", got)
	}
	if got := s.OwnerCount("p"); got != 1 {
		t.Errorf("OwnerCount = %d, want 1", got)
	}
	if g := s.Line(1, 2).Gutter; g != nil {
		t.Errorf("removed item on line 2 is back: %+v", g)
	}
}
