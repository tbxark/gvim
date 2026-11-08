package editor

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// openTwo opens a and b in that order, so b is the active tab.
func openTwo(t *testing.T, a, b string) *Editor {
	t.Helper()
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	if err := e.OpenFile(b); err != nil {
		t.Fatal(err)
	}
	return e
}

func TestSaveAsOntoOpenPathIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	typeText(t, e, "!")

	err := e.SaveAs(a)
	if !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("SaveAs err = %v, want ErrAlreadyOpen", err)
	}
	if !strings.Contains(err.Error(), a) {
		t.Fatalf("error %q does not name the path %q", err, a)
	}
	if got := readFile(t, a); got != "a" {
		t.Fatalf("open tab a was overwritten with %q", got)
	}
	if got := readFile(t, b); got != "b" {
		t.Fatalf("active file b was written as %q", got)
	}
	if e.Active().Path != b || !e.Active().Modified() {
		t.Fatalf("refused SaveAs changed the active document: path %q, modified %v",
			e.Active().Path, e.Active().Modified())
	}
	if e.Status() == "" {
		t.Fatal("refused SaveAs must set the status")
	}
}

func TestSaveAsOntoOpenPathRefusedEvenWithForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	typeText(t, e, "!")

	if r := e.Do("save_as", map[string]any{"path": a, "force": true}); !errors.Is(r.Err, ErrAlreadyOpen) {
		t.Fatalf("save_as with force err = %v, want ErrAlreadyOpen", r.Err)
	}
	if got := readFile(t, a); got != "a" {
		t.Fatalf("open tab a was overwritten with %q", got)
	}
	if e.Active().Path != b {
		t.Fatalf("Path = %q, want %q", e.Active().Path, b)
	}
}

func TestSaveAsOntoOpenPathWritesNothing(t *testing.T) {
	t.Parallel()
	// The path is open in another tab but does not exist on disk. The
	// duplicate check must run before any write, so the error is ErrAlreadyOpen
	// and not the write failure that a missing directory would cause.
	e := withTabs("a", "b")
	e.ActivateTab(1)
	err := e.SaveAs("/fake/a")
	if !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("SaveAs err = %v, want ErrAlreadyOpen", err)
	}
	if e.Active().Path != "/fake/b" {
		t.Fatalf("Path = %q, want /fake/b", e.Active().Path)
	}
}

func TestSaveAsUntitledToFreshPathAllowedWithOtherTabsOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	putFile(t, a, "a")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	e.Do("new", nil)
	typeText(t, e, "fresh")
	fresh := filepath.Join(dir, "fresh.txt")
	if err := e.SaveAs(fresh); err != nil {
		t.Fatalf("SaveAs to fresh path: %v", err)
	}
	if got := readFile(t, fresh); got != "fresh" {
		t.Fatalf("file = %q, want fresh", got)
	}
	if idx, ok := e.DocumentIndexByPath(fresh); !ok || idx != 1 {
		t.Fatalf("DocumentIndexByPath(fresh) = %d, %v, want 1, true", idx, ok)
	}
}

func TestSaveAsSameFileThroughAnotherSpellingIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	// The same file under an unclean spelling is still open in tab 0.
	spelled := dir + "/x/../a.txt"
	if err := e.SaveAs(spelled); !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("SaveAs(%q) err = %v, want ErrAlreadyOpen", spelled, err)
	}
}

func TestRenamePathOntoOpenPathIsRefusedAndChangesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	before := e.Tabs()

	err := e.RenamePath(a, b)
	if !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("RenamePath err = %v, want ErrAlreadyOpen", err)
	}
	if !strings.Contains(err.Error(), b) {
		t.Fatalf("error %q does not name the path %q", err, b)
	}
	if got := e.Tabs(); !equalTabs(got, before) {
		t.Fatalf("Tabs() changed to %+v, want %+v", got, before)
	}
	if e.Status() == "" {
		t.Fatal("refused RenamePath must set the status")
	}
}

func TestRenamePathFolderOntoOpenPathIsRefused(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pkgX := filepath.Join(dir, "pkg", "x.go")
	libX := filepath.Join(dir, "lib", "x.go")
	putFile(t, pkgX, "pkg")
	putFile(t, libX, "lib")
	e := openTwo(t, pkgX, libX)

	// Renaming pkg to lib would move pkg/x.go onto the open lib/x.go.
	if err := e.RenamePath(filepath.Join(dir, "pkg"), filepath.Join(dir, "lib")); !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("RenamePath folder err = %v, want ErrAlreadyOpen", err)
	}
	if got := e.docs[0].Path; got != pkgX {
		t.Fatalf("pkg tab moved to %q despite the refusal", got)
	}
	if got := e.docs[1].Path; got != libX {
		t.Fatalf("lib tab changed to %q", got)
	}
}

