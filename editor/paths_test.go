package editor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates path with text and fails the test on error.
func putFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile returns the content of path and fails the test on error.
func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSaveAsUntitledBufferNamesTabAndWrites(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "note.txt")
	e := New(Options{})
	typeText(t, e, "draft")
	if err := e.SaveAs(path); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := readFile(t, path); got != "draft" {
		t.Fatalf("file = %q, want draft", got)
	}
	if e.Active().Path != path {
		t.Fatalf("Path = %q, want %q", e.Active().Path, path)
	}
	if tabs := e.Tabs(); tabs[0].Name != "note.txt" || tabs[0].Modified {
		t.Fatalf("Tabs() = %+v, want saved note.txt", tabs)
	}
	if e.Status() != "Saved" {
		t.Fatalf("Status() = %q", e.Status())
	}
}

func TestSaveAsExistingDocumentWritesNewFileAndKeepsOld(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := filepath.Join(dir, "old.txt")
	next := filepath.Join(dir, "next.txt")
	putFile(t, old, "one")
	e := New(Options{})
	if err := e.OpenFile(old); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "!")
	if err := e.SaveAs(next); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := readFile(t, next); got != "!one" {
		t.Fatalf("next = %q", got)
	}
	if got := readFile(t, old); got != "one" {
		t.Fatalf("old file changed to %q", got)
	}
	if e.Active().Path != next {
		t.Fatalf("Path = %q, want %q", e.Active().Path, next)
	}
}

func TestSaveAsRefusesDifferentExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	target := filepath.Join(dir, "taken.txt")
	putFile(t, target, "keep me")
	e := New(Options{})
	typeText(t, e, "new")
	err := e.SaveAs(target)
	if !errors.Is(err, ErrExists) {
		t.Fatalf("SaveAs err = %v, want ErrExists", err)
	}
	if got := readFile(t, target); got != "keep me" {
		t.Fatalf("refused save still wrote %q", got)
	}
	if e.Active().Path != "" {
		t.Fatalf("failed SaveAs changed Path to %q", e.Active().Path)
	}
	if e.Status() == "" {
		t.Fatal("failed SaveAs must set the status")
	}
}

func TestSaveAsForceOverwritesThroughAction(t *testing.T) {
	t.Parallel()
	target := filepath.Join(t.TempDir(), "taken.txt")
	putFile(t, target, "old")
	e := New(Options{})
	typeText(t, e, "new")
	if r := e.Do("save_as", map[string]any{"path": target}); !errors.Is(r.Err, ErrExists) {
		t.Fatalf("save_as without force err = %v, want ErrExists", r.Err)
	}
	if r := e.Do("save_as", map[string]any{"path": target, "force": true}); r.Err != nil {
		t.Fatalf("save_as with force: %v", r.Err)
	}
	if got := readFile(t, target); got != "new" {
		t.Fatalf("file = %q, want new", got)
	}
}

func TestSaveAsSamePathIsNotAConflict(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "self.txt")
	putFile(t, path, "a")
	e := New(Options{})
	if err := e.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "b")
	if err := e.SaveAs(path); err != nil {
		t.Fatalf("SaveAs to own path: %v", err)
	}
	if got := readFile(t, path); got != "ba" {
		t.Fatalf("file = %q, want ba", got)
	}
}

func TestSaveAsEmptyPathAndMissingArgument(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if err := e.SaveAs(""); !errors.Is(err, ErrEmptyPath) {
		t.Fatalf("SaveAs(\"\") err = %v", err)
	}
	if r := e.Do("save_as", nil); r.Err == nil {
		t.Fatal("save_as without path must fail")
	}
}

func TestSaveAsWriteFailureKeepsOldPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.txt")
	putFile(t, source, "x")
	e := New(Options{})
	if err := e.OpenFile(source); err != nil {
		t.Fatal(err)
	}
	// A directory that does not exist makes the write fail.
	bad := filepath.Join(dir, "missing", "out.txt")
	err := e.SaveAs(bad)
	if err == nil {
		t.Fatal("SaveAs into a missing directory must fail")
	}
	if e.Active().Path != source {
		t.Fatalf("failed SaveAs changed Path to %q", e.Active().Path)
	}
}

func TestRenamePathUpdatesOpenFileAndTabName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	from := filepath.Join(dir, "a.txt")
	to := filepath.Join(dir, "b.txt")
	putFile(t, from, "x")
	e := New(Options{})
	if err := e.OpenFile(from); err != nil {
		t.Fatal(err)
	}
	if err := e.RenamePath(from, to); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if e.Active().Path != to {
		t.Fatalf("Path = %q, want %q", e.Active().Path, to)
	}
	if e.Tabs()[0].Name != "b.txt" {
		t.Fatalf("tab name = %q", e.Tabs()[0].Name)
	}
}

