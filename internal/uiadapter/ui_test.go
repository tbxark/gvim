package uiadapter

import (
	"errors"
	"github.com/tbxark/gvim/internal/highlight"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/explorer"
	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
	"github.com/tbxark/gvim/syntax"
	"github.com/tbxark/gvim/theme"
)

func TestNewUIDepsLeavesNilExplorerNil(t *testing.T) {
	deps := NewDeps(editor.New(editor.Options{}), keymap.Default(), theme.Theme{}, nil, nil)
	if deps.Explorer != nil {
		t.Fatalf("explorer = %T for a nil tree, want a nil interface", deps.Explorer)
	}
	if deps.Clock == nil || deps.Keys == nil || deps.Editor == nil {
		t.Fatal("required ports are missing")
	}
	if deps.LSP != nil {
		t.Fatal("ports for later phases are not nil")
	}
}

func TestNewUIDepsWiresFileOpsToTheEditor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	deps := NewDeps(ed, keymap.Default(), theme.Theme{}, nil, nil)
	if deps.Ops == nil {
		t.Fatal("Ops is nil: file operations are not wired")
	}
	newPath, err := deps.Ops.Rename(t.Context(), path, "b.txt")
	if err != nil {
		t.Fatal(err)
	}
	// The root calls FollowRename from Update once the rename has finished, so
	// the open document moves with the file.
	follow, ok := deps.Ops.(interface {
		FollowRename(oldPath, newPath string) error
	})
	if !ok {
		t.Fatal("Ops does not follow renames in the editor")
	}
	if err := follow.FollowRename(path, newPath); err != nil {
		t.Fatal(err)
	}
	if got := ed.Active().Path; got != newPath {
		t.Fatalf("open document path = %q, want %q after rename", got, newPath)
	}
}

func TestNewUIDepsWiresCommandsFilesAndRanker(t *testing.T) {
	files := finder.Start(t.Context(), t.TempDir())
	defer files.Close()
	deps := NewDeps(editor.New(editor.Options{}), keymap.Default(), theme.Theme{}, nil, files)
	if deps.Files == nil {
		t.Fatal("Files is nil with a file index")
	}
	if deps.Ranker == nil {
		t.Fatal("Ranker is nil")
	}
	if len(deps.Commands) == 0 {
		t.Fatal("Commands is empty: the palette has no command table")
	}
	var hasFind bool
	for _, c := range deps.Commands {
		if c.Action == "find_next" && c.Keybinding != "" {
			hasFind = true
		}
	}
	if !hasFind {
		t.Error("the command table does not list find_next with its shortcut")
	}
	// A nil index stays a nil interface, so the palette shows its loading row.
	if none := NewDeps(editor.New(editor.Options{}), keymap.Default(), theme.Theme{}, nil, nil); none.Files != nil {
		t.Fatalf("Files = %T for a nil index, want a nil interface", none.Files)
	}
}

func TestEditorAdapterMapsTabsAndDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	a := &Editor{ed: ed, spans: highlight.New()}
	tabs := a.Tabs()
	if len(tabs) != 1 || tabs[0].Name != "a.txt" || tabs[0].Path != path || !tabs[0].Active || tabs[0].Modified {
		t.Fatalf("tabs = %+v", tabs)
	}
	doc := a.Active()
	if doc.LineCount() != 3 || doc.Line(1) != "two" {
		t.Fatalf("document lines = %d, line 1 %q", doc.LineCount(), doc.Line(1))
	}
	if _, _, ok := doc.Selection(); ok {
		t.Error("a fresh document reports a selection")
	}
	if doc.Top() != 0 || len(doc.SavedLines()) != 3 {
		t.Errorf("top %d, saved lines %d", doc.Top(), len(doc.SavedLines()))
	}
	// A .txt file has the plain lexer, so every span it reports is plain.
	for _, sp := range doc.LineSpans(0) {
		if sp.Kind != syntax.KindPlain {
			t.Errorf("a plain text span has kind %v", sp.Kind)
		}
	}
}

func TestDocumentLineSpansLexOnceAndFollowEdits(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	a := &Editor{ed: ed, spans: highlight.New()}
	hasKeyword := func(spans []syntax.Span) bool {
		for _, sp := range spans {
			if sp.Kind == syntax.KindKeyword {
				return true
			}
		}
		return false
	}
	if spans := a.Active().LineSpans(0); !hasKeyword(spans) {
		t.Fatalf("the Go keyword is not a keyword span: %v", spans)
	}
	first := a.Active().LineSpans(0)
	a.Active().LineSpans(0)
	if second := a.Active().LineSpans(0); len(first) == 0 || len(second) == 0 || &first[0] != &second[0] {
		t.Fatal("a second read of the same version lexed again")
	}
	a.Do("insert_char", map[string]any{"text": "x"})
	// "xpackage" is one identifier, so the line has no keyword any more. The
	// re-lex shows that.
	if spans := a.Active().LineSpans(0); hasKeyword(spans) || (len(spans) > 0 && len(first) > 0 && &spans[0] == &first[0]) {
		t.Fatalf("an edit did not re-lex the line: spans %v", spans)
	}
	if got := a.Active().Line(0); got != "xpackage main" {
		t.Fatalf("line 0 after the edit = %q", got)
	}
	if spans := a.Active().LineSpans(99); spans != nil {
		t.Fatalf("an out-of-range line has spans: %v", spans)
	}
}

func TestEditorAdapterKeepsStatusForUnbuiltActions(t *testing.T) {
	ed := editor.New(editor.Options{})
	a := &Editor{ed: ed}
	before := ed.Status()
	res := a.Do("quick_open", nil)
	if !errors.Is(res.Err, editor.ErrUnknownAction) {
		t.Fatalf("quick_open error = %v, want ErrUnknownAction", res.Err)
	}
	if ed.Status() != before {
		t.Fatalf("status changed to %q", ed.Status())
	}
	if res := a.Do("move_right", nil); res.Err != nil {
		t.Fatalf("move_right error = %v", res.Err)
	}
}

func TestEditorAdapterOpensFilesAndGoesToLines(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "a.txt")
	second := filepath.Join(dir, "b.txt")
	for _, p := range []string{first, second} {
		if err := os.WriteFile(p, []byte("one\ntwo\nthree\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ed := editor.New(editor.Options{})
	a := &Editor{ed: ed, spans: highlight.New()}
	if err := a.OpenFile(first); err != nil {
		t.Fatal(err)
	}
	if err := a.OpenPreview(second); err != nil {
		t.Fatal(err)
	}
	if tabs := a.Tabs(); len(tabs) != 2 || !tabs[1].Preview || tabs[1].Name != "b.txt (preview)" {
		t.Fatalf("tabs = %+v, want a permanent a.txt and a preview b.txt", tabs)
	}
	if err := a.GotoLine(3, 2); err != nil {
		t.Fatal(err)
	}
	if c := a.Active().Cursor(); c.Line != 2 || c.Col != 1 {
		t.Fatalf("caret = %+v after GotoLine(3, 2), want line 2 col 1", c)
	}
}

func TestDocumentHasPathFollowsTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ed := editor.New(editor.Options{})
	a := &Editor{ed: ed, spans: highlight.New()}
	if a.Active().(documentAdapter).HasPath() {
		t.Error("an unnamed buffer reports a path")
	}
	if err := a.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	if !a.Active().(documentAdapter).HasPath() {
		t.Error("a saved file reports no path")
	}
}

// Compile-time check that the explorer tree satisfies the UI port.
var _ tui.ExplorerTree = (*explorer.Tree)(nil)
