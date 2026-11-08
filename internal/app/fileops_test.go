package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/fsops"
	expl "github.com/tbxark/gvim/internal/tui/explorer"
)

// fakePaths records the editor follow-ups the adapter asks for. It stands in for
// *editor.Editor, so the tests need no document.
type fakePaths struct {
	checked [][2]string
	renamed [][2]string
	closed  []string
	force   []bool
	err     error
}

func (f *fakePaths) CheckRename(oldPath, newPath string) error {
	f.checked = append(f.checked, [2]string{oldPath, newPath})
	return f.err
}

func (f *fakePaths) RenamePath(oldPath, newPath string) error {
	f.renamed = append(f.renamed, [2]string{oldPath, newPath})
	return f.err
}

func (f *fakePaths) CloseByPath(path string, force bool) error {
	f.closed = append(f.closed, path)
	f.force = append(f.force, force)
	return f.err
}

// tempRoot returns a fresh directory with a file a.txt holding "A" and a
// directory sub/ holding b.txt.
func tempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, "a.txt"), []byte("A"), 0o644))
	must(t, os.Mkdir(filepath.Join(root, "sub"), 0o755))
	must(t, os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("B"), 0o644))
	return root
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestCreateFileAndMkdir(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	ops := newFileOps(&fakePaths{})
	ctx := context.Background()

	p, err := ops.CreateFile(ctx, root, "new.txt")
	must(t, err)
	if p != filepath.Join(root, "new.txt") || !exists(p) {
		t.Fatalf("CreateFile returned %q, exists=%v", p, exists(p))
	}
	if _, err := ops.CreateFile(ctx, root, "new.txt"); !errors.Is(err, expl.ErrConflict) || !errors.Is(err, fsops.ErrConflict) {
		t.Errorf("second create: err = %v, want a conflict matching both sentinels", err)
	}

	d, err := ops.Mkdir(ctx, root, "dir")
	must(t, err)
	if info, err := os.Stat(d); err != nil || !info.IsDir() {
		t.Fatalf("Mkdir did not make %q: %v", d, err)
	}
}

func TestInvalidNameIsRejectedBeforeTheDisk(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	ops := newFileOps(&fakePaths{})
	for _, name := range []string{"a/b", "..", "", "   "} {
		if _, err := ops.CreateFile(context.Background(), root, name); !errors.Is(err, fsops.ErrInvalidName) {
			t.Errorf("CreateFile(%q): err = %v, want ErrInvalidName", name, err)
		}
	}
	entries, err := os.ReadDir(root)
	must(t, err)
	if len(entries) != 2 {
		t.Errorf("invalid names changed the directory: %d entries", len(entries))
	}
}

func TestRenameReturnsNewPath(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	ops := newFileOps(&fakePaths{})
	old := filepath.Join(root, "a.txt")
	np, err := ops.Rename(context.Background(), old, "c.txt")
	must(t, err)
	if np != filepath.Join(root, "c.txt") || exists(old) || !exists(np) {
		t.Fatalf("rename: new=%q oldExists=%v newExists=%v", np, exists(old), exists(np))
	}
}

