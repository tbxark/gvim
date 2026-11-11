package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestCopyConcurrentDestinationIsExclusive: a free destination is created with one
// O_EXCL/Mkdir claim, so two copies racing for it cannot both succeed or overwrite each
// other.
func TestCopyConcurrentDestinationIsExclusive(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	writeFile(t, src, "src")
	dst := filepath.Join(root, "dst.txt")
	results := make(chan error, 2)
	for range 2 {
		go func() { results <- Copy(src, dst, Cancel) }()
	}
	var wins, conflicts int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent copy error = %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent copy wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
	if got := readFile(t, dst); got != "src" {
		t.Fatalf("destination content = %q", got)
	}
}

// TestRenameNoReplaceConcurrentDestinationIsExclusive: two bounded copy-and-remove
// renames racing for one target publish it once; the loser keeps its source.
func TestRenameNoReplaceConcurrentDestinationIsExclusive(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a.txt")
	b := filepath.Join(root, "b.txt")
	writeFile(t, a, "a")
	writeFile(t, b, "b")
	dst := filepath.Join(root, "x.txt")
	results := make(chan error, 2)
	for _, src := range []string{a, b} {
		go func(src string) { results <- RenameNoReplace(src, dst) }(src)
	}
	var wins, conflicts int
	for range 2 {
		err := <-results
		switch {
		case err == nil:
			wins++
		case errors.Is(err, ErrConflict):
			conflicts++
		default:
			t.Fatalf("concurrent rename error = %v", err)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("concurrent rename wins=%d conflicts=%d, want 1/1", wins, conflicts)
	}
	if data := readFile(t, dst); data != "a" && data != "b" {
		t.Fatalf("winner content = %q", data)
	}
	if exists(a) == exists(b) {
		t.Fatal("exactly one source should remain after the failed rename")
	}
}

// TestRenameNoReplaceExistingTargetRefused: an existing target is a conflict and neither
// side changes.
func TestRenameNoReplaceExistingTargetRefused(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "a")
	writeFile(t, dst, "b")
	if err := RenameNoReplace(src, dst); !errors.Is(err, ErrConflict) {
		t.Fatalf("rename error = %v, want a conflict", err)
	}
	if readFile(t, src) != "a" || readFile(t, dst) != "b" {
		t.Fatal("a refused rename changed a file")
	}
}

// TestRenameNoReplaceFailureKeepsSource: a write failure leaves the source in place and
// reports the error instead of silently removing it.
func TestRenameNoReplaceFailureKeepsSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission mode is not enforced the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only mode this test relies on")
	}
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	writeFile(t, src, "a")
	locked := filepath.Join(root, "locked")
	must(t, os.Mkdir(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if err := RenameNoReplace(src, filepath.Join(locked, "b.txt")); err == nil {
		t.Fatal("rename into a read-only directory was accepted")
	}
	if got := readFile(t, src); got != "a" {
		t.Fatalf("failed rename changed the source: %q", got)
	}
}

// TestMoveNoReplaceConflictKeepsBoth: cut/paste uses the same no-replace publish path.
func TestMoveNoReplaceConflictKeepsBoth(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dstDir := filepath.Join(root, "dest")
	writeFile(t, src, "a")
	must(t, os.Mkdir(dstDir, 0o755))
	writeFile(t, filepath.Join(dstDir, "a.txt"), "dst")
	if err := MoveNoReplace(src, dstDir); !errors.Is(err, ErrConflict) {
		t.Fatalf("move error = %v, want a conflict", err)
	}
	if readFile(t, src) != "a" || readFile(t, filepath.Join(dstDir, "a.txt")) != "dst" {
		t.Fatal("a refused move changed a file")
	}
}

// TestCopyUnwritableDestinationKeepsSource covers the local no-replace copy error path:
// a permission failure is reported and nothing is silently removed.
func TestCopyUnwritableDestinationKeepsSource(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission mode is not enforced the same way on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only mode this test relies on")
	}
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	writeFile(t, src, "a")
	locked := filepath.Join(root, "locked")
	must(t, os.Mkdir(locked, 0o500))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	if err := Copy(src, filepath.Join(locked, "b.txt"), Cancel); err == nil {
		t.Fatal("copy into a read-only directory was accepted")
	}
	if got := readFile(t, src); got != "a" {
		t.Fatalf("failed copy changed the source: %q", got)
	}
}

// TestLocalRenameMoveNoReplaceSameName checks the no-op edges.
func TestLocalRenameMoveNoReplaceSameName(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	writeFile(t, src, "a")
	if err := RenameNoReplace(src, src); err != nil {
		t.Fatalf("same-name rename: %v", err)
	}
	if err := MoveNoReplace(src, root); err != nil {
		t.Fatalf("same-directory move: %v", err)
	}
	if got := readFile(t, src); got != "a" {
		t.Fatalf("no-op changed the file: %q", got)
	}
}

// TestRenameNoReplaceBoundsBeforeCopy: the local bounded copy-and-remove refuses an
// over-deep tree before creating a destination or removing the source.
func TestRenameNoReplaceBoundsBeforeCopy(t *testing.T) {
	forceRenameCopy(t)
	root := t.TempDir()
	src := filepath.Join(root, "src")
	cur := src
	must(t, os.Mkdir(cur, 0o755))
	for range localRenameMaxDepth + 1 {
		cur = filepath.Join(cur, "d")
		must(t, os.Mkdir(cur, 0o755))
	}
	writeFile(t, filepath.Join(cur, "deep.txt"), "deep")
	dst := filepath.Join(root, "dst")
	if err := RenameNoReplace(src, dst); err == nil || !strings.Contains(err.Error(), "deeper than") {
		t.Fatalf("bounded rename error = %v", err)
	}
	if !exists(src) || exists(dst) {
		t.Fatalf("bounded rename changed the tree: src=%v dst=%v", exists(src), exists(dst))
	}
}

// TestRenameNoReplaceRenamesATreeOverTheCopyBounds checks a tree the copy would refuse
// is renamed in one step where the platform has a no-replace rename.
func TestRenameNoReplaceRenamesATreeOverTheCopyBounds(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	cur := src
	must(t, os.Mkdir(cur, 0o755))
	for range localRenameMaxDepth + 1 {
		cur = filepath.Join(cur, "d")
		must(t, os.Mkdir(cur, 0o755))
	}
	writeFile(t, filepath.Join(cur, "deep.txt"), "deep")
	if errors.Is(renameNative(filepath.Join(root, "none"), filepath.Join(root, "none2")), errNoNativeRename) {
		t.Skip("no native no-replace rename on this platform")
	}
	dst := filepath.Join(root, "dst")
	if err := RenameNoReplace(src, dst); err != nil {
		t.Fatalf("rename of a deep tree = %v", err)
	}
	if exists(src) || !exists(dst) {
		t.Fatalf("rename did not move the tree: src=%v dst=%v", exists(src), exists(dst))
	}
}

// TestRenameNoReplaceCaseOnly checks a rename that only changes the case of the name
// works, also on a case-insensitive filesystem where the "target" is the source.
func TestRenameNoReplaceCaseOnly(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	writeFile(t, src, "content")
	dst := filepath.Join(root, "A.txt")
	if err := RenameNoReplace(src, dst); err != nil {
		t.Fatalf("case-only rename = %v", err)
	}
	if got := readFile(t, dst); got != "content" {
		t.Fatalf("renamed content = %q", got)
	}
}
