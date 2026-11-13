package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/buffer/buffertest"
)

// findNext is FindNext with the window size as a parameter, so tests can make
// every window boundary matter.
func findNext(s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, maxBytes int64, window int) (Match, bool, bool, error) {
	m, found, scanned, _, err := findNextSpan(s, q, from, dir, maxBytes, window, defaultHuge, Cursor{})
	return m, found, scanned, err
}

// findNextAt is FindNextAt with the window size as a parameter.
func findNextAt(s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, maxBytes int64, window int, at Cursor) (Match, bool, bool, Cursor, error) {
	return findNextSpan(s, q, from, dir, maxBytes, window, defaultHuge, at)
}

// positions lists every position to start a search from in text: each column
// of each line, the end of each line, and a few positions past the end.
func positions(text string) []buffer.Pos {
	var out []buffer.Pos
	for l, line := range strings.Split(text, "\n") {
		for c := 0; c <= len([]rune(line)); c++ {
			out = append(out, buffer.Pos{Line: l, Col: c})
		}
	}
	last := strings.Count(text, "\n")
	return append(out, buffer.Pos{Line: last + 1}, buffer.Pos{Line: last + 40, Col: 7}, buffer.Pos{Line: -1, Col: 3})
}

// wantNext is what Next and Prev say over the matches Find lists: the answer
// FindNext must reproduce without the list.
func wantNext(matches []Match, from buffer.Pos, dir Dir) (Match, bool) {
	var i int
	var ok bool
	if dir == Forward {
		i, ok = Next(matches, from, true)
	} else {
		i, ok = Prev(matches, from, true)
	}
	if !ok {
		return Match{}, false
	}
	return matches[i], true
}

func TestFindNextEqualsFindNextPrev(t *testing.T) {
	t.Parallel()
	const multi = "foo\nbar\nbaz foo\nbar"
	tests := []struct {
		name string
		text string
		q    Query
		// minWindow: the smallest window that keeps a multi-line match whole
		// (a match crossing a window boundary is missed by design).
		minWindow int
	}{
		{name: "literal", text: "foo bar\nFoo foo\nxfoo foox\n\nfoo\nlast foo", q: Query{Text: "foo", CaseSensitive: true}},
		{name: "case insensitive", text: "foo bar\nFoo foo\nxfoo foox\n\nfoo\nlast FOO", q: Query{Text: "foo"}},
		{name: "whole word", text: "foo bar\nFoo foo\nxfoo foox\n\nfoo_ foo\nlast foo.", q: Query{Text: "foo", WholeWord: true}},
		{name: "regex with groups", text: "a1 b22\nc333 d\n\ne4444", q: Query{Text: `[a-e](\d+)`, Regex: true}},
		{name: "regex line anchor", text: "bar\nbaz\n bar\nbar bar\n", q: Query{Text: `^bar`, Regex: true}},
		{name: "regex line end", text: "ab\nab \nab", q: Query{Text: `b$`, Regex: true}},
		{name: "multibyte columns", text: "héllo wörld foo\n日本語 foo 日本\nfoo", q: Query{Text: "foo"}},
		{name: "no match", text: "alpha\nbeta\ngamma", q: Query{Text: "zzz"}},
		{name: "single line", text: "foo", q: Query{Text: "foo"}},
		{name: "empty document", text: "", q: Query{Text: "foo"}},
		{name: "multi-line literal", text: multi, q: Query{Text: "bar\nbaz"}, minWindow: len(multi)},
		{name: "multi-line regex", text: multi, q: Query{Text: `foo\nbar`, Regex: true}, minWindow: len(multi)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref := buffer.New(tt.text)
			matches, err := Find(ref, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			big, _ := largeBuffer(t, tt.text)
			views := map[string]*buffer.Snapshot{"memory": ref.Snapshot(), "large": big.Snapshot()}
			for name, snap := range views {
				t.Cleanup(snap.Release)
				for _, window := range []int{1, 7, 20, nextWindow} {
					if window < tt.minWindow {
						continue
					}
					for _, dir := range []Dir{Forward, Backward} {
						for _, from := range positions(tt.text) {
							got, found, scanned, err := findNext(snap, tt.q, from, dir, 0, window)
							want, wantFound := wantNext(matches, from, dir)
							if err != nil || !scanned || found != wantFound || got != want {
								t.Fatalf("%s window %d dir %d from %v: FindNext = %v, %v, %v, %v; want %v, %v, true, nil",
									name, window, dir, from, got, found, scanned, err, want, wantFound)
							}
						}
					}
				}
			}
		})
	}
}

