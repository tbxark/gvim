package decorations

import (
	"slices"
	"testing"
	"time"
)

func styled(k Kind, line int, prio int, text string) Item {
	return Item{Kind: k, FromLine: line, ToLine: line, Priority: prio, Text: text}
}

func TestLineSpansOfARange(t *testing.T) {
	s := New()
	_ = s.Set(1, "p", "a", []Item{
		hl(1, 3, 4, 2),
		hl(6, 0, 7, 0), // ends at the start of line 7: only line 6
		hl(9, 1, 9, 5),
	})
	tests := []struct {
		line int
		want []Span
	}{
		{0, nil},
		{1, []Span{{Start: 3, End: -1}}},
		{2, []Span{{Start: 0, End: -1}}},
		{3, []Span{{Start: 0, End: -1}}},
		{4, []Span{{Start: 0, End: 2}}},
		{5, nil},
		{6, []Span{{Start: 0, End: -1}}},
		{7, nil},
		{9, []Span{{Start: 1, End: 5}}},
		{10, nil},
		{-1, nil},
	}
	for _, tc := range tests {
		if got := s.Line(1, tc.line).Spans; !slices.Equal(got, tc.want) {
			t.Errorf("Line(%d).Spans = %+v, want %+v", tc.line, got, tc.want)
		}
	}
	if !s.Line(2, 1).IsZero() {
		t.Error("a document with no decoration has none")
	}
}

func TestLinePaintingOrder(t *testing.T) {
	s := New()
	// Two owners; added out of priority order.
	_ = s.Set(1, "p", "a", []Item{styled(EOL, 2, 5, "five"), styled(EOL, 2, 0, "zero-a")})
	_ = s.Set(1, "q", "b", []Item{styled(EOL, 2, 0, "zero-b"), styled(EOL, 2, -3, "neg")})
	got := s.Line(1, 2).EOL
	var texts []string
	for _, v := range got {
		texts = append(texts, v.Text)
	}
	// Lowest priority first; equal priorities in order of addition.
	if want := []string{"neg", "zero-a", "zero-b", "five"}; !slices.Equal(texts, want) {
		t.Fatalf("EOL order = %v, want %v", texts, want)
	}
}

