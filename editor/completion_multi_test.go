package editor

import (
	"errors"
	"strings"
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

// TestApplyCompletionItemAtCaretsSnippet: a snippet item replaces each caret's word
// prefix with the expanded text in ONE undo step and places every caret at the same rune
// offset ($0) inside its own insertion.
func TestApplyCompletionItemAtCaretsSnippet(t *testing.T) {
	e := withDoc("fo x\nfoobar y\n")
	e.Active().Rename("/fake/a.go")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 0, Col: 2}
	d.Extra = []Caret{{Cursor: buffer.Pos{Line: 1, Col: 6}}}
	n, err := e.ApplyCompletionItemAtCarets("/fake/a.go", "abcXYZ", 3, nil, "", nil)
	if err != nil {
		t.Fatalf("snippet accept: %v", err)
	}
	if n != 2 {
		t.Fatalf("edited carets = %d, want 2", n)
	}
	if got := text(e); got != "abcXYZ x\nabcXYZ y\n" {
		t.Fatalf("text = %q", got)
	}
	// $0 is after "abc", not at the end of the insertion.
	if d.Cursor != (buffer.Pos{Line: 0, Col: 3}) {
		t.Fatalf("primary caret = %v, want 0,3 (inside the insertion)", d.Cursor)
	}
	if got := d.Cursors(); len(got) != 1 || got[0].Cursor != (buffer.Pos{Line: 1, Col: 3}) {
		t.Fatalf("extra carets = %v, want one at line 1 col 3", got)
	}
	if _, ok := d.Buffer.Undo(); !ok {
		t.Fatal("the snippet accept was not one undo step")
	}
	if got := text(e); got != "fo x\nfoobar y\n" {
		t.Fatalf("after undo text = %q", got)
	}
}

// TestApplyCompletionItemAtCaretsSnippetAcrossNewline: the snippet stop is counted in
// RUNES, and a line break inside the inserted text counts as one rune when walking back.
func TestApplyCompletionItemAtCaretsSnippetAcrossNewline(t *testing.T) {
	e := withDoc("fo\n")
	e.Active().Rename("/fake/a.go")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 0, Col: 2}
	// "a\nb" with $0 after the newline: the caret must land on the second inserted line.
	if _, err := e.ApplyCompletionItemAtCarets("/fake/a.go", "a\nb", 2, nil, "", nil); err != nil {
		t.Fatal(err)
	}
	if got := text(e); got != "a\nb\n" {
		t.Fatalf("text = %q", got)
	}
	if d.Cursor != (buffer.Pos{Line: 1, Col: 0}) {
		t.Fatalf("caret = %v, want line 1 col 0", d.Cursor)
	}
}

// TestApplyCompletionItemAtCaretsAutoImport: the additional (import) edit is applied ONCE
// together with every caret insert in the same atomic edit; the version guard refuses a
// stale accept and one undo restores everything.
func TestApplyCompletionItemAtCaretsAutoImport(t *testing.T) {
	e := withDoc("imports\nfo\nf\n")
	e.Active().Rename("/fake/a.go")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 1, Col: 2}
	d.Extra = []Caret{{Cursor: buffer.Pos{Line: 2, Col: 1}}}
	before := d.Version()
	additional := []LSPEdit{{
		From: buffer.Pos{Line: 0, Col: 0},
		To:   buffer.Pos{Line: 0, Col: 0},
		Text: "import X\n",
	}}
	if _, err := e.ApplyCompletionItemAtCarets("/fake/a.go", "abc", -1, additional, "", &before); err != nil {
		t.Fatal(err)
	}
	got := text(e)
	if got != "import X\nimports\nabc\nabc\n" {
		t.Fatalf("text = %q", got)
	}
	if strings.Count(got, "import X") != 1 {
		t.Fatalf("additional edit applied %d times: %q", strings.Count(got, "import X"), got)
	}
	if _, ok := d.Buffer.Undo(); !ok {
		t.Fatal("insert+import was not one undo step")
	}
	if got := text(e); got != "imports\nfo\nf\n" {
		t.Fatalf("after undo text = %q", got)
	}
}

// TestApplyCompletionItemAtCaretsStaleAndOverlap: stale versions and ambiguous combined
// edit lists refuse the whole accept and write nothing.
func TestApplyCompletionItemAtCaretsStaleAndOverlap(t *testing.T) {
	e := withDoc("fo\nfe\n")
	e.Active().Rename("/fake/a.go")
	d := e.Active()
	d.Cursor = buffer.Pos{Line: 0, Col: 2}
	d.Extra = []Caret{{Cursor: buffer.Pos{Line: 1, Col: 2}}}
	stale := d.Version()
	typeKey(t, e, "x")
	afterTyping := text(e)
	if _, err := e.ApplyCompletionItemAtCarets("/fake/a.go", "abc", -1, nil, "", &stale); !errors.Is(err, ErrStale) {
		t.Fatalf("stale err = %v", err)
	}
	if got := text(e); got != afterTyping {
		t.Fatalf("stale changed text: %q, want %q", got, afterTyping)
	}
	// An additional edit covering a caret replacement range overlaps it.
	overlap := []LSPEdit{{From: buffer.Pos{Line: 0, Col: 0}, To: buffer.Pos{Line: 0, Col: 2}, Text: "Y"}}
	if _, err := e.ApplyCompletionItemAtCarets("/fake/a.go", "abc", -1, overlap, "", nil); !errors.Is(err, ErrOverlappingEdits) {
		t.Fatalf("overlap err = %v", err)
	}
	// Two zero-width inserts at the same point would have ambiguous order.
	dup := []LSPEdit{
		{From: buffer.Pos{Line: 0, Col: 2}, To: buffer.Pos{Line: 0, Col: 2}, Text: "1"},
		{From: buffer.Pos{Line: 0, Col: 2}, To: buffer.Pos{Line: 0, Col: 2}, Text: "2"},
	}
	if _, err := e.ApplyCompletionItemAtCarets("/fake/a.go", "abc", -1, dup, "", nil); !errors.Is(err, ErrOverlappingEdits) {
		t.Fatalf("duplicate insert err = %v", err)
	}
	if got := text(e); strings.Contains(got, "abc") || strings.Contains(got, "Y") {
		t.Fatalf("refused accept wrote text: %q", got)
	}
}
