package finder

import (
	"fmt"
	"testing"
	"time"
)

func BenchmarkRank10kPaths(b *testing.B) {
	paths := make([]string, 10_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("internal/tui/editorpane/component_%04d/handler.go", i)
	}
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	usage := Usage{}
	b.ReportAllocs()
	for b.Loop() {
		got := Rank(paths, "edhan", usage, now)
		if len(got) != MaxResults {
			b.Fatalf("len = %d, want %d", len(got), MaxResults)
		}
	}
}