func TestFindNextAfterEditsAndSnapshotIsolation(t *testing.T) {
	t.Parallel()
	b, _ := largeBuffer(t, "one\ntwo\nthree\nfour")
	b.Insert(buffer.Pos{Line: 2, Col: 2}, " needle ")
	before := b.Snapshot()
	defer before.Release()
	b.Delete(buffer.Pos{Line: 2, Col: 0}, buffer.Pos{Line: 2, Col: 10})

	q := Query{Text: "needle"}
	m, found, scanned, err := findNext(before, q, buffer.Pos{}, Forward, 0, 5)
	if err != nil || !found || !scanned || m != (Match{Start: buffer.Pos{Line: 2, Col: 3}, End: buffer.Pos{Line: 2, Col: 9}}) {
		t.Fatalf("snapshot before the delete: %v, %v, %v, %v", m, found, scanned, err)
	}
	after := b.Snapshot()
	defer after.Release()
	if _, found, scanned, err := findNext(after, q, buffer.Pos{}, Forward, 0, 5); err != nil || found || !scanned {
		t.Fatalf("snapshot after the delete: found %v, scanned %v, err %v; want not found", found, scanned, err)
	}
}

// numbered returns n lines "line 0000" ... and the text of line needleLine
// replaced by "needle".
func numbered(n, needleLine int) string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %04d", i)
	}
	lines[needleLine] = "needle"
	return strings.Join(lines, "\n")
}

func TestFindNextStopsAtFirstHit(t *testing.T) {
	t.Parallel()
	const n = 4000
	b, src := largeBuffer(t, numbered(n, 3))
	size := src.Size()
	snap := b.Snapshot()
	defer snap.Release()

	before := b.Stats().ReadBytes
	m, found, scanned, err := findNext(snap, Query{Text: "needle"}, buffer.Pos{}, Forward, 0, 64)
	if err != nil || !found || !scanned || m.Start != (buffer.Pos{Line: 3}) {
		t.Fatalf("FindNext = %v, %v, %v, %v", m, found, scanned, err)
	}
	if read := b.Stats().ReadBytes - before; read > size/20 {
		t.Errorf("a hit on line 3 read %d of %d bytes", read, size)
	}

	// Going backward from the end reaches the same line only after wrapping
	// to it from the far side, so it reads far more; going backward from just
	// after it stops at once.
	before = b.Stats().ReadBytes
	m, found, _, err = findNext(snap, Query{Text: "needle"}, buffer.Pos{Line: 5}, Backward, 0, 64)
	if err != nil || !found || m.Start.Line != 3 {
		t.Fatalf("Backward = %v, %v, %v", m, found, err)
	}
	if read := b.Stats().ReadBytes - before; read > size/20 {
		t.Errorf("a backward hit on line 3 read %d of %d bytes", read, size)
	}
}

