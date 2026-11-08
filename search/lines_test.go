package search

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/buffer/buffertest"
)

// fakeClock returns a clock that advances by step on every reading and
// installs it as the one FindLines uses. Tests that use it must not be parallel.
func fakeClock(t *testing.T, step time.Duration) {
	t.Helper()
	now := time.Unix(0, 0)
	old := linesClock
	linesClock = func() time.Time {
		now = now.Add(step)
		return now
	}
	t.Cleanup(func() { linesClock = old })
}

func TestFindLines(t *testing.T) {
	t.Parallel()
	b := buffer.New("Foo bar\nbaz\n\nfoo日本\nxfoox foo\nTODO: fix\n  // todo later\nend")
	tests := []struct {
		name string
		q    LinesQuery
		want []int
		next int
	}{
		{"literal ignoring case", LinesQuery{Query: Query{Text: "foo"}}, []int{0, 3, 4}, -1},
		{"literal case sensitive", LinesQuery{Query: Query{Text: "foo", CaseSensitive: true}}, []int{3, 4}, -1},
		{"whole word", LinesQuery{Query: Query{Text: "foo", WholeWord: true}}, []int{0, 4}, -1},
		{"regex", LinesQuery{Query: Query{Text: `^\s*//|^TODO`, Regex: true}}, []int{5, 6}, -1},
		{"regex anchors are line anchors", LinesQuery{Query: Query{Text: `^$`, Regex: true}}, []int{2}, -1},
		{"regex never crosses lines", LinesQuery{Query: Query{Text: `bar\nbaz`, Regex: true}}, nil, -1},
		{"regex whole word", LinesQuery{Query: Query{Text: `fo+`, Regex: true, WholeWord: true}}, []int{0, 4}, -1},
		{"multibyte", LinesQuery{Query: Query{Text: "日本"}}, []int{3}, -1},
		{"from", LinesQuery{Query: Query{Text: "foo"}, From: 4}, []int{4}, -1},
		{"to", LinesQuery{Query: Query{Text: "foo"}, To: 4}, []int{0, 3}, -1},
		{"to past the end", LinesQuery{Query: Query{Text: "end"}, To: 99}, []int{7}, -1},
		{"max", LinesQuery{Query: Query{Text: "foo"}, Max: 2}, []int{0, 3}, 4},
		{"max reached on the last line", LinesQuery{Query: Query{Text: "end"}, Max: 1}, []int{7}, -1},
		{"empty range", LinesQuery{Query: Query{Text: "foo"}, From: 5, To: 5}, nil, -1},
		{"from past the end", LinesQuery{Query: Query{Text: "foo"}, From: 50}, nil, -1},
		{"negative from", LinesQuery{Query: Query{Text: "end"}, From: -4}, []int{7}, -1},
		{"empty query", LinesQuery{}, nil, -1},
	}
	for _, tt := range tests {
		got, next, err := FindLines(b, tt.q)
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if !slices.Equal(got, tt.want) || next != tt.next {
			t.Errorf("%s: lines %v next %d, want %v next %d", tt.name, got, next, tt.want, tt.next)
		}
	}
	if _, _, err := FindLines(b, LinesQuery{Query: Query{Text: "(", Regex: true}}); err == nil {
		t.Error("a bad regex was accepted")
	}
}

func TestFindLinesBudgetNext(t *testing.T) {
	fakeClock(t, 10*time.Millisecond)
	var sb strings.Builder
	for i := range 500 {
		fmt.Fprintf(&sb, "line %d hit\n", i)
	}
	b := buffer.New(sb.String())
	q := LinesQuery{Query: Query{Text: "hit"}, Budget: 25 * time.Millisecond}
	got, next, err := FindLines(b, q)
	if err != nil {
		t.Fatal(err)
	}
	if next <= 0 || next >= 500 || len(got) != next {
		t.Fatalf("one chunk: %d lines, next %d; want a partial result that ends where next says", len(got), next)
	}
	// A budget over the cap is the cap, and a budget of 0 the default: with a
	// clock that ticks 10 ms per reading neither finishes 500 lines.
	for _, budget := range []time.Duration{0, time.Hour} {
		q.Budget = budget
		got, next, _ = FindLines(b, q)
		if next < 0 || len(got) != next {
			t.Errorf("budget %v: %d lines, next %d, want a partial result", budget, len(got), next)
		}
	}
	// However small the budget, one call examines at least one line.
	q.Budget = time.Nanosecond
	q.From = 100
	got, next, _ = FindLines(b, q)
	if len(got) == 0 || next <= 100 {
		t.Errorf("tiny budget: %v next %d, want progress", got, next)
	}
}

