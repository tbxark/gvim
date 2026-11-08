package buffer

import (
	"testing"
	"time"
)

// stepClock is a manually advanced clock. Its base is aligned to a whole
// second, so a step of less than one second stays in the same undo bucket
// unless the test crosses a boundary on purpose.
type stepClock struct {
	t time.Time
}

func newStepClock() *stepClock {
	return &stepClock{t: time.Date(2026, time.November, 5, 9, 0, 0, 0, time.UTC)}
}

func (c *stepClock) now() time.Time { return c.t }

func (c *stepClock) step(d time.Duration) { c.t = c.t.Add(d) }

func TestTypingWordThenUndoRestores(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	at := Pos{}
	for _, r := range "word" {
		at = b.Insert(at, string(r))
	}
	if got := b.Text(); got != "word" {
		t.Fatalf("after typing: %q", got)
	}
	if p, ok := b.Undo(); !ok || p != (Pos{0, 0}) {
		t.Fatalf("Undo = %+v, %v; want (0,0), true", p, ok)
	}
	if got := b.Text(); got != "" {
		t.Fatalf("after undo: %q", got)
	}
}

func TestClockStepSplitsWords(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)

	at := Pos{}
	for _, r := range "one" {
		at = b.Insert(at, string(r))
	}
	clock.step(1500 * time.Millisecond)
	for _, r := range " two" {
		at = b.Insert(at, string(r))
	}
	if got := b.Text(); got != "one two" {
		t.Fatalf("after typing: %q", got)
	}

	// The second word, with its space, is one step.
	if p, ok := b.Undo(); !ok || p != (Pos{0, 3}) {
		t.Fatalf("Undo = %+v, %v; want (0,3), true", p, ok)
	}
	if got := b.Text(); got != "one" {
		t.Fatalf("after first undo: %q", got)
	}
	b.Undo()
	if got := b.Text(); got != "" {
		t.Fatalf("after second undo: %q", got)
	}
}

func TestEditsInOneBucketUndoTogether(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Insert(Pos{}, "a")
	clock.step(400 * time.Millisecond)
	b.Insert(Pos{0, 1}, "b")
	clock.step(400 * time.Millisecond)
	b.Insert(Pos{0, 2}, "c")

	b.Undo()
	if got := b.Text(); got != "" {
		t.Fatalf("edits within one bucket should undo together, got %q", got)
	}
}

func TestBucketBoundarySplitsEvenWhenCloseInTime(t *testing.T) {
	t.Parallel()
	// Grouping is by the one-second bucket, not by the gap between edits.
	// 0.9 s and 1.1 s are 0.2 s apart but fall in buckets 0 and 1.
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	clock.step(900 * time.Millisecond)
	b.Insert(Pos{}, "a")
	clock.step(200 * time.Millisecond)
	b.Insert(Pos{0, 1}, "b")

	b.Undo()
	if got := b.Text(); got != "a" {
		t.Fatalf("edit in the next bucket should be its own step, got %q", got)
	}
}

func TestRedoAfterUndoRestoresAllSteps(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Insert(Pos{}, "one")
	clock.step(2 * time.Second)
	b.Insert(Pos{0, 3}, " two")

	b.Undo()
	b.Undo()
	if got := b.Text(); got != "" {
		t.Fatalf("after two undos: %q", got)
	}
	if p, ok := b.Redo(); !ok || p != (Pos{0, 3}) {
		t.Fatalf("Redo = %+v, %v; want (0,3), true", p, ok)
	}
	if got := b.Text(); got != "one" {
		t.Fatalf("after first redo: %q", got)
	}
	if p, ok := b.Redo(); !ok || p != (Pos{0, 7}) {
		t.Fatalf("Redo = %+v, %v; want (0,7), true", p, ok)
	}
	if got := b.Text(); got != "one two" {
		t.Fatalf("after second redo: %q", got)
	}
	if _, ok := b.Redo(); ok {
		t.Fatal("nothing left to redo")
	}
}

