package decorations

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"
)

// randItems makes items at random places of the text of m.
func randItems(r *rand.Rand, m mirror, n int) []Item {
	var set []Item
	for range n {
		bounds := runeBoundaries(m.text)
		pick := func() (int, int) { return m.posOf(bounds[r.IntN(len(bounds))]) }
		var it Item
		switch r.IntN(5) {
		case 0, 1:
			fl, fc := pick()
			tl, tc := pick()
			it = Item{Kind: Highlight, FromLine: fl, FromCol: fc, ToLine: tl, ToCol: tc}
		case 2:
			fl, fc := pick()
			it = Item{Kind: Replace, FromLine: fl, FromCol: fc, Text: "r"}
		case 3:
			fl, _ := pick()
			it = Item{Kind: []Kind{Line, EOL, Gutter}[r.IntN(3)], FromLine: fl}
		default:
			fl, _ := pick()
			tl, _ := pick()
			it = Item{Kind: Scroll, FromLine: fl, ToLine: tl}
		}
		if n, ok := normalize(it); ok {
			set = append(set, n)
		}
	}
	return set
}

// randEdits makes a sequence of edits of the text of m, each in the coordinates after the
// ones before it. With down set every edit starts at or after the end of the text the one
// before inserted, so the sequence goes down the document.
func randEdits(r *rand.Rand, m mirror, count int, down bool) []Edit {
	var out []Edit
	floor := 0
	for range count {
		bounds := runeBoundaries(m.text)
		i := 0
		if down {
			i, _ = slices.BinarySearch(bounds, floor)
		}
		if i >= len(bounds) {
			break
		}
		a := bounds[i+r.IntN(min(len(bounds)-i, 1+r.IntN(8)))]
		b := a
		if r.IntN(2) == 0 {
			b = a + r.IntN(len(m.text)-a+1)
			for !slices.Contains(bounds, b) {
				b--
			}
		}
		var text string
		if r.IntN(4) > 0 {
			text = randText(r, r.IntN(5))
		}
		fl, fc := m.posOf(a)
		tl, tc := m.posOf(b)
		out = append(out, Edit{FromLine: fl, FromCol: fc, ToLine: tl, ToCol: tc, Text: text})
		m.text = m.text[:a] + text + m.text[b:]
		floor = a + len(text)
		if len(m.text) == 0 {
			break
		}
	}
	return out
}

func sortedItems(s *Store, doc uint64) []string {
	var out []string
	for _, it := range s.Items(doc, "p", "a") {
		out = append(out, fmt.Sprintf("%+v", it))
	}
	slices.Sort(out)
	return out
}

// TestBatchMatchesOneEditAtATime checks a batch of edits, applied together, against the same
// edits applied one at a time, for sequences that go down the document (one pass over the
// items) and for sequences that do not (an edit at a time).
func TestBatchMatchesOneEditAtATime(t *testing.T) {
	r := rand.New(rand.NewPCG(21, 4))
	composedRounds := 0
	for round := range 1500 {
		m := mirror{text: randText(r, 8+r.IntN(80))}
		set := randItems(r, m, 20)
		batch, single := New(), New()
		for _, s := range []*Store{batch, single} {
			if err := s.Set(1, "p", "a", set); err != nil {
				t.Fatal(err)
			}
		}
		down := round%3 != 0
		edits := randEdits(r, m, 2+r.IntN(12), down)
		if len(edits) == 0 {
			continue
		}
		shapes := make([]shape, 0, len(edits))
		for _, e := range edits {
			shapes = append(shapes, shapeOf(e))
		}
		if _, ok := compose(shapes); ok {
			composedRounds++
		}
		batch.Apply(1, edits, false)
		for _, e := range edits {
			single.Apply(1, []Edit{e}, false)
		}
		if got, want := sortedItems(batch, 1), sortedItems(single, 1); !slices.Equal(got, want) {
			t.Fatalf("round %d (down %v) edits %+v:\n got %s\nwant %s", round, down, edits, strings.Join(got, "\n     "), strings.Join(want, "\n     "))
		}
		if got, want := batch.OwnerCount("p"), single.OwnerCount("p"); got != want {
			t.Fatalf("round %d: owner count %d, want %d", round, got, want)
		}
	}
	if composedRounds < 200 {
		t.Fatalf("only %d rounds took the one-pass path", composedRounds)
	}
}