func TestRenameOntoExistingIsConflict(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	ops := newFileOps(&fakePaths{})
	_, err := ops.Rename(context.Background(), filepath.Join(root, "a.txt"), "sub")
	if !errors.Is(err, expl.ErrConflict) {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if !exists(filepath.Join(root, "a.txt")) {
		t.Error("failed rename removed the source")
	}
}

func TestMoveIntoDirectory(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	ops := newFileOps(&fakePaths{})
	dst, err := ops.Move(context.Background(), filepath.Join(root, "a.txt"), filepath.Join(root, "sub"))
	must(t, err)
	if dst != filepath.Join(root, "sub", "a.txt") || !exists(dst) || exists(filepath.Join(root, "a.txt")) {
		t.Fatalf("move: dst=%q", dst)
	}
}

func TestCopyPoliciesAndSameDirectoryName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("cancel reports conflict and keeps the destination", func(t *testing.T) {
		t.Parallel()
		root := tempRoot(t)
		must(t, os.WriteFile(filepath.Join(root, "sub", "a.txt"), []byte("old"), 0o644))
		ops := newFileOps(&fakePaths{})
		_, err := ops.Copy(ctx, filepath.Join(root, "a.txt"), filepath.Join(root, "sub"), expl.ConflictCancel)
		if !errors.Is(err, expl.ErrConflict) {
			t.Fatalf("err = %v, want a conflict", err)
		}
		if b, _ := os.ReadFile(filepath.Join(root, "sub", "a.txt")); string(b) != "old" {
			t.Errorf("destination changed to %q", b)
		}
	})

	t.Run("overwrite replaces the destination", func(t *testing.T) {
		t.Parallel()
		root := tempRoot(t)
		must(t, os.WriteFile(filepath.Join(root, "sub", "a.txt"), []byte("old"), 0o644))
		ops := newFileOps(&fakePaths{})
		dst, err := ops.Copy(ctx, filepath.Join(root, "a.txt"), filepath.Join(root, "sub"), expl.ConflictOverwrite)
		must(t, err)
		if b, _ := os.ReadFile(dst); string(b) != "A" {
			t.Errorf("overwrite left %q", b)
		}
	})

	t.Run("skip leaves the destination and reports no error", func(t *testing.T) {
		t.Parallel()
		root := tempRoot(t)
		must(t, os.WriteFile(filepath.Join(root, "sub", "a.txt"), []byte("old"), 0o644))
		ops := newFileOps(&fakePaths{})
		_, err := ops.Copy(ctx, filepath.Join(root, "a.txt"), filepath.Join(root, "sub"), expl.ConflictSkip)
		must(t, err)
		if b, _ := os.ReadFile(filepath.Join(root, "sub", "a.txt")); string(b) != "old" {
			t.Errorf("skip changed the destination to %q", b)
		}
	})

	t.Run("same directory gets a free name", func(t *testing.T) {
		t.Parallel()
		root := tempRoot(t)
		ops := newFileOps(&fakePaths{})
		dst, err := ops.Copy(ctx, filepath.Join(root, "a.txt"), root, expl.ConflictCancel)
		must(t, err)
		if dst != filepath.Join(root, "a copy.txt") || !exists(filepath.Join(root, "a.txt")) {
			t.Errorf("copy into the same directory: dst=%q", dst)
		}
	})

	t.Run("unknown policy is an error", func(t *testing.T) {
		t.Parallel()
		root := tempRoot(t)
		ops := newFileOps(&fakePaths{})
		if _, err := ops.Copy(ctx, filepath.Join(root, "a.txt"), root, expl.Conflict(0)); err == nil {
			t.Error("unknown policy accepted")
		}
	})
}

func TestTrashMissingPathFails(t *testing.T) {
	t.Parallel()
	ops := newFileOps(&fakePaths{})
	if err := ops.Trash(context.Background(), filepath.Join(t.TempDir(), "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("trash of a missing path: err = %v, want not-exist", err)
	}
}

// TestCheckRenameRefusesAnOpenTab checks the editor's dry run, which the root
// calls in Update before a rename: a target held by another tab is refused, and
// nothing on disk or in the tabs changes.
func TestCheckRenameRefusesAnOpenTab(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(a))
	must(t, ed.OpenFile(b))
	ops := newFileOps(ed)

	if err := ops.CheckRename(a, b); !errors.Is(err, editor.ErrAlreadyOpen) {
		t.Fatalf("CheckRename: err = %v, want editor.ErrAlreadyOpen", err)
	}
	if !exists(a) || exists(filepath.Join(root, "c.txt")) {
		t.Error("the dry run touched the disk")
	}
	if got := ed.Tabs()[0].Path; got != a {
		t.Errorf("tab A moved to %q", got)
	}
}

// TestCheckRenameRefusesAMoveOntoAnOpenTab is the move case of the dry run.
func TestCheckRenameRefusesAMoveOntoAnOpenTab(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	a := filepath.Join(root, "a.txt")
	inSub := filepath.Join(root, "sub", "a.txt")
	ed := editor.New(editor.Options{})
	must(t, ed.OpenFile(a))
	must(t, ed.OpenFile(inSub))
	ops := newFileOps(ed)

	if err := ops.CheckRename(a, inSub); !errors.Is(err, editor.ErrAlreadyOpen) {
		t.Fatalf("CheckRename: err = %v, want editor.ErrAlreadyOpen", err)
	}
}

