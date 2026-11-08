package buffer

import (
	"strings"
	"testing"
)

// A memory snapshot keeps the text it was taken at, whatever edits follow, those that are made in
// place once it is released included.
func TestSnapshotKeepsItsTextThroughEdits(t *testing.T) {
	t.Parallel()
	edits := []struct {
		name string
		do   func(b *Buffer)
	}{
		{"type at the end", func(b *Buffer) { b.Insert(Pos{Line: 1, Col: 9}, "xyz") }},
		{"type inside", func(b *Buffer) { b.Insert(Pos{Line: 1, Col: 3}, "xyz") }},
		{"delete at the end", func(b *Buffer) { b.Delete(Pos{Line: 1, Col: 6}, Pos{Line: 1, Col: 9}) }},
		{"delete inside", func(b *Buffer) { b.Delete(Pos{Line: 1, Col: 2}, Pos{Line: 1, Col: 4}) }},
		{"delete then type", func(b *Buffer) {
			b.Delete(Pos{Line: 1, Col: 6}, Pos{Line: 1, Col: 9})
			b.Insert(Pos{Line: 1, Col: 6}, "QQQ")
		}},
		{"undo", func(b *Buffer) { b.Insert(Pos{Line: 1, Col: 9}, "xyz"); b.Undo() }},
	}
	for _, tt := range edits {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := New("first\nlongline!\nlast")
			snap := b.Snapshot()
			tt.do(b)
			if got := snap.Line(1); got != "longline!" {
				t.Fatalf("the snapshot's line is %q after the edit", got)
			}
			snap.Release()
			snap.Release() // a second release changes nothing
			b.Insert(Pos{Line: 1, Col: 0}, "after release ")
			if got := b.Line(1); !strings.HasPrefix(got, "after release ") {
				t.Fatalf("line after the release = %q", got)
			}
			if n := b.snaps.Load(); n != 0 {
				t.Fatalf("%d snapshots counted after the release", n)
			}
		})
	}
}
