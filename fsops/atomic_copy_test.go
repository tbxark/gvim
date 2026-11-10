package fsops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tempNames returns the leftover files named like copy temporary files in dir.
func tempNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".fsops-copy-") {
			out = append(out, e.Name())
		}
	}
	return out
}

// TestFileCopyWriteFailureLeavesNoTargetOrTemp: the file is written to a private temp,
// so a write failure removes the temp and leaves no destination; retry succeeds.
func TestFileCopyWriteFailureLeavesNoTargetOrTemp(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "content")
	copyFileWriteHook = func() error { return errors.New("injected write failure") }
	t.Cleanup(func() { copyFileWriteHook = nil })
	if err := Copy(src, dst, Cancel); err == nil {
		t.Fatal("copy accepted an injected write failure")
	}
	if exists(dst) {
		t.Fatal("failed file copy left a destination")
	}
	if got := tempNames(t, root); len(got) != 0 {
		t.Fatalf("failed file copy left temporary files: %v", got)
	}
	copyFileWriteHook = nil
	if err := Copy(src, dst, Cancel); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if got := readFile(t, dst); got != "content" {
		t.Fatalf("retry content = %q", got)
	}
	if got := tempNames(t, root); len(got) != 0 {
		t.Fatalf("successful copy left temporary files: %v", got)
	}
}

// TestDirectoryCopyFailureRemovesPartialDestination: a child write failure removes the
// directory the copy had just created, so a retry is not blocked by a partial destination.
func TestDirectoryCopyFailureRemovesPartialDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	must(t, os.Mkdir(src, 0o755))
	writeFile(t, filepath.Join(src, "a.txt"), "a")
	dst := filepath.Join(root, "dst")
	copyFileWriteHook = func() error { return errors.New("injected child failure") }
	t.Cleanup(func() { copyFileWriteHook = nil })
	if err := Copy(src, dst, Cancel); err == nil {
		t.Fatal("copy accepted an injected child failure")
	}
	if exists(dst) {
		t.Fatal("failed directory copy left a partial destination")
	}
	if got := tempNames(t, root); len(got) != 0 {
		t.Fatalf("failed directory copy left temporary files: %v", got)
	}
	copyFileWriteHook = nil
	if err := Copy(src, dst, Cancel); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "a" {
		t.Fatalf("retry content = %q", got)
	}
}

// TestRenameNoReplaceWriteFailureLeavesNoDestination: rename uses the same atomic file
// publish; the source remains and a retry succeeds.
func TestRenameNoReplaceWriteFailureLeavesNoDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "content")
	copyFileWriteHook = func() error { return errors.New("injected write failure") }
	t.Cleanup(func() { copyFileWriteHook = nil })
	if err := RenameNoReplace(src, dst); err == nil {
		t.Fatal("rename accepted an injected write failure")
	}
	if exists(dst) {
		t.Fatal("failed rename left a destination")
	}
	if got := tempNames(t, root); len(got) != 0 {
		t.Fatalf("failed rename left temporary files: %v", got)
	}
	if got := readFile(t, src); got != "content" {
		t.Fatalf("failed rename changed the source: %q", got)
	}
	copyFileWriteHook = nil
	if err := RenameNoReplace(src, dst); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
	if exists(src) || readFile(t, dst) != "content" {
		t.Fatal("retry did not complete the rename")
	}
}

// TestFileCopyHardLinkFallbackWarns: a filesystem/file denial of hard links uses the
// recorded O_EXCL fallback and still publishes the bytes.
func TestFileCopyHardLinkFallbackWarns(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "content")
	linkCopyFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkCopyFile = os.Link })
	result, err := CopyWithResult(src, dst, Cancel)
	if err != nil {
		t.Fatalf("fallback copy: %v", err)
	}
	if !strings.Contains(result.Warning, "O_EXCL fallback") {
		t.Fatalf("fallback warning = %q", result.Warning)
	}
	if got := readFile(t, dst); got != "content" {
		t.Fatalf("fallback content = %q", got)
	}
	if got := tempNames(t, root); len(got) != 0 {
		t.Fatalf("fallback left temporary files: %v", got)
	}
}

// TestFileCopyConcurrentStillOneWinner covers the hard-link publish under concurrency.
func TestFileCopyConcurrentStillOneWinner(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "content")
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
}

// TestFileCopyCleanupFailureIsReported: a failed write whose temporary file cannot be
// removed is not silently swallowed; the error names the cleanup failure and the target
// still does not exist.
func TestFileCopyCleanupFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only mode this test relies on")
	}
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "content")
	copyFileWriteHook = func() error {
		if err := os.Chmod(root, 0o500); err != nil {
			return err
		}
		return errors.New("injected write failure")
	}
	t.Cleanup(func() {
		copyFileWriteHook = nil
		_ = os.Chmod(root, 0o700)
	})
	err := Copy(src, dst, Cancel)
	if err == nil {
		t.Fatal("copy accepted a failed write with an unremovable temp file")
	}
	if !strings.Contains(err.Error(), "remove temporary file") {
		t.Fatalf("cleanup failure is not reported: %v", err)
	}
	if exists(dst) {
		t.Fatal("failed copy left a target")
	}
	t.Cleanup(func() {
		_ = os.Chmod(root, 0o700)
		for _, name := range tempNames(t, root) {
			_ = os.Remove(filepath.Join(root, name))
		}
	})
}

