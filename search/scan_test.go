package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/buffer/buffertest"
)

// runScan steps sc to the end one window at a time (a budget of zero reads
// exactly one window per call) and checks, after every step, that the matches
// found so far are the first ones of wantOrder, the order a scan finds them in.
func runScan(t *testing.T, sc *Scan, wantOrder []Match) (steps int) {
	t.Helper()
	for {
		done, err := sc.Step(0)
		steps++
		if err != nil {
			t.Fatalf("Step: %v", err)
		}
		if done {
			return steps
		}
		got := sc.Matches()
		if len(got) > len(wantOrder) || !equalMatches(got, wantOrder[:len(got)]) {
			t.Fatalf("after step %d the matches are %v, want a prefix of %v", steps, got, wantOrder)
		}
		if steps > 10_000 {
			t.Fatal("scan does not end")
		}
	}
}

// scanOrder is the order a scan that began at line start finds the matches that
// Find lists: from that line to the end, then the lines above it.
func scanOrder(all []Match, start int) []Match {
	var out, above []Match
	for _, m := range all {
		if m.Start.Line >= start {
			out = append(out, m)
		} else {
			above = append(above, m)
		}
	}
	return append(out, above...)
}

func TestScanEqualsFind(t *testing.T) {
	t.Parallel()
	const multi = "foo\nbar\nbaz foo\nbar"
	tests := []struct {
		name string
		text string
		q    Query
		// minWindow: the smallest window that keeps a multi-line match whole. A
		// scan that starts below line 0 also splits such a match at its start
		// line, so those tests start at line 0 only.
		minWindow int
	}{
		{name: "literal", text: "foo bar\nFoo foo\nxfoo foox\n\nfoo\nlast foo", q: Query{Text: "foo", CaseSensitive: true}},
		{name: "case insensitive", text: "foo bar\nFoo foo\nxfoo foox\n\nfoo\nlast FOO", q: Query{Text: "foo"}},
		{name: "whole word", text: "foo bar\nFoo foo\nxfoo foox\n\nfoo_ foo\nlast foo.", q: Query{Text: "foo", WholeWord: true}},
		{name: "whole word regex", text: "foo bar\nFoo foo\nxfoo foox\n\nfoo_ foo\nlast foo.", q: Query{Text: "fo+", Regex: true, WholeWord: true}},
		{name: "regex with groups", text: "a1 b22\nc333 d\n\ne4444", q: Query{Text: `[a-e](\d+)`, Regex: true}},
		{name: "regex line anchor", text: "bar\nbaz\n bar\nbar bar\n", q: Query{Text: `^bar`, Regex: true}},
		{name: "regex line end", text: "ab\nab \nab", q: Query{Text: `b$`, Regex: true}},
		{name: "multibyte columns", text: "héllo wörld foo\n日本語 foo 日本 foo\nfoo", q: Query{Text: "foo"}},
		{name: "overlapping literal", text: "aaaa\naa\na", q: Query{Text: "aa"}},
		{name: "no match", text: "alpha\nbeta\ngamma", q: Query{Text: "zzz"}},
		{name: "single line", text: "foo", q: Query{Text: "foo"}},
		{name: "empty document", text: "", q: Query{Text: "foo"}},
		{name: "empty query", text: "foo\nbar", q: Query{}},
		{name: "multi-line literal", text: multi, q: Query{Text: "bar\nbaz"}, minWindow: len(multi)},
		{name: "multi-line regex", text: multi, q: Query{Text: `foo\nbar`, Regex: true}, minWindow: len(multi)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ref := buffer.New(tt.text)
			want, err := Find(ref, tt.q)
			if err != nil {
				t.Fatal(err)
			}
			big, _ := largeBuffer(t, tt.text)
			buffers := map[string]*buffer.Buffer{"memory": ref, "large": big}
			lines := strings.Count(tt.text, "\n") + 1
			for name, b := range buffers {
				for _, window := range []int{1, 7, 20, nextWindow} {
					if window < tt.minWindow {
						continue
					}
					for start := -1; start <= lines+1; start++ {
						if tt.minWindow > 0 && start > 0 {
							continue
						}
						sc, err := newScan(b.Snapshot(), tt.q, start, window, MaxMatches)
						if err != nil {
							t.Fatalf("%s window %d start %d: %v", name, window, start, err)
						}
						clamped := min(max(start, 0), lines-1)
						runScan(t, sc, scanOrder(want, clamped))
						if got := sc.Matches(); !equalMatches(got, want) {
							t.Fatalf("%s window %d start %d: matches %v, want %v", name, window, start, got, want)
						}
						if !sc.Done() || sc.Capped() || sc.Progress() != 1 {
							t.Fatalf("%s window %d start %d: done %v capped %v progress %v", name, window, start, sc.Done(), sc.Capped(), sc.Progress())
						}
						sc.Close()
					}
				}
			}
		})
	}
}