// TestQueuedEditsApplyBeforeAnyRead checks that every reader sees the anchors moved by the
// edits that were queued, and that the owner count follows the items they removed.
func TestQueuedEditsApplyBeforeAnyRead(t *testing.T) {
	edits := []Edit{
		{FromLine: 0, FromCol: 0, ToLine: 0, ToCol: 0, Text: "x\n"}, // a line above everything
		{FromLine: 2, FromCol: 0, ToLine: 2, ToCol: 0, Text: "y\n"}, // and one above line 1
		{FromLine: 5, FromCol: 0, ToLine: 6, ToCol: 0},              // delete a line: its gutter goes
	}
	items := []Item{{Kind: Gutter, FromLine: 0, Text: "a"}, {Kind: Gutter, FromLine: 3, Text: "b"}, {Kind: Gutter, FromLine: 4, Text: "c"}}
	reads := map[string]func(*Store){
		"Count":      func(s *Store) { s.Count(1) },
		"Items":      func(s *Store) { s.Items(1, "p", "a") },
		"Line":       func(s *Store) { s.Line(1, 0) },
		"OwnerCount": func(s *Store) { s.OwnerCount("p") },
		"Empty":      func(s *Store) { s.Empty() },
		"Docs":       func(s *Store) { s.Docs() },
	}
	for name, read := range reads {
		t.Run(name, func(t *testing.T) {
			s := New()
			if err := s.Set(1, "p", "a", items); err != nil {
				t.Fatal(err)
			}
			rev := s.Rev()
			for _, e := range edits {
				s.Queue(1, e)
			}
			if s.Rev() == rev {
				t.Fatal("Queue did not change Rev")
			}
			if d := s.docs[1]; len(d.pend) != len(edits) {
				t.Fatalf("%d edits queued, want %d", len(d.pend), len(edits))
			}
			read(s)
			if d := s.docs[1]; len(d.pend) != 0 {
				t.Fatalf("%d edits still queued after %s", len(d.pend), name)
			}
			var lines []int
			for _, it := range s.Items(1, "p", "a") {
				lines = append(lines, it.FromLine)
			}
			// a: 0 -> 1 -> 1 -> 1 (the edit at line 2 is below it); b: 3 -> 4 -> 5 -> deleted with line 5; c: 4 -> 5 -> 6 -> 5
			if want := []int{1, 5}; !slices.Equal(lines, want) {
				t.Fatalf("gutter lines = %v, want %v", lines, want)
			}
			if got := s.OwnerCount("p"); got != 2 {
				t.Fatalf("owner count = %d, want 2", got)
			}
		})
	}
}

// TestQueueFlushesAtTheCap checks that a document does not keep an unbounded queue.
func TestQueueFlushesAtTheCap(t *testing.T) {
	s := New()
	if err := s.Set(1, "p", "a", []Item{{Kind: Gutter, FromLine: 0, Text: "a"}}); err != nil {
		t.Fatal(err)
	}
	for range 3*maxQueued + 5 {
		s.Queue(1, Edit{Text: "\n"})
		if n := len(s.docs[1].pend); n >= maxQueued {
			t.Fatalf("%d edits queued", n)
		}
	}
	if got := s.Items(1, "p", "a")[0].FromLine; got != 3*maxQueued+5 {
		t.Fatalf("gutter on line %d, want %d", got, 3*maxQueued+5)
	}
}

// TestBulkEditsCostOnePass is the shape of Replace All with a line break in the replacement:
// thousands of edits going down a document that carries tens of thousands of items. One at a
// time that is the edits times the items (1.3 s for 5000 and 48 000); in one pass it is a few
// milliseconds. The bound is generous so a slow machine or the race detector passes.
func TestBulkEditsCostOnePass(t *testing.T) {
	s := New()
	const perNS, edits = 16_000, 5000
	for _, ns := range []string{"a", "b", "c"} {
		items := make([]Item, perNS)
		for i := range items {
			items[i] = Item{Kind: Gutter, FromLine: i * 3, Text: "+"}
		}
		if err := s.Set(1, "p", ns, items); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	for i := range edits {
		// every 10th line has a match; each replacement adds a line, and the lines above it have moved
		line := i*10 + i
		s.Queue(1, Edit{FromLine: line, FromCol: 0, ToLine: line, ToCol: 3, Text: "a\nb"})
	}
	n := s.Count(1)
	took := time.Since(start)
	if n != 3*perNS {
		t.Fatalf("%d items, want %d", n, 3*perNS)
	}
	if took > 600*time.Millisecond {
		t.Errorf("%d edits over %d items took %v", edits, 3*perNS, took)
	}
	// The last gutter was on line 3*(perNS-1); the edits at lines 10*i above it each added one.
	items := s.Items(1, "p", "a")
	last := 3 * (perNS - 1)
	if got, want := items[len(items)-1].FromLine, last+(last-1)/10+1; got != want {
		t.Errorf("last gutter on line %d, want %d", got, want)
	}
}