// TestRenameFallbackReportsWarning: the rename/move copy-and-remove result carries the
// no-hard-link fallback warning to its caller.
func TestRenameFallbackReportsWarning(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, "content")
	linkCopyFile = func(string, string) error { return errors.New("hard links unsupported") }
	t.Cleanup(func() { linkCopyFile = os.Link })
	result, err := RenameNoReplaceResult(src, dst)
	if err != nil {
		t.Fatalf("fallback rename: %v", err)
	}
	if !strings.Contains(result.Warning, "O_EXCL fallback") {
		t.Fatalf("fallback warning = %q", result.Warning)
	}
	if exists(src) || readFile(t, dst) != "content" {
		t.Fatal("fallback rename did not complete")
	}
}

// TestCopyWithContextCancellationStopsMidFile: the context-aware copy observes
// cancellation between file chunks, removes the temp/destination, and retries cleanly.
func TestCopyWithContextCancellationStopsMidFile(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "a.txt")
	dst := filepath.Join(root, "b.txt")
	writeFile(t, src, strings.Repeat("x", 64<<10))
	copyReadHook = func() error { return context.Canceled }
	t.Cleanup(func() { copyReadHook = nil })
	_, err := CopyWithResultContext(context.Background(), src, dst, Cancel)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled copy error = %v, want context.Canceled", err)
	}
	if exists(dst) {
		t.Fatal("cancelled copy left a destination")
	}
	if got := tempNames(t, root); len(got) != 0 {
		t.Fatalf("cancelled copy left temporary files: %v", got)
	}
	copyReadHook = nil
	if _, err := CopyWithResultContext(context.Background(), src, dst, Cancel); err != nil {
		t.Fatalf("retry after cancellation: %v", err)
	}
}

// TestDirectoryCopyCleanupFailureIsReported: a failed child write whose partial
// destination and temporary file cannot be removed reports both cleanup failures instead
// of swallowing them.
func TestDirectoryCopyCleanupFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only mode this test relies on")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	must(t, os.Mkdir(src, 0o755))
	writeFile(t, filepath.Join(src, "a.txt"), "a")
	dst := filepath.Join(root, "dst")
	copyFileWriteHook = func() error {
		// Make both the temporary-file removal and the partial-directory removal fail.
		if err := os.Chmod(dst, 0o500); err != nil {
			return err
		}
		return errors.New("injected child failure")
	}
	t.Cleanup(func() {
		copyFileWriteHook = nil
		_ = os.Chmod(root, 0o700)
		_ = os.Chmod(dst, 0o700)
		_ = os.RemoveAll(dst)
	})
	err := Copy(src, dst, Cancel)
	if err == nil {
		t.Fatal("copy accepted a failed child write")
	}
	if !strings.Contains(err.Error(), "remove temporary file") || !strings.Contains(err.Error(), "remove partial destination") {
		t.Fatalf("cleanup failures are not reported: %v", err)
	}
	if !exists(dst) {
		t.Fatal("the read-only destination should still exist until cleanup is retried")
	}
	if got := readFile(t, filepath.Join(src, "a.txt")); got != "a" {
		t.Fatalf("source changed: %q", got)
	}
}

// TestRenameSourceRemovalFailureIsReported: the copy succeeds but deleting the source
// fails; both copies stay and the error names the source-removal failure.
func TestRenameSourceRemovalFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores the read-only mode this test relies on")
	}
	root := t.TempDir()
	src := filepath.Join(root, "src")
	must(t, os.Mkdir(src, 0o755))
	writeFile(t, filepath.Join(src, "a.txt"), "a")
	must(t, os.Chmod(src, 0o500))
	t.Cleanup(func() { _ = os.Chmod(src, 0o700) })
	t.Cleanup(func() { makeWritable(t, root) })
	dst := filepath.Join(root, "dst")
	err := RenameNoReplace(src, dst)
	if err == nil || !strings.Contains(err.Error(), "could not remove the source") {
		t.Fatalf("source-removal failure = %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "a.txt")); got != "a" {
		t.Fatalf("completed copy changed: %q", got)
	}
	if !exists(filepath.Join(src, "a.txt")) {
		t.Fatal("failed source removal removed the source")
	}
	if got := tempNames(t, root); len(got) != 0 {
		t.Fatalf("rename left temporary files: %v", got)
	}
}