func TestRenamePathFolderPartialConflictChangesNothing(t *testing.T) {
	t.Parallel()
	// Two tabs move, and only one of their targets is taken. Neither may move.
	dir := t.TempDir()
	a1 := filepath.Join(dir, "src", "one.go")
	a2 := filepath.Join(dir, "src", "two.go")
	taken := filepath.Join(dir, "dst", "two.go")
	putFile(t, a1, "1")
	putFile(t, a2, "2")
	putFile(t, taken, "t")
	e := New(Options{})
	for _, p := range []string{a1, a2, taken} {
		if err := e.OpenFile(p); err != nil {
			t.Fatal(err)
		}
	}
	err := e.RenamePath(filepath.Join(dir, "src"), filepath.Join(dir, "dst"))
	if !errors.Is(err, ErrAlreadyOpen) {
		t.Fatalf("RenamePath err = %v, want ErrAlreadyOpen", err)
	}
	if e.docs[0].Path != a1 || e.docs[1].Path != a2 {
		t.Fatalf("paths changed after refusal: %q, %q", e.docs[0].Path, e.docs[1].Path)
	}
}

func TestRenamePathSameDocumentToOwnPathIsAllowed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	if err := e.RenamePath(a, a); err != nil {
		t.Fatalf("RenamePath to own path: %v", err)
	}
	if e.docs[0].Path != a {
		t.Fatalf("Path = %q, want %q", e.docs[0].Path, a)
	}
	if err := e.RenamePath(b, b); err != nil {
		t.Fatalf("RenamePath active doc to own path: %v", err)
	}
	if e.Active().Path != b {
		t.Fatalf("Path = %q, want %q", e.Active().Path, b)
	}
}

func TestRenamePathFolderOntoItselfIsAllowed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	x := filepath.Join(dir, "pkg", "x.go")
	putFile(t, x, "x")
	e := New(Options{})
	if err := e.OpenFile(x); err != nil {
		t.Fatal(err)
	}
	if err := e.RenamePath(filepath.Join(dir, "pkg"), filepath.Join(dir, "pkg")); err != nil {
		t.Fatalf("RenamePath folder to itself: %v", err)
	}
	if e.Active().Path != x {
		t.Fatalf("Path = %q, want %q", e.Active().Path, x)
	}
}

func TestRenamePathUntitledDocumentIsNotAConflict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	e := New(Options{})
	if err := e.OpenFile(a); err != nil {
		t.Fatal(err)
	}
	e.Do("new", nil)
	// The untitled tab has no path, so it cannot collide with the rename target.
	if err := e.RenamePath(a, b); err != nil {
		t.Fatalf("RenamePath: %v", err)
	}
	if e.docs[0].Path != b || e.docs[1].Path != "" {
		t.Fatalf("paths = %q, %q", e.docs[0].Path, e.docs[1].Path)
	}
}

func TestDocumentIndexByPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	if i, ok := e.DocumentIndexByPath(a); !ok || i != 0 {
		t.Fatalf("DocumentIndexByPath(a) = %d, %v, want 0, true", i, ok)
	}
	if i, ok := e.DocumentIndexByPath(b); !ok || i != 1 {
		t.Fatalf("DocumentIndexByPath(b) = %d, %v, want 1, true", i, ok)
	}
	if _, ok := e.DocumentIndexByPath(filepath.Join(dir, "missing.txt")); ok {
		t.Fatal("DocumentIndexByPath found a path that is not open")
	}
	if _, ok := e.DocumentIndexByPath(""); ok {
		t.Fatal("DocumentIndexByPath matched the empty path")
	}
	if i, ok := e.DocumentIndexByPath(dir + "/x/../a.txt"); !ok || i != 0 {
		t.Fatalf("unclean spelling = %d, %v, want 0, true", i, ok)
	}
}

func TestCloseNeedsConfirm(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	putFile(t, a, "a")
	putFile(t, b, "b")
	e := openTwo(t, a, b)
	typeText(t, e, "!")
	status := e.Status()
	before := e.Tabs()

	if got, err := e.CloseNeedsConfirm(1); err != nil || !got {
		t.Fatalf("CloseNeedsConfirm(modified) = %v, %v, want true, nil", got, err)
	}
	if got, err := e.CloseNeedsConfirm(0); err != nil || got {
		t.Fatalf("CloseNeedsConfirm(clean) = %v, %v, want false, nil", got, err)
	}
	if _, err := e.CloseNeedsConfirm(2); !errors.Is(err, ErrNoTab) {
		t.Fatalf("CloseNeedsConfirm(2) err = %v, want ErrNoTab", err)
	}
	if _, err := e.CloseNeedsConfirm(-1); !errors.Is(err, ErrNoTab) {
		t.Fatalf("CloseNeedsConfirm(-1) err = %v, want ErrNoTab", err)
	}
	if e.Status() != status {
		t.Fatalf("CloseNeedsConfirm changed status to %q", e.Status())
	}
	if got := e.Tabs(); !equalTabs(got, before) {
		t.Fatalf("CloseNeedsConfirm changed tabs to %+v", got)
	}
	// The answer agrees with CloseTab: a confirmed tab is refused without force.
	if err := e.CloseTab(1, false); !errors.Is(err, ErrUnsaved) {
		t.Fatalf("CloseTab(1, false) err = %v, want ErrUnsaved", err)
	}
}

// equalTabs reports whether two tab lists hold the same entries.
func equalTabs(a, b []Tab) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
