package decorations

import (
	"errors"
	"slices"
	"testing"
	"time"
)

// itemsOf returns the items of (owner, ns) on the document, in store order.
func itemsOf(s *Store, doc uint64, owner, ns string) []Item {
	d := s.docs[doc]
	if d == nil {
		return nil
	}
	g := d.group(owner, ns)
	if g == nil {
		return nil
	}
	g.compact()
	out := make([]Item, len(g.items))
	for i, e := range g.items {
		out[i] = e.Item
	}
	return out
}

func hl(fl, fc, tl, tc int) Item {
	return Item{Kind: Highlight, FromLine: fl, FromCol: fc, ToLine: tl, ToCol: tc}
}

func lineItem(k Kind, line int) Item { return Item{Kind: k, FromLine: line} }

func rep(line, col int) Item { return Item{Kind: Replace, FromLine: line, FromCol: col, Text: "x"} }

func ins(line, col int, text string) Edit {
	return Edit{FromLine: line, FromCol: col, ToLine: line, ToCol: col, Text: text}
}

func del(fl, fc, tl, tc int) Edit { return Edit{FromLine: fl, FromCol: fc, ToLine: tl, ToCol: tc} }

func TestApplyAnchors(t *testing.T) {
	tests := []struct {
		name  string
		item  Item
		edits []Edit
		want  []Item // nil: the item is removed
	}{
		// ---- highlight: insert before, at the start, inside, at the end, after
		{"insert before on the same line", hl(2, 4, 2, 8), []Edit{ins(2, 1, "xyz")}, []Item{hl(2, 7, 2, 11)}},
		{"insert at the start stays out", hl(2, 4, 2, 8), []Edit{ins(2, 4, "xyz")}, []Item{hl(2, 7, 2, 11)}},
		{"insert inside grows", hl(2, 4, 2, 8), []Edit{ins(2, 6, "xyz")}, []Item{hl(2, 4, 2, 11)}},
		{"insert at the end stays out", hl(2, 4, 2, 8), []Edit{ins(2, 8, "xyz")}, []Item{hl(2, 4, 2, 8)}},
		{"insert after", hl(2, 4, 2, 8), []Edit{ins(2, 9, "xyz")}, []Item{hl(2, 4, 2, 8)}},
		{"insert on an earlier line", hl(2, 4, 2, 8), []Edit{ins(0, 3, "a\nb\n")}, []Item{hl(4, 4, 4, 8)}},
		{"insert on a later line", hl(2, 4, 2, 8), []Edit{ins(3, 0, "a\nb\n")}, []Item{hl(2, 4, 2, 8)}},
		{"newline before on the same line", hl(2, 4, 2, 8), []Edit{ins(2, 1, "ab\ncd")}, []Item{hl(3, 5, 3, 9)}},
		{"newline inside splits the range over two lines", hl(2, 4, 2, 8), []Edit{ins(2, 6, "\n")}, []Item{hl(2, 4, 3, 2)}},
		{"multi-line range grows by a line", hl(1, 3, 4, 2), []Edit{ins(2, 0, "\n")}, []Item{hl(1, 3, 5, 2)}},
		// ---- highlight: delete
		{"delete before on the same line", hl(2, 4, 2, 8), []Edit{del(2, 0, 2, 2)}, []Item{hl(2, 2, 2, 6)}},
		{"delete inside shrinks", hl(2, 4, 2, 8), []Edit{del(2, 5, 2, 7)}, []Item{hl(2, 4, 2, 6)}},
		{"delete overlapping the start", hl(2, 4, 2, 8), []Edit{del(2, 2, 2, 6)}, []Item{hl(2, 2, 2, 4)}},
		{"delete overlapping the end", hl(2, 4, 2, 8), []Edit{del(2, 6, 2, 10)}, []Item{hl(2, 4, 2, 6)}},
		{"delete all of it removes it", hl(2, 4, 2, 8), []Edit{del(2, 4, 2, 8)}, nil},
		{"delete more than it removes it", hl(2, 4, 2, 8), []Edit{del(2, 2, 2, 9)}, nil},
		{"delete a line above", hl(3, 4, 3, 8), []Edit{del(1, 0, 2, 0)}, []Item{hl(2, 4, 2, 8)}},
		{"join the line above", hl(3, 4, 3, 8), []Edit{del(2, 5, 3, 0)}, []Item{hl(2, 9, 2, 13)}},
		{"delete spanning lines through the end", hl(2, 4, 4, 2), []Edit{del(3, 0, 5, 1)}, []Item{hl(2, 4, 3, 0)}},
		{"delete spanning the start of a multi-line range", hl(2, 4, 4, 2), []Edit{del(1, 1, 3, 1)}, []Item{hl(1, 1, 2, 2)}},
		// ---- replace (a delete and an insert at its start)
		{"replace the text inside removes the range", hl(2, 4, 2, 8), []Edit{del(2, 5, 2, 6), ins(2, 5, "zz")}, []Item{hl(2, 4, 2, 9)}},
		{"replace exactly the range removes it", hl(2, 4, 2, 8), []Edit{del(2, 4, 2, 8), ins(2, 4, "zzzz")}, nil},
		// ---- undo of an insert is a delete of it
		{"undo restores the columns", hl(2, 4, 2, 8), []Edit{ins(2, 0, "ab"), del(2, 0, 2, 2)}, []Item{hl(2, 4, 2, 8)}},
		// ---- replace item
		{"replace item follows an insert before", rep(1, 5), []Edit{ins(1, 2, "ab")}, []Item{rep(1, 7)}},
		{"replace item moves with an insert at it", rep(1, 5), []Edit{ins(1, 5, "ab")}, []Item{rep(1, 7)}},
		{"replace item ignores an insert after", rep(1, 5), []Edit{ins(1, 6, "ab")}, []Item{rep(1, 5)}},
		{"replace item goes with a line break", rep(1, 5), []Edit{ins(1, 2, "\n")}, []Item{rep(2, 3)}},
		{"replace item dies with its character", rep(1, 5), []Edit{del(1, 4, 1, 6)}, nil},
		{"replace item survives a delete just before it", rep(1, 5), []Edit{del(1, 3, 1, 5)}, []Item{rep(1, 3)}},
		{"replace item survives a delete just after it", rep(1, 5), []Edit{del(1, 6, 1, 9)}, []Item{rep(1, 5)}},
		// ---- items that sit on a line
		{"eol after an insert above", lineItem(EOL, 3), []Edit{ins(1, 0, "a\nb\n")}, []Item{lineItem(EOL, 5)}},
		{"eol on the edited line", lineItem(EOL, 3), []Edit{ins(3, 4, "abc")}, []Item{lineItem(EOL, 3)}},
		{"eol keeps its line when the line splits after its start", lineItem(EOL, 3), []Edit{ins(3, 2, "\n")}, []Item{lineItem(EOL, 3)}},
		{"gutter moves with the text when a line is opened above it", lineItem(Gutter, 3), []Edit{ins(3, 0, "\n")}, []Item{lineItem(Gutter, 4)}},
		{"gutter stays when text is typed at the line start", lineItem(Gutter, 3), []Edit{ins(3, 0, "ab")}, []Item{lineItem(Gutter, 3)}},
		{"line item below the edit is untouched", lineItem(Line, 3), []Edit{ins(5, 0, "\n")}, []Item{lineItem(Line, 3)}},
		{"delete a whole line removes its item", lineItem(Gutter, 3), []Edit{del(3, 0, 4, 0)}, nil},
		{"delete a line above moves it up", lineItem(Gutter, 3), []Edit{del(2, 0, 3, 0)}, []Item{lineItem(Gutter, 2)}},
		{"delete the text of a line keeps it", lineItem(Gutter, 3), []Edit{del(3, 0, 3, 9)}, []Item{lineItem(Gutter, 3)}},
		{"join with the next line keeps the first", lineItem(Gutter, 3), []Edit{del(3, 2, 4, 0)}, []Item{lineItem(Gutter, 3)}},
		{"join into the previous line moves it up", lineItem(Gutter, 3), []Edit{del(2, 4, 3, 0)}, []Item{lineItem(Gutter, 2)}},
		{"delete across several lines removes the ones inside", lineItem(Gutter, 3), []Edit{del(2, 4, 5, 1)}, nil},
		{"scroll line follows like a gutter", lineItem(Scroll, 4), []Edit{ins(0, 0, "\n\n")}, []Item{lineItem(Scroll, 6)}},
		// ---- scroll range
		{"scroll range grows with an insert inside", Item{Kind: Scroll, FromLine: 2, ToLine: 5}, []Edit{ins(3, 0, "\n")}, []Item{{Kind: Scroll, FromLine: 2, ToLine: 6}}},
		{"scroll range shrinks and dies", Item{Kind: Scroll, FromLine: 2, ToLine: 5}, []Edit{del(2, 0, 5, 0)}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New()
			if err := s.Set(1, "p", "ns", []Item{tc.item}); err != nil {
				t.Fatal(err)
			}
			s.Apply(1, tc.edits, false)
			got := itemsOf(s, 1, "p", "ns")
			want := normalizedAll(tc.want)
			if !slices.Equal(got, want) {
				t.Fatalf("items = %+v, want %+v", got, want)
			}
			if wantN := len(want); s.OwnerCount("p") != wantN {
				t.Fatalf("OwnerCount = %d, want %d", s.OwnerCount("p"), wantN)
			}
			if len(want) == 0 && !s.Empty() {
				t.Fatalf("the store keeps an empty document")
			}
		})
	}
}