func TestNewEditAfterUndoClearsRedoAcrossClock(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Insert(Pos{}, "a")
	b.Undo()
	// Same bucket as the undone edit: the edit must not join the redo.
	b.Insert(Pos{}, "z")
	if _, ok := b.Redo(); ok {
		t.Fatal("a new edit must clear the redo stack")
	}
	if got := b.Text(); got != "z" {
		t.Fatalf("text = %q", got)
	}
}

func TestMarkSavedBreaksGroupInSameBucket(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Insert(Pos{}, "a")
	b.MarkSaved()
	// Same bucket as the save, but the save is a boundary.
	b.Insert(Pos{0, 1}, "b")

	b.Undo()
	if got := b.Text(); got != "a" {
		t.Fatalf("save boundary should split the group, got %q", got)
	}
	if b.Modified() {
		t.Fatal("undo back to the saved text should not be modified")
	}
}

func TestBreakSplitsSameBucketEdits(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Insert(Pos{}, "a")
	b.Break()
	b.Insert(Pos{0, 1}, "b")
	b.Break()
	b.Insert(Pos{0, 2}, "c")

	steps := []string{"ab", "a", ""}
	for _, want := range steps {
		b.Undo()
		if got := b.Text(); got != want {
			t.Fatalf("undo step text = %q, want %q", got, want)
		}
	}
}

func TestModifiedFalseAfterUndoToSavePoint(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("doc")
	b.SetClock(clock.now)
	b.Insert(Pos{0, 3}, "!")
	b.MarkSaved()

	clock.step(2 * time.Second)
	b.Insert(Pos{0, 4}, "?")
	if !b.Modified() {
		t.Fatal("edit after save should be modified")
	}
	b.Undo()
	if b.Modified() {
		t.Fatal("undo to the save point should not be modified")
	}
	if got := b.Text(); got != "doc!" {
		t.Fatalf("text = %q", got)
	}
	b.Redo()
	if !b.Modified() {
		t.Fatal("redo past the save point should be modified")
	}
	b.Undo()
	b.Undo()
	if !b.Modified() {
		t.Fatal("undo past the save point should be modified")
	}
}

func TestModifiedAfterUndoThenNewEdit(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Insert(Pos{}, "a")
	b.MarkSaved()
	b.Undo()
	if !b.Modified() {
		t.Fatal("undo past the save point should be modified")
	}
	// The content differs from the saved "a" even though the undo depth
	// matches the save point, so the buffer stays modified.
	b.Insert(Pos{}, "x")
	if !b.Modified() {
		t.Fatal("a different edit after undo must stay modified")
	}
}

func TestUndoRedoReturnCursorPositions(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("hello")
	b.SetClock(clock.now)
	b.Delete(Pos{0, 1}, Pos{0, 3}) // "hlo", cursor at 1
	if p, ok := b.Undo(); !ok || p != (Pos{0, 1}) {
		t.Fatalf("Undo = %+v, %v; want (0,1), true", p, ok)
	}
	if p, ok := b.Redo(); !ok || p != (Pos{0, 1}) {
		t.Fatalf("Redo = %+v, %v; want (0,1), true", p, ok)
	}
}

func TestSetClockNilUsesWallClock(t *testing.T) {
	t.Parallel()
	b := New("")
	b.SetClock(nil)
	b.Insert(Pos{}, "a")
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo should succeed with the wall clock")
	}
	if got := b.Text(); got != "" {
		t.Fatalf("text = %q", got)
	}
}

func TestGroupIgnoresClockAndStaysOneStep(t *testing.T) {
	t.Parallel()
	clock := newStepClock()
	b := New("")
	b.SetClock(clock.now)
	b.Group(func() {
		b.Insert(Pos{}, "a")
		clock.step(5 * time.Second)
		b.Insert(Pos{0, 1}, "b")
	})
	if _, ok := b.Undo(); !ok {
		t.Fatal("Undo should succeed")
	}
	if got := b.Text(); got != "" {
		t.Fatalf("group across clock steps should undo as one step, got %q", got)
	}
	if _, ok := b.Undo(); ok {
		t.Fatal("group must form exactly one undo step")
	}
}