func TestScanFindsTheMatchNearestTheCaretFirst(t *testing.T) {
	t.Parallel()
	const n = 400
	b, _ := largeBuffer(t, numbered(n, 300))
	b.Insert(buffer.Pos{Line: 10}, "needle ")
	sc, err := newScan(b.Snapshot(), Query{Text: "needle"}, 250, 64, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	for len(sc.Matches()) == 0 {
		if done, err := sc.Step(0); err != nil || done {
			t.Fatalf("Step = %v, %v before a match", done, err)
		}
	}
	if got := sc.Matches(); len(got) != 1 || got[0].Start.Line != 300 {
		t.Fatalf("first match = %v, want the one on line 300", got)
	}
	if p := sc.Progress(); p <= 0 || p >= 0.5 {
		t.Errorf("Progress after reaching line 300 from 250 = %v, want a small part", p)
	}
	for done := false; !done; {
		done, err = sc.Step(time.Second)
		if err != nil {
			t.Fatal(err)
		}
	}
	got := sc.Matches()
	if len(got) != 2 || got[0].Start.Line != 10 || got[1].Start.Line != 300 {
		t.Fatalf("matches in document order = %v, want lines 10 and 300", got)
	}
}

func TestScanCapSetsCapped(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	for i := range 200 {
		fmt.Fprintf(&sb, "row %03d x\n", i)
	}
	text := strings.TrimSuffix(sb.String(), "\n")
	all, err := Find(buffer.New(text), Query{Text: "x"})
	if err != nil || len(all) != 200 {
		t.Fatalf("setup: %d matches, %v", len(all), err)
	}
	b, _ := largeBuffer(t, text)
	tests := []struct {
		name       string
		limit      int
		start      int
		wantLen    int
		wantCapped bool
	}{
		{"cap lowered to 50", 50, 0, 50, true},
		{"exactly the cap is not capped", 200, 0, 200, false},
		{"under the cap", 201, 0, 200, false},
		{"one over", 199, 0, 199, true},
		{"cap taken from the start line on", 50, 120, 50, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc, err := newScan(b.Snapshot(), Query{Text: "x"}, tt.start, 100, tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			defer sc.Close()
			for done := false; !done; {
				if done, err = sc.Step(time.Second); err != nil {
					t.Fatal(err)
				}
			}
			got := sc.Matches()
			if len(got) != tt.wantLen || sc.Capped() != tt.wantCapped {
				t.Fatalf("%d matches, capped %v; want %d, %v", len(got), sc.Capped(), tt.wantLen, tt.wantCapped)
			}
			if !tt.wantCapped && !equalMatches(got, all) {
				t.Errorf("uncapped matches differ from Find")
			}
			if tt.wantCapped && tt.start == 0 && !equalMatches(got, all[:tt.wantLen]) {
				t.Errorf("a capped scan from line 0 should hold the first matches in order")
			}
			for i := 1; i < len(got); i++ {
				if !got[i-1].Start.Less(got[i].Start) {
					t.Fatalf("matches %d and %d are not in document order: %v, %v", i-1, i, got[i-1], got[i])
				}
			}
			// A capped scan is done: another step changes nothing.
			if done, err := sc.Step(0); !done || err != nil || len(sc.Matches()) != tt.wantLen {
				t.Errorf("Step after the end = %v, %v with %d matches", done, err, len(sc.Matches()))
			}
		})
	}
}

func TestMaxMatchesIsTheDefaultCap(t *testing.T) {
	t.Parallel()
	sc, err := NewScan(buffer.New("a").Snapshot(), Query{Text: "a"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	if sc.limit != MaxMatches || MaxMatches != 100_000 || sc.window != nextWindow {
		t.Errorf("limit %d, window %d; want %d and %d", sc.limit, sc.window, MaxMatches, nextWindow)
	}
}

func TestScanVersionDetectsEdits(t *testing.T) {
	t.Parallel()
	for name, b := range map[string]*buffer.Buffer{"memory": buffer.New("foo\nbar\nfoo"), "large": mustLarge(t, "foo\nbar\nfoo")} {
		t.Run(name, func(t *testing.T) {
			sc, err := NewScan(b.Snapshot(), Query{Text: "foo"}, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer sc.Close()
			ver := b.Version()
			if sc.Version() != ver {
				t.Fatalf("Version = %d, want the buffer's %d", sc.Version(), ver)
			}
			b.Delete(buffer.Pos{}, buffer.Pos{Col: 3})
			if sc.Version() == b.Version() {
				t.Errorf("scan version %d still equals the buffer's after an edit", sc.Version())
			}
			// The scan still searches the text it was started on.
			if done, err := sc.Step(time.Second); !done || err != nil || len(sc.Matches()) != 2 {
				t.Errorf("Step = %v, %v with %d matches; want the 2 matches of the old text", done, err, len(sc.Matches()))
			}
			b.Undo()
			if sc.Version() != b.Version() {
				t.Errorf("after undo the buffer is at %d, the scan at %d", b.Version(), sc.Version())
			}
		})
	}
}

func mustLarge(t *testing.T, text string) *buffer.Buffer {
	t.Helper()
	b, _ := largeBuffer(t, text)
	return b
}

func TestScanEmptyAndInvalidQuery(t *testing.T) {
	t.Parallel()
	b, src := largeBuffer(t, "a\nb\nc")
	before := src.BytesRead()

	snap := b.Snapshot()
	sc, err := NewScan(snap, Query{}, 0)
	if err != nil {
		t.Fatalf("empty query: %v", err)
	}
	if done, err := sc.Step(time.Second); !done || err != nil || len(sc.Matches()) != 0 || sc.Progress() != 1 {
		t.Errorf("empty query: done %v, err %v, %d matches, progress %v", done, err, len(sc.Matches()), sc.Progress())
	}
	sc.Close()

	snap = b.Snapshot()
	defer snap.Release()
	if sc, err := NewScan(snap, Query{Text: "(", Regex: true}, 0); err == nil || sc != nil {
		t.Errorf("invalid regex: scan %v, err %v; want an error", sc, err)
	}
	if got := src.BytesRead(); got != before {
		t.Errorf("queries that cannot match read %d bytes of the source", got-before)
	}
	// The failed NewScan did not take the snapshot: it is still readable.
	if snap.Line(0) != "a" || snap.Err() != nil {
		t.Errorf("snapshot unusable after a failed NewScan: %q, %v", snap.Line(0), snap.Err())
	}
	if _, found, _, err := FindNext(snap, Query{Text: "c"}, buffer.Pos{}, Forward, 0); err != nil || !found {
		t.Errorf("the snapshot cannot be searched after a failed NewScan: %v, %v", found, err)
	}
}

func TestScanCloseReleasesSnapshot(t *testing.T) {
	t.Parallel()
	b, src := largeBuffer(t, numbered(100, 50))
	sc, err := newScan(b.Snapshot(), Query{Text: "needle"}, 0, 32, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Step(0); err != nil {
		t.Fatal(err)
	}
	// The buffer lets go of the source; the scan alone keeps it open.
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if src.Closed() {
		t.Fatal("the source was closed while a scan still holds a snapshot")
	}
	if done, err := sc.Step(time.Second); err != nil || !done || len(sc.Matches()) != 1 {
		t.Fatalf("Step after the buffer closed = %v, %v, %d matches", done, err, len(sc.Matches()))
	}
	sc.Close()
	if !src.Closed() {
		t.Error("the source is still open after Close")
	}
	// Close is idempotent and the matches stay readable; a closed scan is done.
	sc.Close()
	if len(sc.Matches()) != 1 {
		t.Errorf("matches after Close = %v", sc.Matches())
	}
	if done, err := sc.Step(time.Second); !done || err != nil {
		t.Errorf("Step after Close = %v, %v", done, err)
	}
}

func TestScanCloseBeforeTheEnd(t *testing.T) {
	t.Parallel()
	b, _ := largeBuffer(t, numbered(200, 150))
	sc, err := newScan(b.Snapshot(), Query{Text: "line"}, 100, 64, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Step(0); err != nil {
		t.Fatal(err)
	}
	sc.Close()
	if !sc.Done() || sc.Progress() >= 1 {
		t.Errorf("closed early: done %v, progress %v; want done and progress below 1", sc.Done(), sc.Progress())
	}
	before := len(sc.Matches())
	if done, _ := sc.Step(time.Second); !done || len(sc.Matches()) != before {
		t.Errorf("a closed scan went on: %d matches, was %d", len(sc.Matches()), before)
	}
}

func TestScanProgressAndSteps(t *testing.T) {
	t.Parallel()
	const n = 500
	b, _ := largeBuffer(t, numbered(n, 499))
	sc, err := newScan(b.Snapshot(), Query{Text: "needle"}, 200, 128, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	last := -1.0
	steps := 0
	for {
		done, err := sc.Step(0)
		if err != nil {
			t.Fatal(err)
		}
		steps++
		p := sc.Progress()
		if p < last || p > 1 {
			t.Fatalf("Progress went from %v to %v", last, p)
		}
		last = p
		if done {
			break
		}
		if p == 1 {
			t.Fatal("progress is 1 before the scan is done")
		}
	}
	if last != 1 || steps < 10 {
		t.Errorf("progress %v after %d steps; want 1 after many small ones", last, steps)
	}
	st := sc.Stats()
	text := numbered(n, 499)
	if st.BytesScanned < int64(len(text)-steps) || st.BytesScanned > int64(len(text)) {
		t.Errorf("BytesScanned = %d for a text of %d bytes in %d steps", st.BytesScanned, len(text), steps)
	}
	if st.Elapsed < 0 || st.Elapsed != sc.Stats().Elapsed {
		t.Errorf("Elapsed = %v, then %v; want a fixed value once done", st.Elapsed, sc.Stats().Elapsed)
	}
}

func TestScanBigBudgetFinishesInOneStep(t *testing.T) {
	t.Parallel()
	b, _ := largeBuffer(t, numbered(300, 100))
	sc, err := newScan(b.Snapshot(), Query{Text: "needle"}, 0, 64, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	if done, err := sc.Step(time.Hour); !done || err != nil {
		t.Fatalf("Step(1h) = %v, %v; want done", done, err)
	}
}

func TestScanColumnsLinearOnOneLongLine(t *testing.T) {
	t.Parallel()
	const matches = MaxMatches
	// "éx" repeated: every other rune is the match, and every rune is two bytes
	// or one, so byte offsets and columns differ.
	line := strings.Repeat("éx", matches)
	lineRunes := 2 * matches
	text := "head\n" + line + "\ntail"
	for name, snap := range map[string]func() *buffer.Snapshot{
		"memory": func() *buffer.Snapshot { return buffer.New(text).Snapshot() },
		"large":  func() *buffer.Snapshot { return bigLineSnapshot(t, text).Snapshot() },
	} {
		t.Run(name, func(t *testing.T) {
			sc, err := NewScan(snap(), Query{Text: "x", CaseSensitive: true}, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer sc.Close()
			for done := false; !done; {
				if done, err = sc.Step(time.Minute); err != nil {
					t.Fatal(err)
				}
			}
			got := sc.Matches()
			if len(got) != matches || sc.Capped() {
				t.Fatalf("%d matches, capped %v; want %d and not capped", len(got), sc.Capped(), matches)
			}
			if first, last := got[0], got[len(got)-1]; first.Start != (buffer.Pos{Line: 1, Col: 1}) ||
				last.End != (buffer.Pos{Line: 1, Col: lineRunes}) || last.Start.Col != lineRunes-1 {
				t.Errorf("first %v, last %v", first, last)
			}
			if st := sc.Stats(); st.RunesCounted > 2*int64(lineRunes) {
				t.Errorf("RunesCounted = %d for %d matches on a line of %d runes; want at most %d",
					st.RunesCounted, matches, lineRunes, 2*lineRunes)
			}
		})
	}
}

// bigLineSnapshot opens text as a large buffer whose line cap is above its longest
// line.
func bigLineSnapshot(t *testing.T, text string) *buffer.Buffer {
	t.Helper()
	b, err := buffer.OpenLarge(buffertest.NewMemSource([]byte(text)), buffer.LargeOptions{Chunk: 1 << 16, MaxLine: 1 << 20, InitialBytes: 1 << 16})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	for !b.Index(0).Complete {
		sc := b.BeginIndexScan()
		batch, err := sc.Next(context.Background(), 1<<22)
		sc.Close()
		if err != nil {
			t.Fatal(err)
		}
		b.ApplyIndex(batch)
	}
	return b
}

func TestScanPartialFlag(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("y", 200) // over the test line cap of 64
	t.Run("huge line", func(t *testing.T) {
		// The huge line is searched in full: its last byte is a match.
		b, _ := largeBuffer(t, "a\n"+long+"z\nb")
		sc, err := NewScan(b.Snapshot(), Query{Text: "z"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer sc.Close()
		if done, err := sc.Step(time.Hour); !done || err != nil {
			t.Fatalf("Step = %v, %v; want the scan done", done, err)
		}
		if want := []Match{{Start: buffer.Pos{Line: 1, Col: 200}, End: buffer.Pos{Line: 1, Col: 201}}}; !equalMatches(sc.Matches(), want) {
			t.Errorf("matches = %v, want %v", sc.Matches(), want)
		}
		if sc.Partial() {
			t.Errorf("Partial = true; the index of this document is complete")
		}
	})
	t.Run("short lines", func(t *testing.T) {
		b, _ := largeBuffer(t, "a\nbb\nccc")
		sc, err := NewScan(b.Snapshot(), Query{Text: "a"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer sc.Close()
		if sc.Partial() {
			t.Errorf("Partial = true, want false")
		}
	})
	t.Run("memory", func(t *testing.T) {
		sc, err := NewScan(buffer.New(long).Snapshot(), Query{Text: "y"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer sc.Close()
		if sc.Partial() {
			t.Errorf("a memory buffer: Partial = true, want false")
		}
	})
	t.Run("index not complete", func(t *testing.T) {
		text := numbered(300, 280)
		b, err := buffer.OpenLarge(buffertest.NewMemSource([]byte(text)), buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = b.Close() }()
		if b.Index(0).Complete {
			t.Fatal("setup: the index is complete")
		}
		sc, err := NewScan(b.Snapshot(), Query{Text: "needle"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer sc.Close()
		if !sc.Partial() {
			t.Error("Partial = false for a snapshot of an incomplete index")
		}
		if done, err := sc.Step(time.Hour); !done || err != nil || len(sc.Matches()) != 0 {
			t.Errorf("Step = %v, %v with %d matches; the needle is past the indexed part", done, err, len(sc.Matches()))
		}
	})
}

func TestScanReportsFailedRead(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	b, src := largeBuffer(t, numbered(200, 150))
	sc, err := newScan(b.Snapshot(), Query{Text: "needle"}, 0, 64, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	src.FailNext(1_000_000, boom)
	done, err := sc.Step(time.Hour)
	if err == nil || !done {
		t.Fatalf("Step over a failing source = %v, %v; want done with an error", done, err)
	}
	if sc.Progress() == 1 {
		t.Errorf("a failed scan reports progress 1")
	}
	if done, err2 := sc.Step(0); !done || err2 == nil {
		t.Errorf("Step after the failure = %v, %v; want the same error", done, err2)
	}
}

func TestFirstLines(t *testing.T) {
	t.Parallel()
	tests := []struct {
		text string
		n    int
		want string
	}{
		{"a\nb\nc", 1, "a"},
		{"a\nb\nc", 2, "a\nb"},
		{"a\nb\nc", 3, "a\nb\nc"},
		{"a\nb\nc", 9, "a\nb\nc"},
		{"\n\nc", 2, "\n"},
		{"abc", 1, "abc"},
	}
	for _, tt := range tests {
		if got := firstLines(tt.text, tt.n); got != tt.want {
			t.Errorf("firstLines(%q, %d) = %q, want %q", tt.text, tt.n, got, tt.want)
		}
	}
}

// A scan runs on another goroutine while the buffer is edited and indexed on
// this one. Run with -race.
func TestScanConcurrentWithEditsAndIndex(t *testing.T) {
	t.Parallel()
	src := buffertest.NewMemSource([]byte(numbered(600, 40)))
	b, err := buffer.OpenLarge(src, buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	for !b.Index(0).Complete { // a scan of an incomplete index is partial; wait for it
		sc := b.BeginIndexScan()
		batch, err := sc.Next(context.Background(), 1<<20)
		sc.Close()
		if err != nil {
			t.Fatal(err)
		}
		b.ApplyIndex(batch)
	}
	sc, err := newScan(b.Snapshot(), Query{Text: "line 0", CaseSensitive: true}, 300, 40, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()

	type result struct {
		matches []Match
		err     error
	}
	done := make(chan result, 1)
	go func() {
		for {
			finished, err := sc.Step(0)
			if err != nil || finished {
				done <- result{sc.Matches(), err}
				return
			}
		}
	}()
	for i := range 200 {
		b.Insert(buffer.Pos{Line: i % 50}, "line 0")
		b.Delete(buffer.Pos{Line: i % 50}, buffer.Pos{Line: i % 50, Col: 6})
	}
	got := <-done
	if got.err != nil {
		t.Fatalf("Scan: %v", got.err)
	}
	want, err := Find(buffer.New(numbered(600, 40)), Query{Text: "line 0", CaseSensitive: true})
	if err != nil {
		t.Fatal(err)
	}
	if !equalMatches(got.matches, want) {
		t.Errorf("%d matches during edits, want the %d of the text the scan began on", len(got.matches), len(want))
	}
}