func normalizedAll(items []Item) []Item {
	var out []Item
	for _, it := range items {
		if n, ok := normalize(it); ok {
			out = append(out, n)
		}
	}
	return out
}

func TestApplyCoarseDropsTheDocument(t *testing.T) {
	s := New()
	_ = s.Set(1, "p", "a", []Item{hl(0, 0, 0, 3), lineItem(Gutter, 1)})
	_ = s.Set(1, "q", "b", []Item{lineItem(EOL, 2)})
	_ = s.Set(2, "p", "a", []Item{lineItem(EOL, 2)})
	s.Apply(1, nil, true)
	if s.Count(1) != 0 || s.Count(2) != 1 {
		t.Fatalf("counts = %d, %d; want 0, 1", s.Count(1), s.Count(2))
	}
	if s.OwnerCount("p") != 1 || s.OwnerCount("q") != 0 {
		t.Fatalf("owner counts = %d, %d; want 1, 0", s.OwnerCount("p"), s.OwnerCount("q"))
	}
	// An edit of a document with no decoration, or no edits, is a no-op.
	rev := s.Rev()
	s.Apply(9, []Edit{ins(0, 0, "x")}, false)
	s.Apply(2, nil, false)
	if s.Rev() != rev {
		t.Fatalf("Rev moved without a change")
	}
}