// TestRenameDoesNotAskTheEditor pins the split: the disk operation itself does
// not read the open tabs, so only the root's Update-side check decides.
func TestRenameDoesNotAskTheEditor(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	a := filepath.Join(root, "a.txt")
	paths := &fakePaths{err: errors.New("refused by editor")}
	ops := newFileOps(paths)

	if _, err := ops.Rename(context.Background(), a, "c.txt"); err != nil {
		t.Fatalf("Rename: err = %v, want the disk rename to succeed", err)
	}
	if len(paths.checked) != 0 {
		t.Errorf("Rename asked the editor: %v", paths.checked)
	}
	if exists(a) || !exists(filepath.Join(root, "c.txt")) {
		t.Error("rename did not move the file")
	}
}

// TestCheckRenameAsksTheEditorWithTheDestination checks that the adapter passes
// the destination it will use: the same path Rename and Move would create.
func TestCheckRenameAsksTheEditorWithTheDestination(t *testing.T) {
	t.Parallel()
	root := tempRoot(t)
	a := filepath.Join(root, "a.txt")
	c := filepath.Join(root, "c.txt")
	inSub := filepath.Join(root, "sub", "a.txt")
	refused := errors.New("refused by editor")
	paths := &fakePaths{err: refused}
	ops := newFileOps(paths)

	if err := ops.CheckRename(a, c); !errors.Is(err, refused) {
		t.Errorf("CheckRename(rename): err = %v, want the editor's refusal", err)
	}
	if err := ops.CheckRename(a, inSub); !errors.Is(err, refused) {
		t.Errorf("CheckRename(move): err = %v, want the editor's refusal", err)
	}
	if want := [][2]string{{a, c}, {a, inSub}}; !slices.Equal(paths.checked, want) {
		t.Errorf("CheckRename calls = %v, want %v", paths.checked, want)
	}
}

func TestEditorFollowUpsUseTheEditor(t *testing.T) {
	t.Parallel()
	paths := &fakePaths{}
	ops := newFileOps(paths)
	must(t, ops.FollowRename("/r/a", "/r/b"))
	must(t, ops.CloseDeleted("/r/gone"))
	if len(paths.renamed) != 1 || paths.renamed[0] != [2]string{"/r/a", "/r/b"} {
		t.Errorf("RenamePath calls = %v", paths.renamed)
	}
	if len(paths.closed) != 1 || paths.closed[0] != "/r/gone" || paths.force[0] {
		t.Errorf("CloseByPath calls = %v force=%v; want /r/gone without force", paths.closed, paths.force)
	}
}

func TestClassifyMarksConflicts(t *testing.T) {
	t.Parallel()
	inner := errors.New("fsops: copy x: destination already exists")
	err := classify(fsops.ErrConflict)
	if err == nil || !errors.Is(err, fsops.ErrConflict) || !errors.Is(err, expl.ErrConflict) {
		t.Fatalf("classify did not mark the conflict: %v", err)
	}
	if got := classify(inner); got != inner {
		t.Errorf("non-conflict error was wrapped: %v", got)
	}
	if classify(nil) != nil {
		t.Error("classify(nil) is not nil")
	}
}

// TestFileOpsHasTheRootPortMethods checks the method set the root asserts on
// Deps.Ops (tui.explorerOps). The root's assertion is silent when it fails, so a
// missing method would only show as "not supported" at run time.
func TestFileOpsHasTheRootPortMethods(t *testing.T) {
	t.Parallel()
	var port any = newFileOps(&fakePaths{})
	_, ok := port.(interface {
		Mkdir(ctx context.Context, dir, name string) (string, error)
		Move(ctx context.Context, src, dstDir string) (string, error)
		Copy(ctx context.Context, src, dstDir string, c expl.Conflict) (string, error)
		FollowRename(oldPath, newPath string) error
		CloseDeleted(path string) error
	})
	if !ok {
		t.Fatal("fileOps lacks a method the root asserts")
	}
}
