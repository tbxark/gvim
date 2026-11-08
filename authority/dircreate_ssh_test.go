//go:build unix

package authority_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/authority"
)

func TestSSHMkdirNew(t *testing.T) {
	srv := newFakeSSH(t)
	srv.runLocally()
	a := srv.connect(t, authority.SSHOptions{})
	defer func() { _ = a.Close() }()
	creator := authority.DirectoryCreators(a.Files())
	if creator == nil {
		t.Fatal("the SSH file backend has no DirectoryCreator")
	}
	parent := t.TempDir()
	newDir := filepath.Join(parent, "new")
	if err := creator.MkdirNew(context.Background(), newDir); err != nil {
		t.Fatalf("remote MkdirNew: %v", err)
	}
	if st, err := os.Stat(newDir); err != nil || !st.IsDir() {
		t.Fatalf("remote created dir = %v, %v", st, err)
	}
	if err := creator.MkdirNew(context.Background(), newDir); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("remote second MkdirNew error = %v", err)
	}
	if err := creator.MkdirNew(context.Background(), filepath.Join(parent, "a", "b")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("remote missing-parent MkdirNew error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := creator.MkdirNew(ctx, filepath.Join(parent, "canceled")); !errors.Is(err, context.Canceled) {
		t.Fatalf("remote cancelled MkdirNew error = %v", err)
	}
}