func TestRenamePathFolderRebasesOpenFilesBelowIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	inside := filepath.Join(dir, "pkg", "sub", "x.go")
	other := filepath.Join(dir, "pkgs", "y.go")
	putFile(t, inside, "x")
	putFile(t, other, "y")
	e := New(Options{})
	if err := e.OpenFile(inside); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(other); err != nil {
		t.Fatal(err)
	}
	renamed := filepath.Join(dir, "lib")
	if err := e.RenamePath(filepath.Join(dir, "pkg"), renamed); err != nil {
		t.Fatalf("RenamePath folder: %v", err)
	}
	want := filepath.Join(renamed, "sub", "x.go")
	if got := e.docs[0].Path; got != want {
		t.Fatalf("inside path = %q, want %q", got, want)
	}
	if got := e.docs[1].Path; got != other {
		t.Fatalf("sibling with shared prefix changed to %q", got)
	}
}

func TestRenamePathEmptyArgumentsFail(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if err := e.RenamePath("", "x"); !errors.Is(err, ErrEmptyPath) {
		t.Fatalf("RenamePath empty old err = %v", err)
	}
	if err := e.RenamePath("x", ""); !errors.Is(err, ErrEmptyPath) {
		t.Fatalf("RenamePath empty new err = %v", err)
	}
}

func TestCloseByPathClosesCleanDocument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(b); err != nil {
		t.Fatal(err)
	}
	if err := e.CloseByPath(a, false); err != nil {
		t.Fatalf("CloseByPath: %v", err)
	}
	tabs := e.Tabs()
	if len(tabs) != 1 || tabs[0].Path != b || !tabs[0].Active {
		t.Fatalf("Tabs() = %+v, want only active b.txt", tabs)
	}
}

func TestCloseByPathKeepsModifiedUnlessForced(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	putFile(t, a, "a")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "x")
	if err := e.CloseByPath(a, false); !errors.Is(err, ErrUnsaved) {
		t.Fatalf("CloseByPath err = %v, want ErrUnsaved", err)
	}
	if tabs := e.Tabs(); len(tabs) != 1 || !tabs[0].Modified {
		t.Fatalf("refused close changed tabs: %+v", tabs)
	}
	if err := e.CloseByPath(a, true); err != nil {
		t.Fatalf("forced CloseByPath: %v", err)
	}
	if !e.Empty() || len(e.Tabs()) != 0 {
		t.Fatalf("Tabs() = %+v, want no tab after closing the only one", e.Tabs())
	}
}

func TestCloseByPathBlockedCloseClosesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	folder := filepath.Join(dir, "d")
	clean := filepath.Join(folder, "clean.txt")
	dirty := filepath.Join(folder, "dirty.txt")
	putFile(t, clean, "c")
	putFile(t, dirty, "d")
	e := New(Options{})
	if err := e.OpenFile(clean); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(dirty); err != nil {
		t.Fatal(err)
	}
	typeText(t, e, "x")
	if err := e.CloseByPath(folder, false); !errors.Is(err, ErrUnsaved) {
		t.Fatalf("CloseByPath folder err = %v", err)
	}
	if len(e.Tabs()) != 2 {
		t.Fatalf("blocked folder close changed tabs: %+v", e.Tabs())
	}
}

func TestCloseByPathFolderAndActiveNeighbour(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	keep := filepath.Join(dir, "keep.txt")
	gone := filepath.Join(dir, "gone")
	goneFile := filepath.Join(gone, "f.txt")
	putFile(t, keep, "k")
	putFile(t, goneFile, "g")
	e := New(Options{})
	if err := e.OpenFile(keep); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(goneFile); err != nil {
		t.Fatal(err)
	}
	if err := e.CloseByPath(gone, false); err != nil {
		t.Fatalf("CloseByPath folder: %v", err)
	}
	tabs := e.Tabs()
	if len(tabs) != 1 || tabs[0].Path != keep || !tabs[0].Active {
		t.Fatalf("Tabs() = %+v, want active keep.txt", tabs)
	}
}

func TestCloseByPathUnmatchedAndEmpty(t *testing.T) {
	t.Parallel()
	e := New(Options{})
	if err := e.CloseByPath(filepath.Join(t.TempDir(), "none.txt"), false); err != nil {
		t.Fatalf("unmatched CloseByPath err = %v", err)
	}
	if err := e.CloseByPath("", false); !errors.Is(err, ErrEmptyPath) {
		t.Fatalf("empty CloseByPath err = %v", err)
	}
}

// TestCloseByPathActiveGoesToNeighbour: when the closed tab is the active one, the tab that
// takes its place is the next survivor, as CloseTab picks it, and not the first tab.
func TestCloseByPathActiveGoesToNeighbour(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var paths []string
	e := New(Options{})
	for _, name := range []string{"a.txt", "b.txt", "c.txt", "d.txt"} {
		p := filepath.Join(dir, name)
		putFile(t, p, name)
		paths = append(paths, p)
		if err := e.OpenFile(p); err != nil {
			t.Fatal(err)
		}
	}
	e.ActivateTab(2)
	if err := e.CloseByPath(paths[2], false); err != nil {
		t.Fatal(err)
	}
	if got := e.Active().Path; got != paths[3] {
		t.Fatalf("active after closing c.txt = %s, want d.txt", filepath.Base(got))
	}
}