func TestFindNextBudget(t *testing.T) {
	t.Parallel()
	const n = 4000
	b, src := largeBuffer(t, numbered(n, n-2))
	size := src.Size()
	snap := b.Snapshot()
	defer snap.Release()
	q := Query{Text: "needle"}

	const budget = 300
	before := b.Stats().ReadBytes
	m, found, scanned, err := findNext(snap, q, buffer.Pos{}, Forward, budget, 64)
	if err != nil || found || scanned {
		t.Fatalf("with a budget of %d: %v, %v, %v, %v; want not found, not scanned", budget, m, found, scanned, err)
	}
	if read := b.Stats().ReadBytes - before; read > 4*budget {
		t.Errorf("budget %d but %d bytes read (file is %d)", budget, read, size)
	}

	// The same search with room for the whole file finds the match.
	m, found, scanned, err = findNext(snap, q, buffer.Pos{}, Forward, 2*size, 64)
	if err != nil || !found || !scanned || m.Start.Line != n-2 {
		t.Fatalf("with a large budget: %v, %v, %v, %v", m, found, scanned, err)
	}

	// A budget that runs out in the middle of the second phase is reported
	// too, and a query with no match at all finishes when the budget allows.
	_, found, scanned, err = findNext(snap, Query{Text: "absent"}, buffer.Pos{Line: n / 2}, Forward, size/2, 64)
	if err != nil || found || scanned {
		t.Fatalf("absent query, half the budget: found %v, scanned %v, err %v; want false, false", found, scanned, err)
	}
	_, found, scanned, err = findNext(snap, Query{Text: "absent"}, buffer.Pos{Line: n / 2}, Backward, 4*size, 64)
	if err != nil || found || !scanned {
		t.Fatalf("absent query, whole budget: found %v, scanned %v, err %v; want false, true", found, scanned, err)
	}
}

func TestFindNextDefaultBudgetIsUsed(t *testing.T) {
	t.Parallel()
	b := buffer.New("a\nb\nneedle")
	snap := b.Snapshot()
	defer snap.Release()
	for _, budget := range []int64{0, -5} {
		if _, found, scanned, err := FindNext(snap, Query{Text: "needle"}, buffer.Pos{}, Forward, budget); err != nil || !found || !scanned {
			t.Errorf("budget %d: found %v, scanned %v, err %v", budget, found, scanned, err)
		}
	}
}

func TestFindNextEmptyAndInvalidQuery(t *testing.T) {
	t.Parallel()
	b, src := largeBuffer(t, "a\nb\nc")
	snap := b.Snapshot()
	defer snap.Release()
	before := src.BytesRead()

	if _, found, scanned, err := FindNext(snap, Query{}, buffer.Pos{}, Forward, 0); err != nil || found || !scanned {
		t.Errorf("empty query: found %v, scanned %v, err %v; want false, true, nil", found, scanned, err)
	}
	if _, found, scanned, err := FindNext(snap, Query{Text: "(", Regex: true}, buffer.Pos{}, Backward, 0); err == nil || found || scanned {
		t.Errorf("invalid regex: found %v, scanned %v, err %v; want an error", found, scanned, err)
	}
	if got := src.BytesRead(); got != before {
		t.Errorf("source read %d bytes for queries that cannot match", got-before)
	}
}

func TestFindNextReportsFailedRead(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	text := numbered(200, 150)
	b, src := largeBuffer(t, text)
	snap := b.Snapshot()
	defer snap.Release()

	src.FailNext(1_000_000, boom)
	m, found, scanned, err := findNext(snap, Query{Text: "needle"}, buffer.Pos{}, Forward, 0, 64)
	if err == nil {
		t.Fatalf("FindNext over a failing source = %v, %v, %v, nil; want an error", m, found, scanned)
	}
	if found || scanned {
		t.Errorf("found %v, scanned %v after a failed read; want false, false", found, scanned)
	}
}

func TestFindNextBackwardForwardAreInverse(t *testing.T) {
	t.Parallel()
	text := "x foo\nfoo\n\nfoo foo\nend"
	b, _ := largeBuffer(t, text)
	snap := b.Snapshot()
	defer snap.Release()
	q := Query{Text: "foo"}
	all, err := Find(buffer.New(text), q)
	if err != nil {
		t.Fatal(err)
	}
	// Walk forward through every match with F3, then back with Shift+F3.
	from := buffer.Pos{}
	var seen []Match
	for range all {
		m, found, _, err := findNext(snap, q, from, Forward, 0, 4)
		if err != nil || !found {
			t.Fatalf("forward from %v: %v, %v, %v", from, m, found, err)
		}
		seen = append(seen, m)
		from = m.End
	}
	if !equalMatches(seen, all) {
		t.Fatalf("forward walk = %v, want %v", seen, all)
	}
	for i := len(all) - 1; i >= 0; i-- {
		m, found, _, err := findNext(snap, q, from, Backward, 0, 4)
		if err != nil || !found || m != all[i] {
			t.Fatalf("backward from %v = %v, %v, %v; want %v", from, m, found, err, all[i])
		}
		from = m.Start
	}
}

