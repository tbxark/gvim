package uiadapter

import (
	"github.com/tbxark/gvim/internal/apptest"
	"github.com/tbxark/gvim/internal/highlight"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/tbxark/gvim/buffer"
	"github.com/tbxark/gvim/editor"
)

// TestEditedTabIsLexedAgainWhenShownInASplit checks that a tab edited while it was
// the active tab is lexed again when a split pane shows it. The span cache the pane
// kept from an earlier view must not be reused after the edit.
func TestEditedTabIsLexedAgainWhenShownInASplit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.txt")
	pathB := filepath.Join(dir, "b.go")
	apptest.Must(t, os.WriteFile(pathA, []byte("plain\n"), 0o644))
	apptest.Must(t, os.WriteFile(pathB, []byte("package p\n"), 0o644))
	ed := editor.New(editor.Options{})
	apptest.Must(t, ed.OpenFile(pathA))
	apptest.Must(t, ed.OpenFile(pathB))
	a := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}

	// Tab 1 (b.go) is not active, so the split pane lexes it into its own cache.
	a.ActivateTab(0)
	before := a.DocAt(1).LineSpans(0)
	if len(before) == 0 {
		t.Fatal("b.go has no spans on its first line before the edit")
	}

	// Edit b.go while it is the active tab. Its first line becomes a comment.
	a.ActivateTab(1)
	a.SetCursor(buffer.Pos{}, false)
	if res := a.Do("insert_char", map[string]any{"text": "// "}); res.Err != nil {
		t.Fatal(res.Err)
	}

	// Show b.go in the split pane again, after switching the active tab away.
	a.ActivateTab(0)
	after := a.DocAt(1).LineSpans(0)
	fresh := &Editor{ed: ed, spans: highlight.New(), others: map[*editor.Document]*highlight.Cache{}}
	want := fresh.DocAt(1).LineSpans(0)
	if reflect.DeepEqual(after, before) {
		t.Fatalf("the split pane shows the spans from before the edit: %v", after)
	}
	if !reflect.DeepEqual(after, want) {
		t.Fatalf("spans after the edit = %v, want the spans of the edited text %v", after, want)
	}
}
