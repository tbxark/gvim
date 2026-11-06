package buffer

import "testing"

func TestVersionTracksContent(t *testing.T) {
	t.Parallel()
	b := New("abc")
	v0 := b.Version()
	if b.Version() != v0 {
		t.Fatal("Version changed without an edit")
	}
	b.Insert(Pos{}, "x")
	v1 := b.Version()
	if v1 == v0 {
		t.Fatal("Version did not change after an insert")
	}
	b.Break()
	b.Delete(Pos{}, Pos{Col: 1})
	if b.Version() == v1 || b.Version() == v0 {
		t.Fatalf("Version after a second edit = %d, want a new value", b.Version())
	}
	b.Undo()
	if b.Version() != v1 {
		t.Fatalf("Version after undo = %d, want %d, the version of the text it restored", b.Version(), v1)
	}
	b.Insert(Pos{}, "y")
	if b.Version() == v0 || b.Version() == v1 {
		t.Fatalf("Version after an edit following an undo = %d, want a new value", b.Version())
	}
}