// A snapshot is searched on another goroutine while the buffer is edited and
// indexed on this one. Run with -race.
func TestFindNextConcurrentWithEditsAndIndex(t *testing.T) {
	t.Parallel()
	src := buffertest.NewMemSource([]byte(numbered(600, 40)))
	b, err := buffer.OpenLarge(src, buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	snap := b.Snapshot()
	defer snap.Release()

	type result struct {
		m              Match
		found, scanned bool
		err            error
	}
	q := Query{Text: "needle"}
	done := make(chan result, 1)
	go func() {
		var r result
		r.m, r.found, r.scanned, r.err = findNext(snap, q, buffer.Pos{}, Forward, 0, 32)
		done <- r
	}()
	for !b.Index(0).Complete {
		b.Insert(buffer.Pos{}, "x")
		sc := b.BeginIndexScan()
		if sc == nil {
			t.Fatal("no index scan with an incomplete index")
		}
		batch, err := sc.Next(context.Background(), 64)
		sc.Close()
		if err != nil {
			t.Fatal(err)
		}
		b.ApplyIndex(batch)
		b.Delete(buffer.Pos{}, buffer.Pos{Col: 1})
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("FindNext: %v", got.err)
	}
	// The snapshot is frozen: the same search gives the same answer now.
	var again result
	again.m, again.found, again.scanned, again.err = findNext(snap, q, buffer.Pos{}, Forward, 0, 32)
	if got != again {
		t.Errorf("concurrent search = %+v, repeat = %+v", got, again)
	}
}

// TestFindNextAtContinuesWhereTheBudgetRanOut checks that a match farther than one
// budget is reached by passing the returned cursor back, in both directions, with
// and without the wrap, and that each call reads about one budget.
func TestFindNextAtContinuesWhereTheBudgetRanOut(t *testing.T) {
	t.Parallel()
	const n = 4000
	q := Query{Text: "needle"}

	tests := []struct {
		name string
		from buffer.Pos
		dir  Dir
		want int // the line of the match, which is far from from in the direction of travel
	}{
		{"forward", buffer.Pos{}, Forward, n - 2},
		{"forward through the wrap", buffer.Pos{Line: n - 1}, Forward, n - 3},
		{"backward", buffer.Pos{Line: n - 1}, Backward, 3},
		{"backward through the wrap", buffer.Pos{Line: 1}, Backward, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, src := largeBuffer(t, numbered(n, tt.want))
			snap := b.Snapshot()
			defer snap.Release()
			budget := src.Size() / 8
			var at Cursor
			for call := 1; call <= 20; call++ {
				before := b.Stats().ReadBytes
				m, found, scanned, next, err := findNextAt(snap, q, tt.from, tt.dir, budget, 64, at)
				if err != nil {
					t.Fatal(err)
				}
				if read := b.Stats().ReadBytes - before; read > 2*budget {
					t.Errorf("call %d read %d bytes for a budget of %d", call, read, budget)
				}
				if found {
					if !scanned || m.Start.Line != tt.want {
						t.Fatalf("found %v scanned %v after %d calls, want line %d", m, scanned, call, tt.want)
					}
					if call == 1 {
						t.Fatalf("the first call found the match, so the budget %d did not limit it", budget)
					}
					return
				}
				if scanned {
					t.Fatalf("call %d searched the whole document and found nothing", call)
				}
				if next == at {
					t.Fatalf("call %d made no progress: cursor stayed %+v", call, at)
				}
				at = next
			}
			t.Fatal("the match was not reached in 20 calls")
		})
	}
}
