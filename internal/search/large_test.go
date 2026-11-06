package search

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
	"github.com/tbxark/gvim/internal/buffer/buffertest"
)

// largeBuffer opens text as a large buffer with tiny chunks and a complete
// index, over a counting source.
func largeBuffer(t testing.TB, text string) (*buffer.Buffer, *buffertest.MemSource) {
	t.Helper()
	src := buffertest.NewMemSource([]byte(text))
	b, err := buffer.OpenLarge(src, buffer.LargeOptions{Chunk: 16, CachePages: 2, MaxLine: 64, InitialBytes: 32})
	if err != nil {
		t.Fatalf("OpenLarge: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	for !b.Index(0).Complete {
		sc := b.BeginIndexScan()
		if sc == nil {
			t.Fatalf("no index scan with an incomplete index (err %v)", b.SourceErr())
		}
		batch, err := sc.Next(context.Background(), 1<<20)
		sc.Close()
		if err != nil {
			t.Fatalf("index scan: %v", err)
		}
		b.ApplyIndex(batch)
	}
	return b, src
}

func TestLargeBufferIsRefused(t *testing.T) {
	t.Parallel()
	text := "foo bar\nfoo baz\nqux"
	queries := []Query{
		{Text: "foo"},
		{Text: "foo", Regex: true},
		{Text: ""}, // an empty query is refused too: the caller must not search a large buffer at all
	}
	for _, q := range queries {
		t.Run(fmt.Sprintf("%+v", q), func(t *testing.T) {
			t.Parallel()
			b, src := largeBuffer(t, text)
			before, ver := src.BytesRead(), b.Version()
			decided := false
			calls := map[string]func() error{
				"Find": func() error { _, err := Find(b, q); return err },
				"Replace": func() error {
					_, err := Replace(b, q, "x", Match{})
					return err
				},
				"ReplaceAll": func() error { _, err := ReplaceAll(b, q, "x"); return err },
				"QueryReplace": func() error {
					_, err := QueryReplace(b, q, "x", func(Match, int) Decision { decided = true; return DecisionReplace })
					return err
				},
			}
			for name, call := range calls {
				if err := call(); !errors.Is(err, ErrLargeBuffer) {
					t.Errorf("%s error = %v, want ErrLargeBuffer", name, err)
				}
			}
			st := b.Stats()
			if got := src.BytesRead(); got != before {
				t.Errorf("source bytes read grew from %d to %d", before, got)
			}
			if st.OverCap != 0 || st.MaterializedBytes != 0 {
				t.Errorf("OverCap = %d, MaterializedBytes = %d, want 0, 0", st.OverCap, st.MaterializedBytes)
			}
			if b.Version() != ver || b.Modified() {
				t.Errorf("buffer changed: version %d -> %d, modified %v", ver, b.Version(), b.Modified())
			}
			if decided {
				t.Error("QueryReplace asked for a decision")
			}
		})
	}
}

func TestMemoryBufferStillSearched(t *testing.T) {
	t.Parallel()
	b := buffer.New("foo foo")
	got, err := Find(b, Query{Text: "foo"})
	if err != nil || len(got) != 2 {
		t.Fatalf("Find = %v, %v, want 2 matches", got, err)
	}
}

func TestNewWindowPositions(t *testing.T) {
	t.Parallel()
	d := newWindow(40, "ab\ncdé fgh\n\nij")
	tests := []struct {
		off  int
		want buffer.Pos
	}{
		{0, buffer.Pos{Line: 40, Col: 0}},
		{4, buffer.Pos{Line: 41, Col: 1}},
		{9, buffer.Pos{Line: 41, Col: 5}},  // after the two-byte é
		{12, buffer.Pos{Line: 42, Col: 0}}, // the empty line
		{13, buffer.Pos{Line: 43, Col: 0}},
		{15, buffer.Pos{Line: 43, Col: 2}}, // end of the text
	}
	for _, tt := range tests {
		if got := d.pos(tt.off); got != tt.want {
			t.Errorf("pos(%d) = %v, want %v", tt.off, got, tt.want)
		}
	}
}

func TestFindVisibleOffsets(t *testing.T) {
	t.Parallel()
	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("row %02d", i)
	}
	lines[39] = "start foo"
	lines[40] = "bar end"
	lines[42] = "foo"
	lines[44] = "xfoo"
	lines[46] = "foo" // below the rows, inside the context
	lines[30] = "foo" // above the context
	lines[70] = "foo" // below the context
	row := func(i int) string {
		if i < 0 || i >= len(lines) {
			return ""
		}
		return lines[i]
	}
	tests := []struct {
		name        string
		q           Query
		first, last int
		want        []Match
	}{
		{
			name: "visible matches get document line numbers",
			q:    Query{Text: "foo"}, first: 41, last: 45,
			want: []Match{m(42, 0, 42, 3), m(44, 1, 44, 4)},
		},
		{
			name: "multi-line match that starts in the context above is seen",
			q:    Query{Text: "foo\nbar", CaseSensitive: true}, first: 40, last: 44,
			want: []Match{m(39, 6, 40, 3)},
		},
		{
			name: "whole word skips the substring",
			q:    Query{Text: "foo", WholeWord: true}, first: 41, last: 45,
			want: []Match{m(42, 0, 42, 3)},
		},
		{
			name: "regex",
			q:    Query{Text: `o+$`, Regex: true}, first: 42, last: 44,
			want: []Match{m(42, 1, 42, 3), m(44, 2, 44, 4)},
		},
		{
			name: "first row of the document",
			q:    Query{Text: "row 0", CaseSensitive: true}, first: 0, last: 2,
			want: []Match{m(0, 0, 0, 5), m(1, 0, 1, 5), m(2, 0, 2, 5)},
		},
		{
			name: "negative first is the first row",
			q:    Query{Text: "row 0", CaseSensitive: true}, first: -3, last: 0,
			want: []Match{m(0, 0, 0, 5)},
		},
		{name: "first after last", q: Query{Text: "foo"}, first: 5, last: 4},
		{name: "empty query", q: Query{Text: ""}, first: 0, last: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lo, hi := 1<<30, -1
			got, err := FindVisible(func(i int) string {
				lo, hi = min(lo, i), max(hi, i)
				return row(i)
			}, tt.first, tt.last, tt.q)
			if err != nil {
				t.Fatalf("FindVisible: %v", err)
			}
			if !equalMatches(got, tt.want) {
				t.Errorf("matches = %v, want %v", got, tt.want)
			}
			height := tt.last - max(tt.first, 0) + 1
			if hi >= 0 && (lo < max(tt.first, 0)-height || hi > tt.last+height) {
				t.Errorf("rows read %d..%d, outside %d rows of context around %d..%d", lo, hi, height, tt.first, tt.last)
			}
		})
	}
}