func TestLineSingleSlotsTakeTheStrongest(t *testing.T) {
	s := New()
	_ = s.Set(1, "p", "a", []Item{styled(Gutter, 3, 1, "a"), styled(Gutter, 3, 4, "b"), styled(Gutter, 3, 4, "c"), styled(Gutter, 3, 2, "d")})
	if g := s.Line(1, 3).Gutter; g == nil || g.Text != "c" {
		t.Fatalf("gutter = %+v, want the later of the top priority", g)
	}
	_ = s.Set(1, "p", "s", []Item{
		{Kind: Scroll, FromLine: 2, ToLine: 6, Priority: 1, Color: Color{Key: "range"}},
		{Kind: Scroll, FromLine: 4, ToLine: 4, Priority: 2, Color: Color{Key: "line"}},
	})
	tests := []struct {
		line int
		want string
	}{{1, ""}, {2, "range"}, {4, "line"}, {5, "range"}, {6, ""}, {7, ""}}
	for _, tc := range tests {
		m := s.Line(1, tc.line).Scroll
		got := ""
		if m != nil {
			got = m.Color.Key
		}
		if got != tc.want {
			t.Errorf("Line(%d).Scroll = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestLineReplacesAndRows(t *testing.T) {
	s := New()
	_ = s.Set(1, "p", "a", []Item{
		{Kind: Replace, FromLine: 2, FromCol: 4, Text: "b", Priority: 1},
		{Kind: Replace, FromLine: 2, FromCol: 1, Text: "a"},
		{Kind: Line, FromLine: 2, Priority: 2},
		{Kind: Line, FromLine: 2, Priority: 1},
	})
	l := s.Line(1, 2)
	if len(l.Replaces) != 2 || l.Replaces[0].Text != "a" || l.Replaces[1].Col != 4 {
		t.Fatalf("Replaces = %+v", l.Replaces)
	}
	if len(l.Rows) != 2 || l.Rows[0].Priority != 1 || l.Rows[1].Priority != 2 {
		t.Fatalf("Rows = %+v", l.Rows)
	}
}

func TestLineCapKeepsTheStrongest(t *testing.T) {
	s := New()
	var items []Item
	for i := range MaxPerLine + 50 {
		items = append(items, styled(EOL, 0, i, ""))
	}
	_ = s.Set(1, "p", "a", items)
	got := s.Line(1, 0).EOL
	if len(got) != MaxPerLine || got[0].Priority != 50 || got[len(got)-1].Priority != MaxPerLine+49 {
		t.Fatalf("kept %d items from priority %d to %d", len(got), got[0].Priority, got[len(got)-1].Priority)
	}
}

func TestLineAcrossWindowsAndChanges(t *testing.T) {
	s := New()
	_ = s.Set(1, "p", "a", []Item{hl(10, 2, 200, 3), lineItem(Gutter, windowLines-1), lineItem(Gutter, windowLines)})
	for _, line := range []int{10, windowLines - 1, windowLines, 2*windowLines + 5, 199} {
		if len(s.Line(1, line).Spans) == 0 {
			t.Errorf("Line(%d) lost the range that crosses windows", line)
		}
	}
	if s.Line(1, windowLines-1).Gutter == nil || s.Line(1, windowLines).Gutter == nil {
		t.Error("the gutter marks at a window edge are missing")
	}
	if got := s.Line(1, 200).Spans; len(got) != 1 || got[0].End != 3 {
		t.Errorf("Line(200).Spans = %+v", got)
	}
	// A cached window is dropped when the document changes.
	s.Apply(1, []Edit{ins(0, 0, "\n")}, false)
	if len(s.Line(1, 10).Spans) != 0 || len(s.Line(1, 11).Spans) != 1 {
		t.Errorf("a stale window was served after an edit")
	}
	if s.Line(1, windowLines-1).Gutter != nil || s.Line(1, windowLines).Gutter == nil || s.Line(1, windowLines+1).Gutter == nil {
		t.Errorf("the gutter marks did not move down with the edit")
	}
	_ = s.Set(1, "p", "a", nil)
	if !s.Line(1, 50).IsZero() {
		t.Errorf("a cleared document still shows decorations")
	}
}

func TestLineFarFromTheStart(t *testing.T) {
	// Windowing: a query far down a huge document touches the items that reach it only.
	s := New()
	var items []Item
	for i := range MaxPerNamespace - 2 {
		items = append(items, lineItem(Gutter, i))
	}
	items = append(items, hl(10, 0, 9_000_000, 1), lineItem(EOL, 8_000_000))
	if err := s.Set(1, "p", "a", items); err != nil {
		t.Fatal(err)
	}
	l := s.Line(1, 8_000_000)
	if len(l.Spans) != 1 || len(l.EOL) != 1 || l.Gutter != nil {
		t.Fatalf("Line(8e6) = %+v", l)
	}
	if got := s.Line(1, 9_000_001); !got.IsZero() {
		t.Fatalf("Line past the range = %+v", got)
	}
	d := s.docs[1]
	if len(d.windows) > maxWindows {
		t.Fatalf("%d windows kept, cap %d", len(d.windows), maxWindows)
	}
	for i := range 3 * maxWindows {
		s.Line(1, i*windowLines)
	}
	if len(d.windows) > maxWindows {
		t.Fatalf("%d windows kept after scrolling, cap %d", len(d.windows), maxWindows)
	}
}

func BenchmarkLineWindowBuild(b *testing.B) {
	s := New()
	var items []Item
	for i := range MaxPerNamespace {
		items = append(items, Item{Kind: Highlight, FromLine: i, FromCol: 1, ToLine: i, ToCol: 4})
	}
	_ = s.Set(1, "p", "a", items)
	for i := 0; b.Loop(); i++ {
		s.docs[1].windows = nil
		s.Line(1, 10_000+i%windowLines)
	}
}

// TestLineWindowKeepsTheTopItems checks the per-line cap against a brute-force answer: of
// more than MaxPerLine items on a line, the ones painted on top are kept, in painting order.
func TestLineWindowKeepsTheTopItems(t *testing.T) {
	const n = 3*MaxPerLine + 17
	s := New()
	var spans, rows, eols, reps []Item
	for i := range n {
		prio := (i * 7) % 11
		// FromCol is the identity of the item.
		spans = append(spans, Item{Kind: Highlight, FromLine: 0, FromCol: i, ToLine: 5, ToCol: 1, Priority: prio})
		rows = append(rows, Item{Kind: Line, FromLine: 2, Priority: prio, Style: Style{FG: Color{Key: string(rune('a' + i%26))}}})
		eols = append(eols, Item{Kind: EOL, FromLine: 2, Priority: prio, Text: string(rune(0x100 + i))})
		reps = append(reps, Item{Kind: Replace, FromLine: 2, FromCol: i, Priority: prio, Text: "x"})
	}
	// Two namespaces, so the sequence numbers interleave across groups.
	if err := s.Set(1, "p", "a", spans[:n/2]); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(1, "q", "b", spans[n/2:]); err != nil {
		t.Fatal(err)
	}
	for _, it := range slices.Concat(rows, eols, reps) {
		if _, err := s.Add(1, "p", "c", it); err != nil {
			t.Fatal(err)
		}
	}
	type key struct{ prio, id int }
	top := func(all []key) []key {
		slices.SortStableFunc(all, func(a, b key) int { return a.prio - b.prio })
		return all[len(all)-MaxPerLine:]
	}
	// The order of addition is the sequence: spans[:n/2], spans[n/2:] and then the rest.
	var wantSpans, wantEOL, wantReps []key
	for i := range n {
		prio := (i * 7) % 11
		wantSpans = append(wantSpans, key{prio, i})
		wantEOL = append(wantEOL, key{prio, i})
		wantReps = append(wantReps, key{prio, i})
	}
	l := s.Line(1, 2)
	if len(l.Spans) != MaxPerLine || len(l.Rows) != MaxPerLine || len(l.EOL) != MaxPerLine || len(l.Replaces) != MaxPerLine {
		t.Fatalf("kept %d spans, %d rows, %d eol, %d replaces, want %d each", len(l.Spans), len(l.Rows), len(l.EOL), len(l.Replaces), MaxPerLine)
	}
	for i, w := range top(wantSpans) {
		if got := l.Spans[i]; got.Start != 0 || got.Priority != w.prio {
			// Line 2 is inside the range: every span covers it whole, so the identity is
			// checked on line 0 below.
			t.Fatalf("span %d = %+v, want priority %d", i, got, w.prio)
		}
	}
	for i, w := range top(wantEOL) {
		if got := l.EOL[i]; got.Priority != w.prio || got.Text != string(rune(0x100+w.id)) {
			t.Fatalf("eol %d = %+v, want id %d priority %d", i, got, w.id, w.prio)
		}
	}
	for i, w := range top(wantReps) {
		if got := l.Replaces[i]; got.Priority != w.prio || got.Col != w.id {
			t.Fatalf("replace %d = %+v, want id %d priority %d", i, got, w.id, w.prio)
		}
	}
	l0 := s.Line(1, 0)
	for i, w := range top(wantSpans) {
		if got := l0.Spans[i]; got.Start != w.id || got.Priority != w.prio {
			t.Fatalf("line 0 span %d = %+v, want start %d priority %d", i, got, w.id, w.prio)
		}
	}
	for i := 1; i < len(l.Rows); i++ {
		if l.Rows[i].Priority < l.Rows[i-1].Priority {
			t.Fatalf("rows not in painting order at %d", i)
		}
	}
}

// cpuUnit is how long this machine takes, right now, to sort 40000 integers: a yardstick
// that slows down together with the code under test when the machine is busy, so a limit
// written in units of it holds on a loaded machine and still catches an algorithm that got
// ten times slower.
func cpuUnit() time.Duration {
	ref := make([]int, 40000)
	best := time.Hour
	for range 5 {
		for i := range ref {
			ref[i] = (i * 7919) % 40009
		}
		start := time.Now()
		slices.Sort(ref)
		best = min(best, time.Since(start))
	}
	return best
}

// TestLineWindowCostIsBounded: a plugin may put 20000 overlapping highlights in each of
// two namespaces. Building a window (done on the UI goroutine, after every edit) must not
// sort all of them; it took about 0.55 s when it did, ten times what it takes now. The limit
// is in units of cpuUnit (the window costs about 26 of them, a full sort about 280), so it
// does not depend on how fast or how busy the machine is.
func TestLineWindowCostIsBounded(t *testing.T) {
	const limitUnits = 120
	unit := cpuUnit()
	s := New()
	for _, ns := range []string{"a", "b"} {
		items := make([]Item, MaxPerNamespace)
		for i := range items {
			items[i] = Item{Kind: Highlight, FromLine: 0, FromCol: 0, ToLine: 1_000_000, ToCol: 1, Priority: i % 7}
		}
		if err := s.Set(1, "p", ns, items); err != nil {
			t.Fatal(err)
		}
	}
	start := time.Now()
	l := s.Line(1, 10)
	took := time.Since(start)
	if len(l.Spans) != MaxPerLine {
		t.Fatalf("%d spans, want %d", len(l.Spans), MaxPerLine)
	}
	for _, sp := range l.Spans {
		if sp.Priority != 6 {
			t.Fatalf("span of priority %d kept, the top is 6", sp.Priority)
		}
	}
	if took > limitUnits*unit {
		t.Fatalf("building a window of 40000 overlapping items took %v, over %d units of %v", took, limitUnits, unit)
	}
	// An edit drops the windows; the rebuild is bounded as well.
	s.Apply(1, []Edit{ins(0, 0, "x")}, false)
	start = time.Now()
	s.Line(1, 10)
	if took := time.Since(start); took > limitUnits*unit {
		t.Fatalf("rebuilding after an edit took %v, over %d units of %v", took, limitUnits, unit)
	}
}

func BenchmarkLineWindowBuildOverlapping(b *testing.B) {
	s := New()
	for _, ns := range []string{"a", "b"} {
		items := make([]Item, MaxPerNamespace)
		for i := range items {
			items[i] = Item{Kind: Highlight, ToLine: 1_000_000, ToCol: 1, Priority: i % 7}
		}
		_ = s.Set(1, "p", ns, items)
	}
	for b.Loop() {
		s.docs[1].windows = nil
		s.Line(1, 10)
	}
}
