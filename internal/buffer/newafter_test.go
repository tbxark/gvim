package buffer_test

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// NewAfter starts a buffer whose versions are all new to a cache that saw prev, however
// prev's version moved (edits raise it, Undo brings it back to an earlier one).
func TestNewAfterVersionsAreFresh(t *testing.T) {
	edited := buffer.New("a")
	edited.Insert(buffer.Pos{}, "b")
	edited.Break()
	edited.Insert(buffer.Pos{}, "c")
	undone := buffer.New("a")
	undone.Insert(buffer.Pos{}, "b")
	undone.Break()
	undone.Insert(buffer.Pos{}, "c")
	undone.Undo()

	tests := []struct {
		name string
		prev *buffer.Buffer
		max  uint64 // the highest version prev ever had
	}{
		{"fresh buffer (version 0)", buffer.New("a"), 0},
		{"edited", edited, edited.Version()},
		{"undone to an earlier version", undone, undone.Version() + 1},
		{"no previous buffer", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := buffer.NewAfter("text\nmore", tt.prev)
			if tt.prev != nil && b.Version() <= tt.max {
				t.Errorf("version = %d, want above %d", b.Version(), tt.max)
			}
			if b.Text() != "text\nmore" || b.Modified() {
				t.Errorf("text = %q modified = %v", b.Text(), b.Modified())
			}
			if _, ok := b.Undo(); ok {
				t.Error("a new buffer has undo history")
			}
			before := b.Version()
			b.Insert(buffer.Pos{}, "x")
			if b.Version() <= before || !b.Modified() {
				t.Errorf("edit: version %d -> %d modified = %v", before, b.Version(), b.Modified())
			}
			b.Undo()
			if b.Version() != before || b.Modified() {
				t.Errorf("undo: version = %d (want %d) modified = %v", b.Version(), before, b.Modified())
			}
		})
	}
}