func TestFindVisibleInvalidRegex(t *testing.T) {
	t.Parallel()
	_, err := FindVisible(func(int) string { return "x" }, 0, 3, Query{Text: "(", Regex: true})
	if err == nil {
		t.Fatal("FindVisible accepted an invalid regex")
	}
}

func TestFindVisibleAgreesWithFind(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("alpha foo\nbeta Foo bar\n\nfoo_bar foo\n", 10)
	b := buffer.New(text)
	rows := func(i int) string {
		if i < 0 || i >= b.Lines() {
			return ""
		}
		return b.Line(i)
	}
	queries := []Query{
		{Text: "foo"},
		{Text: "foo", CaseSensitive: true},
		{Text: "foo", WholeWord: true},
		{Text: `f(o+)`, Regex: true},
		{Text: `^b`, Regex: true},
	}
	for _, q := range queries {
		all, err := Find(b, q)
		if err != nil {
			t.Fatal(err)
		}
		for first := 0; first < b.Lines(); first += 7 {
			last := min(first+5, b.Lines()-1)
			var want []Match
			for _, mt := range all {
				if mt.End.Line >= first && mt.Start.Line <= last {
					want = append(want, mt)
				}
			}
			got, err := FindVisible(rows, first, last, q)
			if err != nil {
				t.Fatal(err)
			}
			if !equalMatches(got, want) {
				t.Errorf("%+v rows %d..%d: matches = %v, want %v", q, first, last, got, want)
			}
		}
	}
}

func equalMatches(a, b []Match) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
