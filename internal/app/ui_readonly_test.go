package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/search"
)

// TestReplaceThroughTheAdapterRefusesReadOnlyDocument checks the find bar's
// Replace and Replace All through the UI adapter. Both must refuse a read-only
// document and leave its text alone.
func TestReplaceThroughTheAdapterRefusesReadOnlyDocument(t *testing.T) {
	t.Parallel()
	const text = "abc abc\n"
	path := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if r := ed.Do("toggle_read_only", nil); r.Err != nil {
		t.Fatalf("toggle_read_only: %v", r.Err)
	}
	a := editorAdapter{ed: ed, spans: &spanCache{}}
	q := search.Query{Text: "abc"}
	matches, err := a.FindMatches(q)
	if err != nil || len(matches) != 2 {
		t.Fatalf("FindMatches = %d matches, %v; want 2", len(matches), err)
	}
	if _, err := a.ReplaceMatch(q, "x", matches[0]); !errors.Is(err, editor.ErrReadOnly) {
		t.Fatalf("ReplaceMatch err = %v, want ErrReadOnly", err)
	}
	if _, err := a.ReplaceMatches(q, "x"); !errors.Is(err, editor.ErrReadOnly) {
		t.Fatalf("ReplaceMatches err = %v, want ErrReadOnly", err)
	}
	if got := ed.Active().Buffer.Text(); got != text {
		t.Fatalf("read-only document text = %q, want %q", got, text)
	}
}
