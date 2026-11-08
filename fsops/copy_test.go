package fsops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

func TestCopySymlinkOntoItsTargetRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	cases := []struct {
		name   string
		dirDst bool // dst is a directory holding sub/leaf.txt, not a file
		slash  bool // the source path ends with a separator
		// target returns the link target, relative to root or absolute.
		target  func(root string) string
		refused bool
	}{
		{
			name:    "absolute target equal to dst",
			target:  func(root string) string { return filepath.Join(root, "dst") },
			refused: true,
		},
		{
			name:    "relative target equal to dst",
			target:  func(string) string { return "dst" },
			refused: true,
		},
		{
			name:    "target file inside dst directory",
			dirDst:  true,
			target:  func(string) string { return filepath.Join("dst", "sub", "leaf.txt") },
			refused: true,
		},
		{
			name:    "target directory inside dst directory",
			dirDst:  true,
			target:  func(string) string { return filepath.Join("dst", "sub") },
			refused: true,
		},
		{
			name:    "target directory inside dst reached through an alias",
			dirDst:  true,
			target:  func(string) string { return filepath.Join("alias", "sub") },
			refused: true,
		},
		{
			name:    "target directory inside dst with trailing slash on source",
			dirDst:  true,
			slash:   true,
			target:  func(string) string { return filepath.Join("dst", "sub") },
			refused: true,
		},
		{
			name:    "target hard link to dst file",
			target:  func(string) string { return "twin" },
			refused: true,
		},
		{
			name:   "link to a different file still copies",
			target: func(string) string { return "other.txt" },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dst := filepath.Join(root, "dst")
			kept, content := dst, "old"
			if tc.dirDst {
				must(t, Mkdir(dst))
				must(t, Mkdir(filepath.Join(dst, "sub")))
				kept = filepath.Join(dst, "sub", "leaf.txt")
				content = "leaf"
				writeFile(t, kept, content)
			} else {
				writeFile(t, dst, content)
			}
			writeFile(t, filepath.Join(root, "other.txt"), "other")
			if !tc.dirDst {
				must(t, os.Link(dst, filepath.Join(root, "twin")))
			}
			must(t, os.Symlink("dst", filepath.Join(root, "alias")))
			link := filepath.Join(root, "link")
			must(t, os.Symlink(tc.target(root), link))
			src := link
			if tc.slash {
				src += string(filepath.Separator)
			}

			err := Copy(src, dst, Overwrite)
			if tc.refused {
				if !errors.Is(err, ErrInvalidDest) {
					t.Fatalf("want ErrInvalidDest, got %v", err)
				}
				if got := readFile(t, kept); got != content {
					t.Fatalf("%s = %q after refused copy, want %q", kept, got, content)
				}
				return
			}
			if err != nil {
				t.Fatalf("Copy: %v", err)
			}
			got, err := os.Readlink(dst)
			if err != nil || got != tc.target(root) {
				t.Fatalf("dst link target = %q, %v; want %q", got, err, tc.target(root))
			}
			if readFile(t, filepath.Join(root, "other.txt")) != "other" {
				t.Fatal("link target changed")
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestCopyEmptySourceFails(t *testing.T) {
	root := t.TempDir()
	dst := filepath.Join(root, "dst")

	if err := Copy("", dst, Overwrite); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
	if exists(dst) {
		t.Fatal("empty source created a destination")
	}
}

func TestCopySymlinkOntoMissingItsTargetRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	src := filepath.Join(root, "link")
	must(t, os.Symlink("dst", src))

	if err := Copy(src, dst, Cancel); !errors.Is(err, ErrInvalidDest) {
		t.Fatalf("want ErrInvalidDest, got %v", err)
	}
	if exists(dst) {
		t.Fatal("refused copy created a self-referencing link at dst")
	}
}

func TestCopySymlinkThroughSymlinkedParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	cases := []struct {
		name    string
		dst     string // destination, relative to root
		target  string // link target, relative to the real directory of the link
		refused bool
	}{
		{
			name:    "target names the overwritten file",
			dst:     filepath.Join("real", "x"),
			target:  "../x",
			refused: true,
		},
		{
			name:   "target names another file in the same directory",
			dst:    filepath.Join("real", "x"),
			target: "../other.txt",
		},
		{
			name:   "same name in the lexical directory is a different file",
			dst:    "x",
			target: "../x",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			must(t, Mkdir(filepath.Join(root, "real")))
			must(t, Mkdir(filepath.Join(root, "real", "deep")))
			writeFile(t, filepath.Join(root, "real", "x"), "real x")
			writeFile(t, filepath.Join(root, "real", "other.txt"), "other")
			writeFile(t, filepath.Join(root, "x"), "root x")
			must(t, os.Symlink(filepath.Join("real", "deep"), filepath.Join(root, "alias")))
			must(t, os.Symlink(tc.target, filepath.Join(root, "real", "deep", "link")))
			src := filepath.Join(root, "alias", "link")
			dst := filepath.Join(root, tc.dst)

			err := Copy(src, dst, Overwrite)
			if tc.refused {
				if !errors.Is(err, ErrInvalidDest) {
					t.Fatalf("want ErrInvalidDest, got %v", err)
				}
				if got := readFile(t, filepath.Join(root, "real", "x")); got != "real x" {
					t.Fatalf("overwritten file = %q after refused copy, want %q", got, "real x")
				}
				return
			}
			if err != nil {
				t.Fatalf("Copy: %v", err)
			}
			if got, err := os.Readlink(dst); err != nil || got != tc.target {
				t.Fatalf("dst link target = %q, %v; want %q", got, err, tc.target)
			}
		})
	}
}

func TestCopySymlinkOntoLinkToDestRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	root := t.TempDir()
	dst := filepath.Join(root, "dst")
	must(t, os.Symlink("elsewhere.txt", dst))
	writeFile(t, filepath.Join(root, "elsewhere.txt"), "kept")
	src := filepath.Join(root, "link")
	must(t, os.Symlink("dst", src))

	if err := Copy(src, dst, Overwrite); !errors.Is(err, ErrInvalidDest) {
		t.Fatalf("want ErrInvalidDest, got %v", err)
	}
	if got, err := os.Readlink(dst); err != nil || got != "elsewhere.txt" {
		t.Fatalf("dst link target = %q, %v; want elsewhere.txt", got, err)
	}
	if got := readFile(t, filepath.Join(root, "elsewhere.txt")); got != "kept" {
		t.Fatalf("elsewhere.txt = %q after refused copy, want kept", got)
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

// errInjected is the failure that injectRename returns.
var errInjected = errors.New("injected rename failure")

// injectRename makes renamePath fail for renames whose destination is dst.
// With once set, only the first such rename fails; otherwise all of them do.
func injectRename(t *testing.T, dst string, once bool) {
	t.Helper()
	orig := renamePath
	t.Cleanup(func() { renamePath = orig })
	failed := false
	renamePath = func(oldPath, newPath string) error {
		if newPath == dst && (!once || !failed) {
			failed = true
			return errInjected
		}
		return orig(oldPath, newPath)
	}
}

// backupDirs returns the names of the backup directories Copy leaves in root.
func backupDirs(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	for _, name := range dirNames(t, root) {
		if strings.HasPrefix(name, ".fsops-backup-") {
			out = append(out, name)
		}
	}
	return out
}

// makeWritable restores owner access to every directory under root so that
// t.TempDir can remove the tree after a test made some of it read-only.
func makeWritable(t *testing.T, root string) {
	t.Helper()
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
}

func TestCopyConflictOverwriteLeavesOnlyNewContent(t *testing.T) {
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
	if got := dirNames(t, root); !slices.Equal(got, []string{"dst", "src"}) {
		t.Fatalf("backup or staging left behind: %v", got)
	}
}

func TestCopyOverwriteCleanupFailureKeepsOldFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only mode this test relies on")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	must(t, Mkdir(src))
	writeFile(t, filepath.Join(src, "new.txt"), "new")
	must(t, Mkdir(dst))
	writeFile(t, filepath.Join(dst, "top.txt"), "old")
	locked := filepath.Join(dst, "locked")
	must(t, Mkdir(locked))
	writeFile(t, filepath.Join(locked, "kept.txt"), "old")
	must(t, os.Chmod(locked, 0o500))
	t.Cleanup(func() { makeWritable(t, root) })

	err := Copy(src, dst, Overwrite)
	if err == nil {
		t.Fatal("want error: the old destination cannot be fully removed")
	}
	backups := backupDirs(t, root)
	if len(backups) != 1 {
		t.Fatalf("want the old destination kept in one backup directory, got %v", backups)
	}
	backup := filepath.Join(root, backups[0], "dst")
	if !strings.Contains(err.Error(), backup) {
		t.Fatalf("error %q does not name the backup %s", err, backup)
	}
	if got := readFile(t, filepath.Join(backup, "locked", "kept.txt")); got != "old" {
		t.Fatalf("original file = %q, want old", got)
	}
	if got := readFile(t, filepath.Join(dst, "new.txt")); got != "new" {
		t.Fatalf("dst new.txt = %q, want the new copy", got)
	}
}

func TestCopyOverwriteFailedSwapKeepsDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	must(t, Mkdir(src))
	writeFile(t, filepath.Join(src, "new.txt"), "new")
	must(t, Mkdir(dst))
	writeFile(t, filepath.Join(dst, "old.txt"), "old")
	injectRename(t, dst, true)

	err := Copy(src, dst, Overwrite)
	if !errors.Is(err, errInjected) {
		t.Fatalf("want injected failure, got %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "old.txt")); got != "old" {
		t.Fatalf("destination changed after failed swap: old.txt = %q", got)
	}
	if got := dirNames(t, root); !slices.Equal(got, []string{"dst", "src"}) {
		t.Fatalf("backup or staging left behind: %v", got)
	}
}

func TestCopyOverwriteRollbackFailureNamesBackup(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	must(t, Mkdir(src))
	writeFile(t, filepath.Join(src, "new.txt"), "new")
	must(t, Mkdir(dst))
	writeFile(t, filepath.Join(dst, "old.txt"), "old")
	injectRename(t, dst, false)

	err := Copy(src, dst, Overwrite)
	backups := backupDirs(t, root)
	if len(backups) != 1 {
		t.Fatalf("want the old destination kept in one backup directory, got %v", backups)
	}
	backup := filepath.Join(root, backups[0], "dst")
	if err == nil || !strings.Contains(err.Error(), backup) {
		t.Fatalf("error %v does not name the backup %s", err, backup)
	}
	if got := readFile(t, filepath.Join(backup, "old.txt")); got != "old" {
		t.Fatalf("old destination in backup = %q, want old", got)
	}
}
