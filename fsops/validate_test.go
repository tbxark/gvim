package fsops

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	root := t.TempDir()
	valid := []string{"a", "a.txt", ".hidden", "..x", "a b", "a..b", "chapter-2", "héllo"}
	for _, name := range valid {
		if err := Validate(root, name); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"",
		"   ",
		".",
		"..",
		"a/b",
		"/abs",
		"../escape",
		"a/../b",
		"nul\x00byte",
	}
	for _, name := range invalid {
		if err := Validate(root, name); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Validate(%q) = %v, want ErrInvalidName", name, err)
		}
	}
}

func TestValidateBackslashOnlyOnWindows(t *testing.T) {
	root := t.TempDir()
	err := Validate(root, `a\b`)
	if filepath.Separator == '\\' {
		if !errors.Is(err, ErrInvalidName) {
			t.Fatalf("want ErrInvalidName on Windows, got %v", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("backslash is a legal character on this platform, got %v", err)
	}
}

func TestTrashCallsBackendWithPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gone.txt")
	writeFile(t, path, "x")

	var got []string
	swapTrash(t, func(paths ...string) error {
		got = append(got, paths...)
		for _, p := range paths {
			if err := os.RemoveAll(p); err != nil {
				return err
			}
		}
		return nil
	})

	if err := Trash(path); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if len(got) != 1 || got[0] != path {
		t.Fatalf("backend got %v, want [%s]", got, path)
	}
}

func TestTrashMissingPathSkipsBackend(t *testing.T) {
	root := t.TempDir()
	called := false
	swapTrash(t, func(...string) error {
		called = true
		return nil
	})

	err := Trash(filepath.Join(root, "nope"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
	if called {
		t.Fatal("backend called for a missing path")
	}
}

func TestTrashEmptyPathSkipsBackend(t *testing.T) {
	calls := recordTrash(t)

	if err := Trash(""); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("want ErrNotExist, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("backend called with %v for an empty path", *calls)
	}
}

func TestTrashWrapsBackendError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "x.txt")
	writeFile(t, path, "x")
	errBackend := errors.New("backend failed")
	swapTrash(t, func(...string) error { return errBackend })

	if err := Trash(path); !errors.Is(err, errBackend) {
		t.Fatalf("want the backend error, got %v", err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatal("file removed although backend reported failure")
	}
}

func swapTrash(t *testing.T, fn func(paths ...string) error) {
	t.Helper()
	prev := trashBackend
	trashBackend = fn
	t.Cleanup(func() { trashBackend = prev })
}
