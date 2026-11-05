package buffer

import "testing"

func TestGroupUndoRevertsReplace(t *testing.T) {
	t.Parallel()
	b := New("hello world")
	b.Group(func() {
		b.Delete(Pos{0, 6}, Pos{0, 11})
		b.Insert(Pos{0, 6}, "there")
	})
	if got := b.Text(); got != "hello there" {
		t.Fatalf("after replace: %q", got)
	}
	if p, ok := b.Undo(); !ok || p != (Pos{0, 6}) {
		t.Fatalf("Undo = %+v, %v; want (0,6), true", p, ok)
	}
	if got := b.Text(); got != "hello world" {
		t.Fatalf("after undo: %q", got)
	}
	if _, ok := b.Undo(); ok {
		t.Fatal("replace must be a single undo step")
	}
}

func TestGroupEmptyRangeIsPureInsert(t *testing.T) {
	t.Parallel()
	b := New("abc")
	b.Group(func() {
		if got := b.Delete(Pos{0, 1}, Pos{0, 1}); got != "" {
			t.Fatalf("empty-range Delete returned %q", got)
		}
		b.Insert(Pos{0, 1}, "XY")
	})
	if got := b.Text(); got != "aXYbc" {
		t.Fatalf("after pure insert: %q", got)
	}
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo should revert the insert")
	}
	if got := b.Text(); got != "abc" {
		t.Fatalf("after undo: %q", got)
	}
	if _, ok := b.Undo(); ok {
		t.Fatal("empty delete should add nothing to the group")
	}

	// A group whose only edit is an empty-range delete records nothing.
	empty := New("abc")
	empty.Group(func() {
		empty.Delete(Pos{0, 2}, Pos{0, 2})
	})
	if _, ok := empty.Undo(); ok {
		t.Fatal("group with no real edit should not add an undo step")
	}
	if empty.Modified() {
		t.Fatal("group with no real edit should not modify the buffer")
	}
}

func TestGroupNestsIntoOneStep(t *testing.T) {
	t.Parallel()
	b := New("")
	b.Group(func() {
		b.Insert(Pos{}, "a")
		b.Group(func() {
			b.Insert(Pos{0, 1}, "b")
			b.Group(func() {
				b.Insert(Pos{0, 2}, "c")
			})
		})
		b.Insert(Pos{0, 3}, "d")
	})
	if got := b.Text(); got != "abcd" {
		t.Fatalf("after nested groups: %q", got)
	}
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo should succeed")
	}
	if got := b.Text(); got != "" {
		t.Fatalf("one Undo should revert all nested edits, got %q", got)
	}
	if _, ok := b.Undo(); ok {
		t.Fatal("nested groups must form exactly one undo step")
	}
	if _, ok := b.Redo(); !ok {
		t.Fatal("Redo should succeed")
	}
	if got := b.Text(); got != "abcd" {
		t.Fatalf("after redo of nested groups: %q", got)
	}
}

func TestGroupDoesNotMergeWithNeighbors(t *testing.T) {
	t.Parallel()
	b := New("")
	b.Insert(Pos{}, "a")
	b.Group(func() {
		b.Insert(Pos{0, 1}, "b")
	})
	b.Insert(Pos{0, 2}, "c")
	steps := []string{"ab", "a", ""}
	b.Undo() // reverts "c"
	for _, want := range steps {
		if got := b.Text(); got != want {
			t.Fatalf("undo step text = %q, want %q", got, want)
		}
		b.Undo()
	}
}

