package search

import (
	"testing"

	"github.com/tbxark/gvim/internal/buffer"
)

// undoOnce runs one Undo and reports whether it did anything.
func undoOnce(b *buffer.Buffer) bool {
	_, ok := b.Undo()
	return ok
}

func TestReplaceIsOneUndoStep(t *testing.T) {
	t.Parallel()
	b := buffer.New("foo bar foo")
	if _, err := Replace(b, Query{Text: "foo", CaseSensitive: true}, "longer", m(0, 0, 0, 3)); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	if !undoOnce(b) {
		t.Fatalf("Undo() after Replace reported nothing to undo")
	}
	if b.Text() != "foo bar foo" {
		t.Fatalf("after one undo, buffer = %q, want %q", b.Text(), "foo bar foo")
	}
	if undoOnce(b) {
		t.Fatalf("a second Undo reverted more than the one replacement")
	}
}

func TestReplaceRedo(t *testing.T) {
	t.Parallel()
	b := buffer.New("a-b")
	if _, err := Replace(b, Query{Text: "-", CaseSensitive: true}, "=>", m(0, 1, 0, 2)); err != nil {
		t.Fatalf("Replace() error = %v", err)
	}
	undoOnce(b)
	if _, ok := b.Redo(); !ok {
		t.Fatalf("Redo() after undoing a Replace reported nothing to redo")
	}
	if b.Text() != "a=>b" {
		t.Fatalf("after redo, buffer = %q, want %q", b.Text(), "a=>b")
	}
}

func TestReplaceAllIsOneUndoStep(t *testing.T) {
	t.Parallel()
	const original = "x1 x2\nx3 x4"
	b := buffer.New(original)
	n, err := ReplaceAll(b, Query{Text: `x(\d)`, CaseSensitive: true, Regex: true}, "y$1")
	if err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}
	if n != 4 {
		t.Fatalf("ReplaceAll() count = %d, want 4", n)
	}
	if want := "y1 y2\ny3 y4"; b.Text() != want {
		t.Fatalf("after ReplaceAll, buffer = %q, want %q", b.Text(), want)
	}
	if !undoOnce(b) {
		t.Fatalf("Undo() after ReplaceAll reported nothing to undo")
	}
	if b.Text() != original {
		t.Fatalf("after one undo, buffer = %q, want %q", b.Text(), original)
	}
	if undoOnce(b) {
		t.Fatalf("ReplaceAll took more than one undo step")
	}
}

func TestReplaceAllRedo(t *testing.T) {
	t.Parallel()
	b := buffer.New("aa aa")
	if _, err := ReplaceAll(b, Query{Text: "aa", CaseSensitive: true}, "b"); err != nil {
		t.Fatalf("ReplaceAll() error = %v", err)
	}
	undoOnce(b)
	if _, ok := b.Redo(); !ok {
		t.Fatalf("Redo() after undoing ReplaceAll reported nothing to redo")
	}
	if b.Text() != "b b" {
		t.Fatalf("after redo, buffer = %q, want %q", b.Text(), "b b")
	}
}

func TestReplaceAllNoMatchMakesNoUndoStep(t *testing.T) {
	t.Parallel()
	b := buffer.New("abc")
	n, err := ReplaceAll(b, Query{Text: "zzz", CaseSensitive: true}, "y")
	if err != nil || n != 0 {
		t.Fatalf("ReplaceAll() = %d, %v; want 0, nil", n, err)
	}
	if undoOnce(b) {
		t.Fatalf("ReplaceAll with no matches left an undo step")
	}
}

func TestQueryReplaceIsOneUndoStep(t *testing.T) {
	t.Parallel()
	const original = "a a a"
	b := buffer.New(original)
	n, err := QueryReplace(b, Query{Text: "a", CaseSensitive: true}, "b", func(_ Match, i int) Decision {
		if i == 1 {
			return DecisionSkip
		}
		return DecisionReplace
	})
	if err != nil || n != 2 {
		t.Fatalf("QueryReplace() = %d, %v; want 2, nil", n, err)
	}
	if want := "b a b"; b.Text() != want {
		t.Fatalf("after QueryReplace, buffer = %q, want %q", b.Text(), want)
	}
	if !undoOnce(b) {
		t.Fatalf("Undo() after QueryReplace reported nothing to undo")
	}
	if b.Text() != original {
		t.Fatalf("after one undo, buffer = %q, want %q", b.Text(), original)
	}
	if undoOnce(b) {
		t.Fatalf("QueryReplace took more than one undo step")
	}
}

func TestReplaceInsideCallerGroupJoinsIt(t *testing.T) {
	t.Parallel()
	b := buffer.New("one two")
	b.Group(func() {
		if _, err := Replace(b, Query{Text: "one", CaseSensitive: true}, "1", m(0, 0, 0, 3)); err != nil {
			t.Errorf("Replace() error = %v", err)
		}
		if _, err := Replace(b, Query{Text: "two", CaseSensitive: true}, "2", m(0, 2, 0, 5)); err != nil {
			t.Errorf("Replace() error = %v", err)
		}
	})
	if b.Text() != "1 2" {
		t.Fatalf("buffer = %q, want %q", b.Text(), "1 2")
	}
	if !undoOnce(b) {
		t.Fatalf("Undo() reported nothing to undo")
	}
	if b.Text() != "one two" {
		t.Fatalf("after one undo, buffer = %q, want %q", b.Text(), "one two")
	}
	if undoOnce(b) {
		t.Fatalf("the two replacements made more than one undo step")
	}
}
