//go:build unix

package authority_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// TestSSHFileMutations exercises create/rename/delete over the real helper scripts,
// through the in-process SSH server that runs them locally.
func TestSSHFileMutations(t *testing.T) {
	dir := t.TempDir()
	a := fsSlot(t)
	m := authority.Mutations(a.Files())
	if m == nil {
		t.Fatal("the SSH filesystem mounts no file mutations")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a directory tree and a file inside it.
	nested := filepath.Join(dir, "a", "b")
	if err := m.Mkdir(ctx, nested, true); err != nil {
		t.Fatalf("mkdir -p: %v", err)
	}
	if st, err := os.Stat(nested); err != nil || !st.IsDir() {
		t.Fatalf("the created directory is missing: %v", err)
	}
	if err := m.Mkdir(ctx, nested, false); err == nil {
		t.Fatal("mkdir of an existing directory succeeded without parents")
	}
	src := filepath.Join(dir, "one.txt")
	if err := a.Files().WriteFile(ctx, src, []byte("hello"), hostosWrite()); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Rename over an existing target replaces it.
	dst := filepath.Join(dir, "two.txt")
	if err := a.Files().WriteFile(ctx, dst, []byte("old"), hostosWrite()); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(ctx, src, dst); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "hello" {
		t.Fatalf("renamed file = %q, %v", got, err)
	}
	if _, err := os.Stat(src); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the source survived the rename: %v", err)
	}
	if err := m.Rename(ctx, filepath.Join(dir, "missing.txt"), dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("rename of a missing file = %v, want fs.ErrNotExist", err)
	}
	// Delete a file, then a tree recursively.
	if err := m.Remove(ctx, dst, false); err != nil {
		t.Fatalf("remove file: %v", err)
	}
	if _, err := os.Stat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the file survived the remove: %v", err)
	}
	if err := m.Remove(ctx, filepath.Join(dir, "a"), true); err != nil {
		t.Fatalf("remove tree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the tree survived the recursive remove: %v", err)
	}
	if err := m.Remove(ctx, filepath.Join(dir, "missing"), false); err != nil {
		// rm -f of a missing path succeeds: removing nothing is the documented behaviour.
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("remove of a missing path = %v", err)
		}
	}
	// The helpers quote their arguments: a path with a space and a quote round-trips.
	odd := filepath.Join(dir, "a b 'c'.txt")
	if err := a.Files().WriteFile(ctx, odd, []byte("x"), hostosWrite()); err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(ctx, odd, odd+".moved"); err != nil {
		t.Fatalf("rename of a hostile name: %v", err)
	}
	if _, err := os.Stat(odd + ".moved"); err != nil {
		t.Fatalf("the hostile name did not move: %v", err)
	}
}

// TestLocalFileMutations checks the local slot forwards to the OS port, so the remote
// explorer's operations work identically on a local workspace.
func TestLocalFileMutations(t *testing.T) {
	osFake := plugintest.NewFakeOS()
	a := authority.Local(authority.LocalOptions{OS: osFake})
	m := authority.Mutations(a.Files())
	if m == nil {
		t.Fatal("the local filesystem mounts no file mutations")
	}
	ctx := context.Background()
	if err := m.Mkdir(ctx, "/work/new", true); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := m.Rename(ctx, "/work/new", "/work/renamed"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if err := m.Remove(ctx, "/work/renamed", true); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

// TestUnsupportedFileMutations checks a backend that mounts a read surface without the
// mutations answers nil, so a caller reports the unsupported error instead of panicking.
func TestUnsupportedFileMutations(t *testing.T) {
	if m := authority.Mutations(authority.UnsupportedFiles{}); m != nil {
		t.Fatalf("UnsupportedFiles mounts mutations: %v", m)
	}
	var nilFS authority.FileSystem
	if m := authority.Mutations(nilFS); m != nil {
		t.Fatalf("a nil filesystem mounts mutations: %v", m)
	}
}

// hostosWrite is the write options the remote tests use: plain truncating write.
func hostosWrite() hostosWriteOptions { return hostosWriteOptions{} }

// hostosWriteOptions is an alias for the port's options so the test reads cleanly.
type hostosWriteOptions = hostos.WriteOptions

var _ = strings.TrimSpace
var _ = time.Second

// TestSSHRenameOntoDirectoryIsRefused: mv would move the source into an existing
// directory, where a local rename fails; the helper refuses the move.
func TestSSHRenameOntoDirectoryIsRefused(t *testing.T) {
	dir := t.TempDir()
	a := fsSlot(t)
	m := authority.Mutations(a.Files())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	src := filepath.Join(dir, "one.txt")
	writeLocal(t, src, "x")
	target := filepath.Join(dir, "d")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	err := m.Rename(ctx, src, target)
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("rename onto a directory = %v, want the directory refusal", err)
	}
	if _, err := os.Stat(filepath.Join(target, "one.txt")); err == nil {
		t.Fatal("the file was moved into the directory")
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatalf("the source is gone: %v", err)
	}
}

// TestSSHHelpersTakeADashPathAsAPath: a relative path that starts with "-" is not an
// option of the helper's utilities.
func TestSSHHelpersTakeADashPathAsAPath(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	a := fsSlot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	writeLocal(t, filepath.Join(dir, "-rf"), "dash")
	got, err := a.Files().ReadFile(ctx, "-rf", 100)
	if err != nil || string(got) != "dash" {
		t.Fatalf("ReadFile(-rf) = %q, %v", got, err)
	}
	m := authority.Mutations(a.Files())
	if err := m.Rename(ctx, "-rf", "-moved"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "-moved")); err != nil {
		t.Fatalf("the file did not move: %v", err)
	}
	if err := m.Remove(ctx, "-moved", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "-moved")); err == nil {
		t.Fatal("the file survived the remove")
	}
}
