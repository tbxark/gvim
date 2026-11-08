package pattern

import "testing"

// The speed floor of the spec (AMENDMENTS C6) for a 10 MiB subject:
// Find("TODO") <= 20 ms, Find("%s+$") <= 150 ms. Run with
//
//	go test ./internal/luapat -bench . -benchtime 5x
func BenchmarkFindTODO10MiB(b *testing.B) {
	s := wordsSubject(10<<20, true)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		if _, ok, err := Find(s, "TODO", 1, false, Options{}); !ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkFindSpaceRunAtEnd10MiB(b *testing.B) {
	s := wordsSubject(10<<20, false)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		if _, ok, err := Find(s, "%s+$", 1, false, Options{}); ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkFindIdentifierCall10MiB(b *testing.B) {
	s := wordsSubject(10<<20, false)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		if _, ok, err := Find(s, "[%a_][%w_]*%(", 1, false, Options{}); ok || err != nil {
			b.Fatal(ok, err)
		}
	}
}

func BenchmarkGsubLiteral10MiB(b *testing.B) {
	s := wordsSubject(10<<20, false)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		if _, _, err := Gsub(s, "ipsum", Replacement{Template: "IPSUM"}, Unlimited, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGmatchWords10MiB(b *testing.B) {
	s := wordsSubject(10<<20, false)
	b.SetBytes(int64(len(s)))
	for b.Loop() {
		it, err := GMatch(s, "%a+", Options{})
		if err != nil {
			b.Fatal(err)
		}
		for {
			_, ok, err := it.Next()
			if err != nil {
				b.Fatal(err)
			}
			if !ok {
				break
			}
		}
	}
}