func TestGroupRedoAfterUndo(t *testing.T) {
	t.Parallel()
	b := New("one two")
	b.Group(func() {
		b.Delete(Pos{0, 0}, Pos{0, 3})
		b.Insert(Pos{0, 0}, "uno")
	})
	if got := b.Text(); got != "uno two" {
		t.Fatalf("after replace: %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "one two" {
		t.Fatalf("after undo: %q", got)
	}
	if p, ok := b.Redo(); !ok || p != (Pos{0, 3}) {
		t.Fatalf("Redo = %+v, %v; want (0,3), true", p, ok)
	}
	if got := b.Text(); got != "uno two" {
		t.Fatalf("after redo: %q", got)
	}
	if _, ok := b.Redo(); ok {
		t.Fatal("redo with nothing undone should report false")
	}
	b.Undo()
	if got := b.Text(); got != "one two" {
		t.Fatalf("second undo: %q", got)
	}
	b.Redo()
	if got := b.Text(); got != "uno two" {
		t.Fatalf("second redo: %q", got)
	}
}

func TestGroupNewEditAfterUndoClearsRedo(t *testing.T) {
	t.Parallel()
	b := New("ab")
	b.Group(func() {
		b.Delete(Pos{0, 0}, Pos{0, 1})
		b.Insert(Pos{0, 0}, "X")
	})
	b.Undo()
	b.Insert(Pos{0, 2}, "!")
	if _, ok := b.Redo(); ok {
		t.Fatal("a new edit should clear the redo of an undone group")
	}
}

func TestGroupMultiLineReplace(t *testing.T) {
	t.Parallel()
	b := New("one\ntwo\nthree")
	b.Group(func() {
		b.Delete(Pos{0, 1}, Pos{2, 3})
		b.Insert(Pos{0, 1}, "X\nY")
	})
	if got := b.Text(); got != "oX\nYee" {
		t.Fatalf("after multi-line replace: %q", got)
	}
	if b.Lines() != 2 {
		t.Fatalf("Lines() = %d, want 2", b.Lines())
	}
	if p, ok := b.Undo(); !ok || p != (Pos{0, 1}) {
		t.Fatalf("Undo = %+v, %v; want (0,1), true", p, ok)
	}
	if got := b.Text(); got != "one\ntwo\nthree" {
		t.Fatalf("after undo: %q", got)
	}
	if _, ok := b.Redo(); !ok {
		t.Fatal("Redo should succeed")
	}
	if got := b.Text(); got != "oX\nYee" {
		t.Fatalf("after redo: %q", got)
	}
}

func TestGroupModifiedTracksSave(t *testing.T) {
	t.Parallel()
	b := New("x")
	b.Group(func() {
		b.Insert(Pos{0, 1}, "y")
		b.Delete(Pos{0, 0}, Pos{0, 1})
	})
	if !b.Modified() {
		t.Fatal("group edit should mark buffer modified")
	}
	b.Undo()
	if b.Modified() {
		t.Fatal("undo of group back to original content should not be modified")
	}
	b.Redo()
	if !b.Modified() {
		t.Fatal("redo of group should be modified")
	}
	b.MarkSaved()
	b.Undo()
	if !b.Modified() {
		t.Fatal("undo past the saved group state should be modified")
	}
}

func TestUndoAndRedoInsideGroupAreNoOps(t *testing.T) {
	t.Parallel()
	b := New("")
	b.Insert(Pos{}, "a")
	b.Break()
	b.Group(func() {
		b.Insert(Pos{0, 1}, "b")
		if _, ok := b.Undo(); ok {
			t.Error("Undo inside Group should report false")
		}
		if _, ok := b.Redo(); ok {
			t.Error("Redo inside Group should report false")
		}
	})
	if got := b.Text(); got != "ab" {
		t.Fatalf("after group: %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "a" {
		t.Fatalf("undo should revert only the group: %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "" {
		t.Fatalf("second undo: %q", got)
	}
}

func TestGroupPanicKeepsPartialEditAsOneStep(t *testing.T) {
	t.Parallel()
	b := New("abc")
	func() {
		defer func() { _ = recover() }()
		b.Group(func() {
			b.Delete(Pos{0, 0}, Pos{0, 1})
			panic("boom")
		})
	}()
	if got := b.Text(); got != "bc" {
		t.Fatalf("partial group edit should stay in the buffer: %q", got)
	}
	b.Insert(Pos{0, 0}, "z")
	b.Undo()
	if got := b.Text(); got != "bc" {
		t.Fatalf("edit after panic should be its own step: %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "abc" {
		t.Fatalf("partial group should undo as one step: %q", got)
	}
}
