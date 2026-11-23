package search

import (
	"strings"
	"testing"
)

func BenchmarkCompileQueryFindStringMiss(b *testing.B) {
	text := strings.Repeat("func renderFrame(ctx Context, width int) error {\n\treturn nil\n}\n", 200)
	cq, err := CompileQuery(Query{Text: "definitelyNotInFile", CaseSensitive: true})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if got := cq.FindString(text); len(got) != 0 {
			b.Fatalf("got %d matches", len(got))
		}
	}
}

func BenchmarkCompileQueryFindStringHit(b *testing.B) {
	text := strings.Repeat("func renderFrame(ctx Context, width int) error {\n\treturn nil\n}\n", 200)
	cq, err := CompileQuery(Query{Text: "renderFrame", CaseSensitive: true})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if got := cq.FindString(text); len(got) == 0 {
			b.Fatal("expected matches")
		}
	}
}

func BenchmarkCompileQueryFindStringLimit(b *testing.B) {
	text := strings.Repeat("func renderFrame(ctx Context, width int) error {\n\treturn nil\n}\n", 200)
	cq, err := CompileQuery(Query{Text: "renderFrame", CaseSensitive: true})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if got := cq.FindStringLimit(text, 10); len(got) != 10 {
			b.Fatalf("got %d matches, want 10", len(got))
		}
	}
}
