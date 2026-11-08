package search

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/buffer"
)

// hugeFixture is a document whose line 1 is longer than the line cap of largeBuffer (64
// bytes), with the needle at many columns of it, and normal lines before and after it.
func hugeFixture() string {
	var sb strings.Builder
	sb.WriteString("alpha needle\nbeta\n")
	for i := range 40 {
		fmt.Fprintf(&sb, "w%02d needle ", i)
	}
	sb.WriteString("tail needle\n")
	sb.WriteString("gamma needle\nend\n")
	return sb.String()
}

// The scan of a document with a huge line finds every match the memory search finds, at the
// same positions, however the line is cut into windows. A margin of at least the needle's length
// keeps a match that crosses a window edge whole.
func TestScanFindsEveryMatchOfAHugeLine(t *testing.T) {
	t.Parallel()
	text := hugeFixture()
	q := Query{Text: "needle"}
	want, err := Find(buffer.New(text), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) < 40 {
		t.Fatalf("the memory search found %d matches; the fixture has more", len(want))
	}
	b, _ := largeBuffer(t, text)
	for _, span := range []hugeSpan{defaultHuge, {window: 37, margin: 8}, {window: 1, margin: 6}} {
		sc, err := newScan(b.Snapshot(), q, 0, 1<<20, MaxMatches)
		if err != nil {
			t.Fatal(err)
		}
		sc.huge = span
		for done := false; !done; {
			if done, err = sc.Step(time.Minute); err != nil {
				t.Fatal(err)
			}
		}
		if got := sc.Matches(); !equalMatches(got, want) {
			t.Errorf("span %+v: %d matches %v, want the %d of the memory search", span, len(got), got, len(want))
		}
		sc.Close()
	}
}

// A match past the line cap of a large buffer is found, and reported at the column the line
// really has it, even when every slice of time ends before the scan does.
func TestScanReportsTheColumnOfAMatchPastTheLineCap(t *testing.T) {
	t.Parallel()
	line := strings.Repeat("0123456789", 30) + "NEEDLE" + strings.Repeat("9876543210", 20)
	b, _ := largeBuffer(t, "head\n"+line+"\n")
	sc, err := newScan(b.Snapshot(), Query{Text: "NEEDLE"}, 0, 1<<20, MaxMatches)
	if err != nil {
		t.Fatal(err)
	}
	defer sc.Close()
	for done := false; !done; {
		if done, err = sc.Step(0); err != nil {
			t.Fatal(err)
		}
	}
	want := []Match{{Start: buffer.Pos{Line: 1, Col: 300}, End: buffer.Pos{Line: 1, Col: 306}}}
	if got := sc.Matches(); !equalMatches(got, want) {
		t.Fatalf("matches = %v, want %v", got, want)
	}
}

// FindNext over a document with a huge line goes to the same match as the memory search
// from every position, forward and backward, when it is given a budget too small for the whole
// line and is asked again from where it stopped.
func TestFindNextOverAHugeLineResumesToTheSameMatch(t *testing.T) {
	t.Parallel()
	text := hugeFixture()
	q := Query{Text: "needle"}
	mem := buffer.New(text).Snapshot()
	defer mem.Release()
	b, _ := largeBuffer(t, text)
	snap := b.Snapshot()
	defer snap.Release()
	froms := []buffer.Pos{
		{}, {Line: 1}, {Line: 1, Col: 1}, {Line: 1, Col: 6}, {Line: 1, Col: 7}, {Line: 1, Col: 70},
		{Line: 1, Col: 300}, {Line: 1, Col: 540}, {Line: 1, Col: 2000}, {Line: 2}, {Line: 3, Col: 4}, {Line: 9},
	}
	for _, from := range froms {
		for _, dir := range []Dir{Forward, Backward} {
			wantM, wantFound, _, err := FindNext(mem, q, from, dir, 0)
			if err != nil {
				t.Fatal(err)
			}
			m, found, scanned := budgetedFindNext(t, snap, q, from, dir, 40)
			if found != wantFound || (found && m != wantM) || (!found && !scanned) {
				t.Errorf("from %v dir %v: got %v found %v scanned %v, want %v found %v",
					from, dir, m, found, scanned, wantM, wantFound)
			}
		}
	}
}

// With windows of a few runes, a search from every fifth column of the huge line, forward and
// backward, gives the memory search's answer: the windows never lose or repeat a match.
func TestFindNextOverAHugeLineAgreesFromEveryColumn(t *testing.T) {
	t.Parallel()
	text := hugeFixture()
	q := Query{Text: "needle"}
	mem := buffer.New(text).Snapshot()
	defer mem.Release()
	b, _ := largeBuffer(t, text)
	snap := b.Snapshot()
	defer snap.Release()
	span := hugeSpan{window: 8, margin: 6}
	for col := 0; col <= 460; col += 5 {
		from := buffer.Pos{Line: 1, Col: col}
		for _, dir := range []Dir{Forward, Backward} {
			wantM, wantFound, _, err := FindNext(mem, q, from, dir, 0)
			if err != nil {
				t.Fatal(err)
			}
			m, found, scanned := budgetedFindNextSpan(t, snap, q, from, dir, 64, span)
			if found != wantFound || (found && m != wantM) || (!found && !scanned) {
				t.Fatalf("from %v dir %v: got %v found %v scanned %v, want %v found %v",
					from, dir, m, found, scanned, wantM, wantFound)
			}
		}
	}
}

// budgetedFindNext is budgetedFindNextSpan with the span of the search.
func budgetedFindNext(t *testing.T, s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, budget int64) (Match, bool, bool) {
	t.Helper()
	return budgetedFindNextSpan(t, s, q, from, dir, budget, hugeSpan{window: 37, margin: 8})
}

// budgetedFindNextSpan asks findNextSpan over the windows span until the search has an answer.
func budgetedFindNextSpan(t *testing.T, s *buffer.Snapshot, q Query, from buffer.Pos, dir Dir, budget int64, span hugeSpan) (Match, bool, bool) {
	t.Helper()
	var at Cursor
	for range 100000 {
		m, found, scanned, next, err := findNextSpan(s, q, from, dir, budget, nextWindow, span, at)
		if err != nil {
			t.Fatal(err)
		}
		if found || scanned {
			return m, found, scanned
		}
		at = next
	}
	t.Fatal("the search did not end")
	return Match{}, false, false
}

// A snapshot of a document with a huge line is searched on another goroutine while this one
// reads the line through the buffer, which builds the line's index into the memo that the
// snapshot shares. Run with -race.
func TestHugeLineSearchConcurrentWithTheBuffer(t *testing.T) {
	t.Parallel()
	b, _ := largeBuffer(t, hugeFixture())
	snap := b.Snapshot() // the scan owns it, and Close releases it
	done := make(chan error, 1)
	go func() {
		sc, err := newScan(snap, Query{Text: "needle"}, 0, 1<<20, MaxMatches)
		if err != nil {
			done <- err
			return
		}
		defer sc.Close()
		for ended := false; !ended; {
			if ended, err = sc.Step(time.Minute); err != nil {
				done <- err
				return
			}
		}
		if n := len(sc.Matches()); n < 40 {
			done <- fmt.Errorf("the scan found %d matches, want at least 40", n)
			return
		}
		done <- nil
	}()
	for range 200 {
		_ = b.LineLen(1)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
