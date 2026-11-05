package finder

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestRankerRecordsOpensAndRanksByFrecency(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	r := NewRanker(func() time.Time { return now })
	r.Record("x/foo.go")
	r.Record("y/foo.go")
	r.Record("y/foo.go")
	got := paths(r.Rank("foo", []string{"x/foo.go", "y/foo.go", "z/foo.go"}, 0))
	want := []string{"y/foo.go", "x/foo.go", "z/foo.go"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Rank = %v, want %v", got, want)
	}
}

func TestRankerLimit(t *testing.T) {
	files := make([]string, 300)
	for i := range files {
		files[i] = fmt.Sprintf("f%03d.go", i)
	}
	r := NewRanker(nil)
	tests := []struct {
		limit, want int
	}{
		{5, 5},
		{0, MaxResults},
		{-3, MaxResults},
		{MaxResults + 50, MaxResults},
	}
	for _, tt := range tests {
		if got := len(r.Rank("f", files, tt.limit)); got != tt.want {
			t.Errorf("limit %d: len = %d, want %d", tt.limit, got, tt.want)
		}
	}
}

// TestRankerFixtureOrder pins the order of a small project for a query that
// mixes a literal prefix, a basename match, a camel-case match and a scattered
// match.
func TestRankerFixtureOrder(t *testing.T) {
	files := []string{
		"internal/app/run.go",
		"internal/app/run_test.go",
		"internal/finder/finder.go",
		"internal/finder/score.go",
		"cmd/runner/main.go",
		"docs/rerun-notes.md",
		"internal/ui/palette/palette.go",
	}
	r := NewRanker(nil)
	got := paths(r.Rank("run", files, 0))
	want := []string{
		"internal/app/run.go",
		"internal/app/run_test.go",
		"cmd/runner/main.go",
		"docs/rerun-notes.md",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Rank(\"run\") = %v\nwant           %v", got, want)
	}
}

func TestRankerIsSafeForConcurrentUse(t *testing.T) {
	r := NewRanker(nil)
	files := []string{"a/x.go", "b/x.go", "c/y.go"}
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 50 {
				r.Record(files[i%len(files)])
			}
		}()
		go func() {
			defer wg.Done()
			for range 50 {
				_ = r.Rank("x", files, 10)
			}
		}()
	}
	wg.Wait()
}