func TestFindLinesChunkedLoop(t *testing.T) {
	fakeClock(t, 3*time.Millisecond)
	var sb strings.Builder
	for i := range 1500 {
		if i%7 == 0 {
			fmt.Fprintf(&sb, "match %d\n", i)
		} else {
			fmt.Fprintf(&sb, "other %d\n", i)
		}
	}
	b := buffer.New(sb.String())
	q := LinesQuery{Query: Query{Text: "match"}, Budget: 20 * time.Millisecond}
	var chunked []int
	calls := 0
	for from := 0; from >= 0; calls++ {
		q.From = from
		got, nx, err := FindLines(b, q)
		if err != nil {
			t.Fatal(err)
		}
		chunked = append(chunked, got...)
		if nx >= 0 && nx <= from {
			t.Fatalf("no progress: from %d next %d", from, nx)
		}
		from = nx
		if calls > 5000 {
			t.Fatal("the loop does not end")
		}
	}
	if calls < 2 {
		t.Errorf("finished in %d call; the budget was not applied", calls)
	}
	var want []int
	for i := 0; i < 1500; i += 7 {
		want = append(want, i)
	}
	if !slices.Equal(chunked, want) {
		t.Errorf("chunked scan found %d lines, want %d", len(chunked), len(want))
	}
}

func TestFindLinesRegexCostPerLine(t *testing.T) {
	t.Parallel()
	b := buffer.New("short line\n" + strings.Repeat("a", 3<<20) + "\ntail")
	_, _, err := FindLines(b, LinesQuery{Query: Query{Text: `(?:[a-z]+\s*){1,50}!`, Regex: true}, From: 1, Budget: MaxLinesBudget})
	if !errors.Is(err, ErrTooCostly) {
		t.Errorf("a long line against a heavy pattern: %v, want ErrTooCostly", err)
	}
	// The same line is fine for a plain string and a cheap pattern.
	for _, q := range []Query{{Text: "aaaa"}, {Text: "aaaa", Regex: true, CaseSensitive: true}, {Text: "b+", Regex: true}} {
		if _, _, err := FindLines(b, LinesQuery{Query: q, Budget: MaxLinesBudget}); err != nil {
			t.Errorf("%+v: %v", q, err)
		}
	}
}

// The clock is read after every costly line, not every 32 lines: 31 lines of a few
// milliseconds each must not run past a 30 ms budget by more than a line. That is
// asserted on how many lines were scanned: the scan stops before the 32nd line, so the
// clock was read after each of them. How long it took depends on the machine and on what
// else it is running, so the wall clock only gets a generous sanity bound.
func TestFindLinesBudgetHoldsForCostlyLines(t *testing.T) {
	line := strings.Repeat("a ", 2500) + "\n"
	b := buffer.New(strings.Repeat(line, 400))
	q := LinesQuery{Query: Query{Text: `(?:[a-z]+\s*){1,50}!`, Regex: true, CaseSensitive: true}, Budget: 30 * time.Millisecond}
	start := time.Now()
	got, next, err := FindLines(b, q)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || next <= 0 || next >= 32 {
		t.Errorf("lines %v next %d, want no hit and a stop before the 32nd line", got, next)
	}
	if elapsed > 10*time.Second {
		t.Errorf("FindLines took %v for a budget of %v", elapsed, q.Budget)
	}
}

func TestFindLinesOnLargeBuffer(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 300 {
		if i%50 == 0 {
			fmt.Fprintf(&sb, "needle %d\n", i)
		} else {
			fmt.Fprintf(&sb, "hay %d\n", i)
		}
	}
	b, _ := largeBuffer(t, sb.String())
	got, next, err := FindLines(b, LinesQuery{Query: Query{Text: "needle"}, Budget: MaxLinesBudget})
	if err != nil || next != -1 || !slices.Equal(got, []int{0, 50, 100, 150, 200, 250}) {
		t.Fatalf("got %v next %d err %v", got, next, err)
	}
	if st := b.Stats(); st.OverCap != 0 {
		t.Errorf("the scan hit the materialize cap %d times", st.OverCap)
	}
	got, next, err = FindLines(b, LinesQuery{Query: Query{Text: `needle \d+0$`, Regex: true}, From: 60, To: 160})
	if err != nil || next != -1 || !slices.Equal(got, []int{100, 150}) {
		t.Fatalf("regex in a range: %v next %d err %v", got, next, err)
	}
}

func TestFindLinesOnAGrowingIndex(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 400 {
		fmt.Fprintf(&sb, "row %03d\n", i)
	}
	src := sb.String()
	b, err := buffer.OpenLarge(buffertest.NewMemSource([]byte(src)), buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	if b.Index(0).Complete {
		t.Fatal("test setup: the index is already complete")
	}
	known := b.Lines()
	got, next, err := FindLines(b, LinesQuery{Query: Query{Text: "row"}})
	if err != nil || next != known || len(got) != known {
		t.Fatalf("incomplete index: %d lines next %d err %v, want %d lines and next %d", len(got), next, err, known, known)
	}
	// Once the index is complete the same scan, continued, finishes.
	for !b.Index(0).Complete {
		sc := b.BeginIndexScan()
		batch, err := sc.Next(context.Background(), 1<<20)
		sc.Close()
		if err != nil {
			t.Fatal(err)
		}
		b.ApplyIndex(batch)
	}
	got, next, err = FindLines(b, LinesQuery{Query: Query{Text: "row"}, From: known, Budget: MaxLinesBudget})
	if err != nil || next != -1 || len(got) != 400-known {
		t.Fatalf("after indexing: %d lines next %d err %v", len(got), next, err)
	}
}
