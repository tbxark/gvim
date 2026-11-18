package fuzzy

import (
	"fmt"
	"testing"
)

func BenchmarkMatcherRank10kPaths(b *testing.B) {
	paths := make([]string, 10_000)
	for i := range paths {
		paths[i] = fmt.Sprintf("internal/tui/editorpane/component_%04d/handler.go", i)
	}
	m := NewMatcher("edhan")
	b.ReportAllocs()
	for b.Loop() {
		matched := 0
		for _, p := range paths {
			if _, _, ok := m.Match(p); ok {
				matched++
			}
		}
		if matched == 0 {
			b.Fatal("expected matches")
		}
	}
}