func TestApplyOrderOfEditsIsSequential(t *testing.T) {
	// The second edit is in the coordinates after the first.
	s := New()
	_ = s.Set(1, "p", "a", []Item{hl(0, 10, 0, 14)})
	s.Apply(1, []Edit{ins(0, 0, "\n"), del(1, 0, 1, 2)}, false)
	want := []Item{hl(1, 8, 1, 12)}
	if got := itemsOf(s, 1, "p", "a"); !slices.Equal(got, want) {
		t.Fatalf("items = %+v, want %+v", got, want)
	}
}

func TestSetNormalizes(t *testing.T) {
	s := New()
	err := s.Set(1, "p", "a", []Item{
		hl(2, 5, 2, 5),   // empty: dropped
		hl(3, 9, 1, 2),   // reversed: turned round
		hl(-4, -1, 0, 3), // negative: the start of the document
		{Kind: EOL, FromLine: 4, FromCol: 7, ToLine: 9, ToCol: 9, Text: "t"},
		{Kind: Scroll, FromLine: 6, FromCol: 3, ToLine: 6, ToCol: 3},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Item{
		hl(1, 2, 3, 9),
		hl(0, 0, 0, 3),
		{Kind: EOL, FromLine: 4, ToLine: 4, Text: "t"},
		{Kind: Scroll, FromLine: 6, ToLine: 6},
	}
	if got := itemsOf(s, 1, "p", "a"); !slices.Equal(got, want) {
		t.Fatalf("items = %+v, want %+v", got, want)
	}
	// All items empty: the document is not kept.
	_ = s.Set(2, "p", "a", []Item{hl(1, 1, 1, 1)})
	if s.Count(2) != 0 {
		t.Fatalf("Count(2) = %d, want 0", s.Count(2))
	}
}

func TestSetReplacesAndClears(t *testing.T) {
	s := New()
	_ = s.Set(1, "p", "a", []Item{lineItem(Gutter, 1), lineItem(Gutter, 2)})
	_ = s.Set(1, "p", "b", []Item{lineItem(EOL, 1)})
	_ = s.Set(1, "p", "a", []Item{lineItem(Gutter, 7)})
	if got := itemsOf(s, 1, "p", "a"); len(got) != 1 || got[0].FromLine != 7 {
		t.Fatalf("a = %+v", got)
	}
	if s.OwnerCount("p") != 2 {
		t.Fatalf("OwnerCount = %d, want 2", s.OwnerCount("p"))
	}
	_ = s.Set(1, "p", "a", nil)
	if got := itemsOf(s, 1, "p", "a"); got != nil || s.Count(1) != 1 {
		t.Fatalf("a = %+v, count %d", got, s.Count(1))
	}
	_ = s.Set(2, "p", "b", []Item{lineItem(EOL, 3)})
	s.Clear(0, "p", "b") // every document
	if !s.Empty() || s.OwnerCount("p") != 0 {
		t.Fatalf("Clear(0) left %d docs, %d items", len(s.docs), s.OwnerCount("p"))
	}
}

func TestAddRemoveIDs(t *testing.T) {
	s := New()
	a, _ := s.Add(1, "p", "ns", lineItem(Gutter, 4))
	b, _ := s.Add(1, "p", "ns", lineItem(Gutter, 2))
	if a == 0 || b == 0 || a == b {
		t.Fatalf("ids = %d, %d", a, b)
	}
	if id, err := s.Add(1, "p", "ns", hl(1, 1, 1, 1)); id != 0 || err != nil {
		t.Fatalf("empty highlight = (%d, %v), want (0, nil)", id, err)
	}
	// An id of another namespace or owner removes nothing.
	if s.Remove(1, "q", "ns", a) || s.Remove(1, "p", "other", a) || s.Remove(2, "p", "ns", a) {
		t.Fatal("Remove found an item it should not")
	}
	if !s.Remove(1, "p", "ns", a) || s.Remove(1, "p", "ns", a) {
		t.Fatal("Remove(a) should succeed once")
	}
	if got := s.Line(1, 2).Gutter; got == nil {
		t.Fatal("the other item is gone")
	}
	if got := s.Line(1, 4).Gutter; got != nil {
		t.Fatal("the removed item still shows")
	}
	// ids survive edits and are not reused after Set.
	s.Apply(1, []Edit{ins(0, 0, "\n")}, false)
	if !s.Remove(1, "p", "ns", b) || !s.Empty() {
		t.Fatalf("Remove(b) after an edit failed; empty = %v", s.Empty())
	}
}

func TestDropOwnerAndDoc(t *testing.T) {
	s := New()
	_ = s.Set(1, "p", "a", []Item{lineItem(EOL, 1)})
	_ = s.Set(2, "p", "a", []Item{lineItem(EOL, 1)})
	_ = s.Set(2, "q", "a", []Item{lineItem(EOL, 1)})
	s.DropOwner("p")
	if s.Count(1) != 0 || s.Count(2) != 1 || s.OwnerCount("p") != 0 || s.OwnerCount("q") != 1 {
		t.Fatalf("after DropOwner: %d %d %d %d", s.Count(1), s.Count(2), s.OwnerCount("p"), s.OwnerCount("q"))
	}
	s.DropDoc(2)
	if !s.Empty() || s.OwnerCount("q") != 0 {
		t.Fatalf("after DropDoc: empty %v, q %d", s.Empty(), s.OwnerCount("q"))
	}
}

func TestLimits(t *testing.T) {
	big := func(n int) []Item {
		items := make([]Item, n)
		for i := range items {
			items[i] = lineItem(Gutter, i)
		}
		return items
	}
	s := New()
	if err := s.Set(1, "p", "a", big(MaxPerNamespace)); err != nil {
		t.Fatalf("a full namespace is allowed: %v", err)
	}
	if err := s.Set(1, "p", "a", big(MaxPerNamespace+1)); !errors.Is(err, ErrFull) {
		t.Fatalf("one over the namespace cap: err = %v", err)
	}
	if s.Count(1) != MaxPerNamespace {
		t.Fatalf("a refused Set changed the store: count %d", s.Count(1))
	}
	if _, err := s.Add(1, "p", "a", lineItem(Gutter, 0)); !errors.Is(err, ErrFull) {
		t.Fatalf("Add past the namespace cap: err = %v", err)
	}
	// The owner cap spans namespaces and documents.
	if err := s.Set(2, "p", "b", big(MaxPerNamespace)); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(3, "p", "c", big(MaxPerOwner-2*MaxPerNamespace)); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(3, "p", "d", big(1)); !errors.Is(err, ErrFull) {
		t.Fatalf("past the owner cap: err = %v", err)
	}
	if _, err := s.Add(3, "p", "c", lineItem(Gutter, 0)); !errors.Is(err, ErrFull) {
		t.Fatalf("Add past the owner cap: err = %v", err)
	}
	if err := s.Set(1, "q", "a", big(10)); err != nil {
		t.Fatalf("another owner has its own cap: %v", err)
	}
	// Replacing a namespace frees its share.
	if err := s.Set(3, "p", "c", big(5)); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(3, "p", "d", big(5)); err != nil {
		t.Fatalf("the share of the replaced namespace is free: %v", err)
	}
}

// TestRemoveInterleaved removes items one by one between the other operations of the
// store: what is left must be exactly the items that were not removed, wherever they sit.
func TestRemoveInterleaved(t *testing.T) {
	const n = 200
	s := New()
	ids := make([]int, n)
	for i := range n {
		// Added in reverse line order, so the store has to sort them.
		id, err := s.Add(1, "p", "ns", lineItem(Gutter, n-i))
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	live := map[int]bool{} // lines that still have a mark
	for i := range n {
		live[n-i] = true
	}
	check := func(when string) {
		t.Helper()
		if got := s.Count(1); got != len(live) {
			t.Fatalf("%s: Count = %d, want %d", when, got, len(live))
		}
		if got := s.OwnerCount("p"); got != len(live) {
			t.Fatalf("%s: OwnerCount = %d, want %d", when, got, len(live))
		}
		for line := 1; line <= n; line++ {
			if has := s.Line(1, line).Gutter != nil; has != live[line] {
				t.Fatalf("%s: line %d has mark %v, want %v", when, line, has, live[line])
			}
		}
	}
	for i := 0; i < n; i += 3 { // every third, scattered
		if !s.Remove(1, "p", "ns", ids[i]) {
			t.Fatalf("Remove(%d) failed", ids[i])
		}
		delete(live, n-i)
		if s.Remove(1, "p", "ns", ids[i]) {
			t.Fatalf("Remove(%d) succeeded twice", ids[i])
		}
	}
	check("after removing")
	// An insert at the top moves everything down one line.
	s.Apply(1, []Edit{ins(0, 0, "\n")}, false)
	moved := map[int]bool{}
	for line := range live {
		moved[line+1] = true
	}
	live = moved
	check("after an edit")
	// Add after removing, and remove an item added after the index was built.
	id, err := s.Add(1, "p", "ns", lineItem(Gutter, 0))
	if err != nil {
		t.Fatal(err)
	}
	live[0] = true
	check("after an add")
	if !s.Remove(1, "p", "ns", id) {
		t.Fatal("Remove of a new item failed")
	}
	delete(live, 0)
	check("after removing the new item")
	// Removing most of the rest compacts on the way; the survivors stay right.
	for i := 1; i < n; i++ {
		if i%3 != 0 {
			s.Remove(1, "p", "ns", ids[i])
			delete(live, n-i+1)
		}
	}
	check("after removing most")
	if got := len(s.Items(1, "p", "ns")); got != len(live) {
		t.Fatalf("Items = %d, want %d", got, len(live))
	}
	for i := range n {
		s.Remove(1, "p", "ns", ids[i])
	}
	if !s.Empty() || s.OwnerCount("p") != 0 {
		t.Fatalf("empty = %v, owner count %d after removing everything", s.Empty(), s.OwnerCount("p"))
	}
}

// TestRemoveManyIsFast: a plugin that adds up to the cap and removes the items one by one
// must not be quadratic (the removal loop of 20000 items took 0.58 s with a shifting
// delete, which is past the command time budget).
func TestRemoveManyIsFast(t *testing.T) {
	s := New()
	ids := make([]int, MaxPerNamespace)
	for i := range ids {
		id, err := s.Add(1, "p", "ns", Item{Kind: Highlight, FromLine: i, FromCol: 1, ToLine: i, ToCol: 4})
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	unit := cpuUnit()
	start := time.Now()
	for _, id := range ids {
		if !s.Remove(1, "p", "ns", id) {
			t.Fatalf("Remove(%d) failed", id)
		}
	}
	// A shifting delete took 0.58 s here, about 300 units of cpuUnit; the loop takes a
	// handful. The limit is in units so that it holds on a busy machine too.
	if took := time.Since(start); took > 60*unit {
		t.Fatalf("removing %d items one by one took %v, over 60 units of %v", len(ids), took, unit)
	}
	if !s.Empty() || s.OwnerCount("p") != 0 {
		t.Fatal("the store is not empty")
	}
}
