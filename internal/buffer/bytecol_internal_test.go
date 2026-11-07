package buffer

import (
	"strings"
	"testing"
)

func TestByteOffsetKeepsPrefixSumsAboveAnEdit(t *testing.T) {
	t.Parallel()
	b := New(strings.Repeat("line é\n", 100) + "end")
	if _, err := b.ByteOffset(Pos{Line: 100}); err != nil {
		t.Fatal(err)
	}
	if b.startsValid != 101 {
		t.Fatalf("startsValid after the query = %d, want 101", b.startsValid)
	}
	steps := []struct {
		name string
		do   func()
		want int
	}{
		{"insert on line 90", func() { b.Insert(Pos{Line: 90, Col: 1}, "x") }, 91},
		{"insert on line 95", func() { b.Insert(Pos{Line: 95}, "x") }, 91},
		{"delete from line 40", func() { b.Delete(Pos{Line: 40, Col: 2}, Pos{Line: 41, Col: 1}) }, 41},
		{"undo", func() { b.Undo() }, 41},
	}
	for _, s := range steps {
		s.do()
		if b.startsValid != s.want {
			t.Errorf("%s: startsValid = %d, want %d", s.name, b.startsValid, s.want)
		}
	}
	want := len(b.Range(Pos{}, Pos{Line: 100}))
	if got, _ := b.ByteOffset(Pos{Line: 100}); got != want {
		t.Errorf("offset after edits = %d, want %d", got, want)
	}
}

func TestByteColWalkerMatchesByteCol(t *testing.T) {
	t.Parallel()
	b := New("héllo wörld\n\nplain\n日本語 text é")
	var ps []Pos
	for l := -1; l <= b.Lines(); l++ {
		for c := -1; c < 14; c++ {
			ps = append(ps, Pos{Line: l, Col: c})
		}
	}
	// Ascending, descending, and interleaved lines, with repeats.
	orders := map[string][]Pos{"ascending": ps}
	rev := make([]Pos, len(ps))
	for i, p := range ps {
		rev[len(ps)-1-i] = p
	}
	orders["descending"] = rev
	var mixed []Pos
	for i := range ps {
		mixed = append(mixed, ps[i], ps[(i*7)%len(ps)], ps[i])
	}
	orders["mixed"] = mixed
	for name, order := range orders {
		w := b.NewByteColWalker()
		for _, p := range order {
			if got, want := w.ByteCol(p), b.ByteCol(p); got != want {
				t.Fatalf("%s: ByteCol(%v) = %d, want %d", name, p, got, want)
			}
		}
	}
}
