package fsops

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// recordTrash replaces trashBackend for the rest of the test. The fake removes
// each path, as a real backend does. The returned slice holds every path the
// backend was called with.
func recordTrash(t *testing.T) *[]string {
	t.Helper()
	var calls []string
	swapTrash(t, func(paths ...string) error {
		calls = append(calls, paths...)
		for _, p := range paths {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
		return nil
	})
	return &calls
}

// requireRefused checks that Trash refused the path: an error that says the
// item was not moved and names blocked, with the backend never called.
func requireRefused(t *testing.T, err error, calls []string, blocked string) {
	t.Helper()
	if err == nil {
		t.Fatal("want refusal, got nil error")
	}
	if !strings.Contains(err.Error(), "not moved") || !strings.Contains(err.Error(), blocked) {
		t.Fatalf("error %q does not say not moved and name %s", err, blocked)
	}
	if len(calls) != 0 {
		t.Fatalf("backend called with %v; want no call", calls)
	}
}

func TestTrashRegularTreeReachesBackend(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "pkg")
	must(t, os.MkdirAll(filepath.Join(dir, "deep"), 0o755))
	writeFile(t, filepath.Join(dir, "main.go"), "package main")
	writeFile(t, filepath.Join(dir, "deep", "x.txt"), "deep")
	calls := recordTrash(t)

	if err := Trash(dir); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if !slices.Equal(*calls, []string{dir}) {
		t.Fatalf("backend got %v, want [%s]", *calls, dir)
	}
}

func TestTrashRefusesSymlinkEntries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on Windows")
	}
	cases := []struct {
		name string
		// setup returns the path to trash and the entry that blocks it.
		setup func(t *testing.T, root, target string) (path, blocked string)
	}{
		{
			name: "top-level symlink to directory",
			setup: func(t *testing.T, root, target string) (string, string) {
				link := filepath.Join(root, "link")
				must(t, os.Symlink(target, link))
				return link, link
			},
		},
		{
			name: "top-level symlink to directory with trailing slash",
			setup: func(t *testing.T, root, target string) (string, string) {
				link := filepath.Join(root, "link")
				must(t, os.Symlink(target, link))
				return link + string(filepath.Separator), link
			},
		},
		{
			name: "nested symlink to directory",
			setup: func(t *testing.T, root, target string) (string, string) {
				dir := filepath.Join(root, "dir")
				must(t, Mkdir(dir))
				link := filepath.Join(dir, "link")
				must(t, os.Symlink(target, link))
				return dir, link
			},
		},
		{
			name: "nested symlink to file",
			setup: func(t *testing.T, root, target string) (string, string) {
				dir := filepath.Join(root, "dir")
				must(t, Mkdir(dir))
				link := filepath.Join(dir, "file-link")
				must(t, os.Symlink(filepath.Join(target, "keep.txt"), link))
				return dir, link
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, "target")
			must(t, Mkdir(target))
			writeFile(t, filepath.Join(target, "keep.txt"), "keep")
			path, blocked := tc.setup(t, root, target)
			calls := recordTrash(t)

			err := Trash(path)
			requireRefused(t, err, *calls, blocked)
			if got := readFile(t, filepath.Join(target, "keep.txt")); got != "keep" {
				t.Fatalf("link target changed: %q", got)
			}
		})
	}
}

func TestTrashRefusesSpecialFileInTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("named pipes are not available on Windows")
	}
	mkfifo, err := exec.LookPath("mkfifo")
	if err != nil {
		t.Skip("mkfifo not available")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	must(t, Mkdir(dir))
	pipe := filepath.Join(dir, "pipe")
	if out, err := exec.Command(mkfifo, pipe).CombinedOutput(); err != nil {
		t.Skipf("mkfifo failed: %v: %s", err, out)
	}
	calls := recordTrash(t)

	requireRefused(t, Trash(dir), *calls, pipe)
}

func TestTrashRefusesUnreadableDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable directories, so the walk cannot fail")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "dir")
	locked := filepath.Join(dir, "locked")
	must(t, Mkdir(dir))
	must(t, Mkdir(locked))
	writeFile(t, filepath.Join(locked, "hidden.txt"), "hidden")
	must(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { makeWritable(t, root) })
	calls := recordTrash(t)

	requireRefused(t, Trash(dir), *calls, locked)
}

func TestTrashReportsOriginalStillThere(t *testing.T) {
	cases := []struct {
		name string
		// make creates the entry to trash and returns its path.
		make func(t *testing.T, root string) string
	}{
		{
			name: "file",
			make: func(t *testing.T, root string) string {
				path := filepath.Join(root, "x.txt")
				writeFile(t, path, "x")
				return path
			},
		},
		{
			name: "directory",
			make: func(t *testing.T, root string) string {
				dir := filepath.Join(root, "dir")
				must(t, Mkdir(dir))
				writeFile(t, filepath.Join(dir, "x.txt"), "x")
				return dir
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.make(t, t.TempDir())
			calls := 0
			// The backend reports success but does not remove the original,
			// as trash-go does when its removal after a copy fails.
			swapTrash(t, func(...string) error {
				calls++
				return nil
			})

			err := Trash(path)
			if err == nil {
				t.Fatal("want error when the original is still there, got nil")
			}
			if !strings.Contains(err.Error(), "still there") || !strings.Contains(err.Error(), path) {
				t.Fatalf("error %q does not say the original is still there and name %s", err, path)
			}
			if calls != 1 {
				t.Fatalf("backend called %d times, want 1", calls)
			}
			if !exists(path) {
				t.Fatal("original removed although the backend did not remove it")
			}
		})
	}
}
