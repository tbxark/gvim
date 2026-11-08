package editor

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
)

// TestExternalFoldsMergeWithIndentFolds checks the external fold source: a language
// server's ranges join the indentation folds, ordered by start line, and the fold
// actions see both kinds.
func TestExternalFoldsMergeWithIndentFolds(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a\n  b\n  c\nd\n  e\n  f\ng\n")
	d := e.Active()
	if got, want := d.Folds(4), []fold.Range{{Start: 0, End: 2}, {Start: 3, End: 5}}; !slices.Equal(got, want) {
		t.Fatalf("indent folds = %v, want %v", got, want)
	}

	e.SetExternalFolds(path, []fold.Range{{Start: 1, End: 6}})
	want := []fold.Range{{Start: 0, End: 2}, {Start: 1, End: 6}, {Start: 3, End: 5}}
	if got := d.Folds(4); !slices.Equal(got, want) {
		t.Fatalf("merged folds = %v, want %v", got, want)
	}

	// An external range with no indentation fold below it folds too: the caret's
	// toggle finds it, and the hidden-line logic hides its body.
	e.SetCursor(buffer.Pos{Line: 1}, false)
	if r := e.Do("toggle_fold", nil); r.Err != nil {
		t.Fatalf("toggle_fold on an external fold: %v", r.Err)
	}
	if !d.Folded(1) {
		t.Fatal("the external fold is not collapsed")
	}
	if !d.Hidden(4, 4) {
		t.Fatal("a line inside the external fold is not hidden")
	}
}

// TestExternalFoldsClear checks the invalidation: a new empty set with a new
// revision removes the external folds and leaves the indentation ones.
func TestExternalFoldsClear(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a\n  b\n  c\nd\n")
	d := e.Active()
	e.SetExternalFolds(path, []fold.Range{{Start: 0, End: 3}})
	if got, want := d.Folds(4), []fold.Range{{Start: 0, End: 2}, {Start: 0, End: 3}}; !slices.Equal(got, want) {
		t.Fatalf("merged folds = %v, want %v", got, want)
	}
	e.SetExternalFolds(path, nil)
	if got, want := d.Folds(4), []fold.Range{{Start: 0, End: 2}}; !slices.Equal(got, want) {
		t.Fatalf("folds after clearing = %v, want %v", got, want)
	}
}

// TestExternalFoldsIgnoreUnopenedPaths checks that ranges for a file that is not
// open are dropped: the server will send them again when the file opens.
func TestExternalFoldsIgnoreUnopenedPaths(t *testing.T) {
	t.Parallel()
	e, _ := openFixture(t, Options{}, "a\n")
	e.SetExternalFolds("/no/such/file.txt", []fold.Range{{Start: 0, End: 0}})
	if got := e.Active().Folds(4); len(got) != 0 {
		t.Fatalf("folds = %v, want none", got)
	}
}
