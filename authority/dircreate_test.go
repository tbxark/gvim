package authority

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalMkdirNew(t *testing.T) {
	a := Local(LocalOptions{})
	creator := DirectoryCreators(a.Files())
	if creator == nil {
		t.Fatal("the local backend has no DirectoryCreator")
	}
	dir := t.TempDir()
	newDir := filepath.Join(dir, "new")
	if err := creator.MkdirNew(context.Background(), newDir); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(newDir); err != nil || !st.IsDir() {
		t.Fatalf("created dir = %v, %v", st, err)
	}
	if err := creator.MkdirNew(context.Background(), newDir); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second MkdirNew error = %v", err)
	}
	if err := creator.MkdirNew(context.Background(), filepath.Join(dir, "a", "b")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing-parent MkdirNew error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := creator.MkdirNew(ctx, filepath.Join(dir, "canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled MkdirNew error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "canceled")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a cancelled MkdirNew created something: %v", err)
	}
}
