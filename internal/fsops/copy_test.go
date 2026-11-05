package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func dirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

func TestCopyFileKeepsModeAndContents(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "run.sh")
	dst := filepath.Join(root, "copy.sh")
	if err := os.WriteFile(src, []byte("#!/bin/sh\n"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := Copy(src, dst, Overwrite); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if readFile(t, dst) != "#!/bin/sh\n" {
		t.Fatal("contents differ")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o750 {
			t.Fatalf("mode = %v, want 0750", info.Mode().Perm())
		}
	}
	if got := dirNames(t, root); !slices.Equal(got, []string{"copy.sh", "run.sh"}) {
		t.Fatalf("staging left behind: %v", got)
	}
}

func TestCopyDirectoryRecursive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(src, "pkg", "deep"), 0o755))
	writeFile(t, filepath.Join(src, "main.go"), "package main")
	writeFile(t, filepath.Join(src, "pkg", "deep", "x.txt"), "deep")
	dst := filepath.Join(root, "src copy")

	if err := Copy(src, dst, Cancel); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "pkg", "deep", "x.txt")); got != "deep" {
		t.Fatalf("nested contents = %q", got)
	}
	if got := readFile(t, filepath.Join(dst, "main.go")); got != "package main" {
		t.Fatalf("top-level contents = %q", got)
	}
	if !exists(src) {
		t.Fatal("source removed by copy")
	}
}

func TestCopySymlinkNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	must(t, Mkdir(src))
	must(t, os.Symlink("..", filepath.Join(src, "up")))
	dst := filepath.Join(root, "dst")

	if err := Copy(src, dst, Cancel); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	target, err := os.Readlink(filepath.Join(dst, "up"))
	if err != nil || target != ".." {
		t.Fatalf("link target = %q, %v; want \"..\"", target, err)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCopyConflictOverwriteReplacesFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "new")
	writeFile(t, dst, "old")

	if err := Copy(src, dst, Overwrite); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if readFile(t, dst) != "new" {
		t.Fatalf("dst = %q, want new", readFile(t, dst))
	}
	if got := dirNames(t, root); !slices.Equal(got, []string{"a.txt", "b.txt"}) {
		t.Fatalf("staging left behind: %v", got)
	}
}

func TestCopyConflictOverwriteReplacesDirectory(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	must(t, Mkdir(src))
	must(t, Mkdir(dst))
	writeFile(t, filepath.Join(src, "new.txt"), "new")
	writeFile(t, filepath.Join(dst, "stale.txt"), "stale")

	if err := Copy(src, dst, Overwrite); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if got := dirNames(t, dst); !slices.Equal(got, []string{"new.txt"}) {
		t.Fatalf("dst contents = %v, want only new.txt", got)
	}
}

func TestCopyConflictSkipLeavesDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "new")
	writeFile(t, dst, "old")

	if err := Copy(src, dst, Skip); err != nil {
		t.Fatalf("Skip should return nil, got %v", err)
	}
	if readFile(t, dst) != "old" {
		t.Fatal("skip changed the destination")
	}
}

func TestCopyConflictCancelReportsBothSentinels(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "new")
	writeFile(t, dst, "old")

	err := Copy(src, dst, Cancel)
	if !errors.Is(err, ErrCanceled) || !errors.Is(err, ErrConflict) {
		t.Fatalf("want ErrCanceled and ErrConflict, got %v", err)
	}
	if readFile(t, dst) != "old" {
		t.Fatal("cancel changed the destination")
	}
}

func TestCopyRejectsSelfAndNesting(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	file := filepath.Join(root, "file.txt")
	inner := filepath.Join(dir, "inner.txt")
	must(t, Mkdir(dir))
	writeFile(t, file, "alpha")
	writeFile(t, inner, "inner")

	cases := []struct {
		name     string
		src, dst string
		policy   ConflictPolicy
	}{
		{"onto itself", file, file, Overwrite},
		{"into own child", dir, filepath.Join(dir, "child"), Overwrite},
		{"over own parent", inner, dir, Overwrite},
		{"dir over itself", dir, dir, Skip},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Copy(tc.src, tc.dst, tc.policy)
			if !errors.Is(err, ErrInvalidDest) {
				t.Fatalf("want ErrInvalidDest, got %v", err)
			}
		})
	}
	if readFile(t, file) != "alpha" {
		t.Fatal("source file was modified")
	}
	if !exists(dir) {
		t.Fatal("source directory was removed")
	}
}

func TestCopyRejectsUnknownPolicy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	writeFile(t, src, "x")

	if err := Copy(src, filepath.Join(root, "b.txt"), ConflictPolicy(0)); err == nil {
		t.Fatal("zero policy should be rejected")
	}
	if exists(filepath.Join(root, "b.txt")) {
		t.Fatal("rejected copy created a file")
	}
}

func TestCopyMissingParentFails(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	writeFile(t, src, "x")

	err := Copy(src, filepath.Join(root, "missing", "b.txt"), Overwrite)
	if err == nil {
		t.Fatal("want error for missing parent")
	}
	if got := dirNames(t, root); !slices.Equal(got, []string{"a.txt"}) {
		t.Fatalf("root changed: %v", got)
	}
}

func TestCopyNameFirstAndNumbered(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "alpha.txt"), "x")

	got, err := CopyName(root, "alpha.txt")
	if err != nil || got != "alpha copy.txt" {
		t.Fatalf("CopyName = %q, %v; want alpha copy.txt", got, err)
	}
	writeFile(t, filepath.Join(root, "alpha copy.txt"), "y")
	got, err = CopyName(root, "alpha.txt")
	if err != nil || got != "alpha copy 2.txt" {
		t.Fatalf("CopyName = %q, %v; want alpha copy 2.txt", got, err)
	}
}

func TestCopyNameTable(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name, want string
	}{
		{"alpha.txt", "alpha copy.txt"},
		{".bashrc", ".bashrc copy"},
		{"archive.tar.gz", "archive.tar copy.gz"},
		{"Makefile", "Makefile copy"},
		{"src", "src copy"},
	}
	for _, tc := range cases {
		got, err := CopyName(root, tc.name)
		if err != nil || got != tc.want {
			t.Errorf("CopyName(%q) = %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestCopyNameRejectsInvalidName(t *testing.T) {
	root := t.TempDir()
	if _, err := CopyName(root, "a/b"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("want ErrInvalidName, got %v", err)
	}
}

func TestCopyNameSkipsDanglingSymlinkName(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	must(t, os.Symlink("nowhere", filepath.Join(root, "alpha copy.txt")))

	got, err := CopyName(root, "alpha.txt")
	if err != nil || got != "alpha copy 2.txt" {
		t.Fatalf("CopyName = %q, %v; want alpha copy 2.txt", got, err)
	}
}

func TestConflictPolicyString(t *testing.T) {
	cases := map[ConflictPolicy]string{
		Overwrite:         "Overwrite",
		Skip:              "Skip",
		Cancel:            "Cancel",
		ConflictPolicy(0): "ConflictPolicy(0)",
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(p), got, want)
		}
	}
}
