package editor

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/buffer"
)

// TestApplyCompletionAtCarets: accepting a completion with extra carets replaces each
// caret's own identifier prefix with the same text as one atomic undo step.
func TestApplyCompletionAtCarets(t *testing.T) {
	e := withDoc("fo x\nfoobar y\n")
	e.Active().Rename("/fake/a.go")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 0, Col: 2}
	d.Extra = []Caret{{Cursor: buffer.Pos{Line: 1, Col: 6}}}
	n, err := e.ApplyCompletionAtCarets("/fake/a.go", "abc", "", nil)
	if err != nil {
		t.Fatalf("ApplyCompletionAtCarets: %v", err)
	}
	if n != 2 {
		t.Fatalf("edited carets = %d, want 2", n)
	}
	if got := text(e); got != "abc x\nabc y\n" {
		t.Fatalf("text = %q", got)
	}
	if d.Cursor != (buffer.Pos{Line: 0, Col: 3}) || len(d.Cursors()) != 1 || d.Cursors()[0].Cursor != (buffer.Pos{Line: 1, Col: 3}) {
		t.Fatalf("carets = primary %v extras %v", d.Cursor, d.Cursors())
	}
	if _, ok := d.Buffer.Undo(); !ok {
		t.Fatal("the multi-caret accept was not one undo step")
	}
	if got := text(e); got != "fo x\nfoobar y\n" {
		t.Fatalf("after undo text = %q, want the original", got)
	}
}

// TestApplyCompletionAtCaretsEmptyPrefixAndStale: an extra caret without a word prefix
// gets the text inserted, and the version guard refuses a stale accept without touching
// the text.
func TestApplyCompletionAtCaretsEmptyPrefixAndStale(t *testing.T) {
	e := withDoc(" x\nfo y\n")
	e.Active().Rename("/fake/a.go")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 0, Col: 0}
	d.Extra = []Caret{{Cursor: buffer.Pos{Line: 1, Col: 2}}}
	if _, err := e.ApplyCompletionAtCarets("/fake/a.go", "abc", "", nil); err != nil {
		t.Fatalf("empty-prefix accept: %v", err)
	}
	if got := text(e); got != "abc x\nabc y\n" {
		t.Fatalf("empty-prefix text = %q", got)
	}
	stale := d.Version()
	typeKey(t, e, "z")
	before := text(e)
	if _, err := e.ApplyCompletionAtCarets("/fake/a.go", "abc", "", &stale); !errors.Is(err, ErrStale) {
		t.Fatalf("stale accept error = %v, want ErrStale", err)
	}
	if got := text(e); got != before {
		t.Fatalf("stale accept changed the text: %q", got)
	}
}

// TestApplyCompletionAtCaretsOverlapRefused: two carets inside one word produce overlapping
// replacement ranges; the accept is refused as a whole and nothing is written.
func TestApplyCompletionAtCaretsOverlapRefused(t *testing.T) {
	e := withDoc("foobar\n")
	e.Active().Rename("/fake/a.go")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 0, Col: 3}
	d.Extra = []Caret{{Cursor: buffer.Pos{Line: 0, Col: 1}}}
	if _, err := e.ApplyCompletionAtCarets("/fake/a.go", "x", "", nil); !errors.Is(err, ErrOverlappingEdits) {
		t.Fatalf("overlap error = %v, want ErrOverlappingEdits", err)
	}
	if got := text(e); got != "foobar\n" {
		t.Fatalf("overlapping accept changed the text: %q", got)
	}
}
