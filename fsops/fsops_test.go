package fsops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestCreateFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "new.txt")

	if err := CreateFile(path); err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Size() != 0 {
		t.Fatalf("want empty regular file, got mode %v size %d", info.Mode(), info.Size())
	}

	err = CreateFile(path)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second CreateFile: want ErrConflict, got %v", err)
	}

	err = CreateFile(filepath.Join(root, "missing", "x.txt"))
	if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrConflict) {
		t.Fatalf("missing parent: want ErrNotExist without ErrConflict, got %v", err)
	}
}

func TestCreateFileDoesNotTruncate(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "keep.txt")
	writeFile(t, path, "data")

	if err := CreateFile(path); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if got := readFile(t, path); got != "data" {
		t.Fatalf("existing file changed: %q", got)
	}
}

func TestMkdir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "sub")

	if err := Mkdir(dir); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("want directory, got %v, %v", info, err)
	}
	if err := Mkdir(dir); !errors.Is(err, ErrConflict) {
		t.Fatalf("second Mkdir: want ErrConflict, got %v", err)
	}

	file := filepath.Join(root, "file")
	writeFile(t, file, "x")
	if err := Mkdir(file); !errors.Is(err, ErrConflict) {
		t.Fatalf("Mkdir over file: want ErrConflict, got %v", err)
	}
	if err := Mkdir(filepath.Join(root, "a", "b")); errors.Is(err, ErrConflict) || err == nil {
		t.Fatalf("Mkdir without parent: want a non-conflict error, got %v", err)
	}
}

func TestRename(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "a.txt")
	newPath := filepath.Join(root, "b.txt")
	writeFile(t, oldPath, "alpha")

	if err := RenameNoReplace(oldPath, newPath); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if exists(oldPath) || readFile(t, newPath) != "alpha" {
		t.Fatal("rename did not move contents")
	}
}

func TestRenameToSameNameIsNoop(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	writeFile(t, path, "alpha")

	if err := RenameNoReplace(path, path); err != nil {
		t.Fatalf("Rename to itself: %v", err)
	}
	if readFile(t, path) != "alpha" {
		t.Fatal("contents changed")
	}
}

func TestRenameConflictKeepsBoth(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "a.txt")
	newPath := filepath.Join(root, "b.txt")
	writeFile(t, oldPath, "alpha")
	writeFile(t, newPath, "beta")

	if err := RenameNoReplace(oldPath, newPath); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if readFile(t, oldPath) != "alpha" || readFile(t, newPath) != "beta" {
		t.Fatal("conflicting rename changed a file")
	}
}

func TestRenameDirectoryKeepsContents(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	if err := Mkdir(dir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "main.go"), "package main")

	if err := RenameNoReplace(dir, filepath.Join(root, "lib")); err != nil {
		t.Fatalf("Rename dir: %v", err)
	}
	if got := readFile(t, filepath.Join(root, "lib", "main.go")); got != "package main" {
		t.Fatalf("child contents = %q", got)
	}
}

func TestRenameMissingSource(t *testing.T) {
	root := t.TempDir()
	err := RenameNoReplace(filepath.Join(root, "nope"), filepath.Join(root, "x"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}

func TestMove(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dstDir := filepath.Join(root, "dest")
	writeFile(t, src, "alpha")
	if err := Mkdir(dstDir); err != nil {
		t.Fatal(err)
	}

	if err := MoveNoReplace(src, dstDir); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if exists(src) || readFile(t, filepath.Join(dstDir, "a.txt")) != "alpha" {
		t.Fatal("move did not relocate the file")
	}
}

func TestMoveSameDirectoryIsNoop(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	writeFile(t, src, "alpha")

	if err := MoveNoReplace(src, root); err != nil {
		t.Fatalf("Move within same dir: %v", err)
	}
	if readFile(t, src) != "alpha" {
		t.Fatal("same-directory move changed the file")
	}
}

func TestMoveConflictKeepsBoth(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dstDir := filepath.Join(root, "dest")
	writeFile(t, src, "alpha")
	if err := Mkdir(dstDir); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dstDir, "a.txt"), "beta")

	if err := MoveNoReplace(src, dstDir); !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	if readFile(t, src) != "alpha" || readFile(t, filepath.Join(dstDir, "a.txt")) != "beta" {
		t.Fatal("conflicting move changed a file")
	}
}

func TestMoveRejectsNonDirectoryTarget(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	notDir := filepath.Join(root, "b.txt")
	writeFile(t, src, "alpha")
	writeFile(t, notDir, "beta")

	if err := MoveNoReplace(src, notDir); !errors.Is(err, ErrInvalidDest) {
		t.Fatalf("want ErrInvalidDest, got %v", err)
	}
	if !exists(src) {
		t.Fatal("source removed by rejected move")
	}
}

func TestMoveDirectoryIntoItselfRejected(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "outer")
	inner := filepath.Join(dir, "inner")
	if err := Mkdir(dir); err != nil {
		t.Fatal(err)
	}
	if err := Mkdir(inner); err != nil {
		t.Fatal(err)
	}

	if err := MoveNoReplace(dir, inner); !errors.Is(err, ErrInvalidDest) {
		t.Fatalf("want ErrInvalidDest, got %v", err)
	}
	if !exists(inner) {
		t.Fatal("directory removed by rejected move")
	}
}

func TestMoveMissingSource(t *testing.T) {
	root := t.TempDir()
	if err := Mkdir(filepath.Join(root, "dest")); err != nil {
		t.Fatal(err)
	}
	err := MoveNoReplace(filepath.Join(root, "nope"), filepath.Join(root, "dest"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
}
