package editor

import (
	"slices"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/fold"
)

// TestOutlineFoldsMerge checks the outline fold source: the Contents section's
// heading ranges join the indentation folds and the language server's ranges in one
// ordered set, and the standard fold action and hidden-line logic see them.
func TestOutlineFoldsMerge(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a\n  b\n  c\nd\n  e\n  f\ng\n")
	d := e.Active()
	e.SetExternalFolds(path, []fold.Range{{Start: 3, End: 5}})
	e.SetOutlineFolds(path, []fold.Range{{Start: 1, End: 2}, {Start: 3, End: 4}}, d.Buffer.Version())
	want := []fold.Range{{Start: 0, End: 2}, {Start: 1, End: 2}, {Start: 3, End: 4}, {Start: 3, End: 5}}
	if got := d.Folds(4); !slices.Equal(got, want) {
		t.Fatalf("merged folds = %v, want %v", got, want)
	}
	// A published heading range folds through the normal action and hides its body.
	e.SetCursor(buffer.Pos{Line: 1}, false)
	if r := e.Do("toggle_fold", nil); r.Err != nil {
		t.Fatalf("toggle_fold on an outline fold: %v", r.Err)
	}
	if !d.Folded(1) {
		t.Fatal("the outline fold is not collapsed")
	}
	if !d.Hidden(2, 4) {
		t.Fatal("a line inside the outline fold is not hidden")
	}
	if !e.FoldedAt(1) || e.FoldedAt(0) {
		t.Errorf("FoldedAt(1) = %v, FoldedAt(0) = %v, want only line 1 folded", e.FoldedAt(1), e.FoldedAt(0))
	}
}

// TestOutlineFoldsDropOnEdit checks the text-version guard: an edit drops the
// published ranges until the outline republishes for the new text, so a stale
// heading range can never fold a line it was not computed for.
func TestOutlineFoldsDropOnEdit(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "one\ntwo\n")
	d := e.Active()
	version := d.Buffer.Version()
	e.SetOutlineFolds(path, []fold.Range{{Start: 0, End: 1}}, version)
	if got := d.Folds(4); !slices.Equal(got, []fold.Range{{Start: 0, End: 1}}) {
		t.Fatalf("folds = %v, want the published range", got)
	}
	typeText(t, e, "x")
	if d.Buffer.Version() == version {
		t.Fatal("the edit did not change the text version")
	}
	if got := d.Folds(4); len(got) != 0 {
		t.Fatalf("folds after an edit = %v, want the stale outline ranges dropped", got)
	}
	// Republishing at the new version brings a range back.
	e.SetOutlineFolds(path, []fold.Range{{Start: 0, End: 0}}, d.Buffer.Version())
	want := []fold.Range{{Start: 0, End: 0}}
	if got := d.Folds(4); !slices.Equal(got, want) {
		t.Fatalf("folds after republishing = %v, want %v", got, want)
	}
}

// TestOutlineFoldsReplaceAndClear checks that a republish replaces the previous set
// (the memo is invalidated even for the same text) and that an empty set clears it.
func TestOutlineFoldsReplaceAndClear(t *testing.T) {
	t.Parallel()
	e, path := openFixture(t, Options{}, "a\nb\nc\nd\n")
	d := e.Active()
	version := d.Buffer.Version()
	e.SetOutlineFolds(path, []fold.Range{{Start: 0, End: 1}}, version)
	if got := d.Folds(4); !slices.Equal(got, []fold.Range{{Start: 0, End: 1}}) {
		t.Fatalf("folds = %v, want the first set", got)
	}
	e.SetOutlineFolds(path, []fold.Range{{Start: 2, End: 3}}, version)
	want := []fold.Range{{Start: 2, End: 3}}
	if got := d.Folds(4); !slices.Equal(got, want) {
		t.Fatalf("folds after replacing the set = %v, want %v", got, want)
	}
	if got := d.OutlineFolds(); !slices.Equal(got, want) {
		t.Errorf("OutlineFolds() = %v, want %v", got, want)
	}
	e.SetOutlineFolds(path, nil, version)
	if got := d.Folds(4); len(got) != 0 {
		t.Fatalf("folds after clearing = %v, want none", got)
	}
	// A path that is not open is ignored.
	e.SetOutlineFolds("/not/open.md", []fold.Range{{Start: 0, End: 3}}, version)
	if got := d.Folds(4); len(got) != 0 {
		t.Fatalf("folds after a publish for a closed path = %v, want none", got)
	}
}
