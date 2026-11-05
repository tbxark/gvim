package finder

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// paths returns the Path of each match, in order.
func paths(ms []Match) []string {
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Path
	}
	return out
}

func TestRankOrderingTable(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		files []string
		query string
		usage Usage
		want  []string
	}{
		{
			name:  "exact name beats the same name deeper in the tree",
			files: []string{"cmd/tools/main.go", "main.go"},
			query: "main.go",
			want:  []string{"main.go", "cmd/tools/main.go"},
		},
		{
			name:  "literal path prefix gets the 200 boost",
			files: []string{"lib/src.go", "src/app.go"},
			query: "src",
			want:  []string{"src/app.go", "lib/src.go"},
		},
		{
			name:  "frecency breaks a tie",
			files: []string{"x/foo.go", "y/foo.go"},
			query: "foo",
			usage: Usage{"y/foo.go": {Count: 3, Last: now}},
			want:  []string{"y/foo.go", "x/foo.go"},
		},
		{
			name:  "no tie-break without usage keeps file order",
			files: []string{"x/foo.go", "y/foo.go"},
			query: "foo",
			want:  []string{"x/foo.go", "y/foo.go"},
		},
		{
			name:  "non-matching files are dropped",
			files: []string{"a.txt", "b.go"},
			query: "go",
			want:  []string{"b.go"},
		},
		{
			name:  "empty query orders by frecency, then file order",
			files: []string{"c.txt", "a.txt", "b.txt"},
			query: "",
			usage: Usage{"b.txt": {Count: 2, Last: now}, "a.txt": {Count: 1, Last: now}},
			want:  []string{"b.txt", "a.txt", "c.txt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usage := tc.usage
			if usage == nil {
				usage = Usage{}
			}
			got := paths(Rank(tc.files, tc.query, usage, now))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Rank = %v\nwant     %v", got, tc.want)
			}
		})
	}
}

func TestRankCapsAtMaxResults(t *testing.T) {
	files := make([]string, 250)
	for i := range files {
		files[i] = fmt.Sprintf("f%03d.go", i)
	}
	if got := Rank(files, "f", Usage{}, time.Now()); len(got) != MaxResults {
		t.Fatalf("len = %d, want %d", len(got), MaxResults)
	}
}

func TestRankPositions(t *testing.T) {
	got := Rank([]string{"main.go"}, "go", Usage{}, time.Now())
	if len(got) != 1 || !reflect.DeepEqual(got[0].Positions, []int{5, 6}) {
		t.Fatalf("Rank positions = %+v, want [5 6]", got)
	}
}

func TestRawFrecencyTiersAndCap(t *testing.T) {
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		u    Use
		want int
	}{
		{"never opened", Use{}, 0},
		{"under four hours counts fully", Use{Count: 1, Last: now.Add(-time.Hour)}, 100},
		{"under a day", Use{Count: 3, Last: now.Add(-10 * time.Hour)}, 210},
		{"under a week", Use{Count: 2, Last: now.Add(-48 * time.Hour)}, 100},
		{"under a month", Use{Count: 2, Last: now.Add(-10 * 24 * time.Hour)}, 60},
		{"under three months", Use{Count: 2, Last: now.Add(-40 * 24 * time.Hour)}, 20},
		{"older than three months", Use{Count: 2, Last: now.Add(-200 * 24 * time.Hour)}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := rawFrecency(tc.u, now); got != tc.want {
				t.Fatalf("rawFrecency = %d, want %d", got, tc.want)
			}
		})
	}
	if got := frecencyBoost(Use{Count: 50, Last: now}, now); got != frecencyCap {
		t.Fatalf("frecencyBoost cap = %d, want %d", got, frecencyCap)
	}
}

func TestUsageRecord(t *testing.T) {
	u := Usage{}
	now := time.Now()
	u.Record("a.go", now)
	u.Record("a.go", now.Add(time.Second))
	if got := u["a.go"]; got.Count != 2 || !got.Last.Equal(now.Add(time.Second)) {
		t.Fatalf("after two records = %+v", got)
	}
}
