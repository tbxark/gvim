package authority

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalRealPath(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "a.txt")
	if err := os.WriteFile(target, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(sub, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	a := Local(LocalOptions{})
	rp, ok := RealPathResolver(a.Files())
	if !ok {
		t.Fatal("the local backend has no RealPather")
	}
	got, err := rp.RealPath(context.Background(), filepath.Join(link, "a.txt"))
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("local realpath = %q, want %q (absolute)", got, want)
	}
	// Local semantics for the same odd names: bytes are preserved, never trimmed.
	for _, name := range []string{"a\nb", "c\rd", " ", "trailing\n\n"} {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := rp.RealPath(context.Background(), full)
		if err != nil {
			t.Fatalf("local realpath %q: %v", name, err)
		}
		want, err := filepath.EvalSymlinks(full)
		if err != nil || got != want {
			t.Fatalf("local realpath %q = %q, want %q (%v)", name, got, want, err)
		}
	}
	if _, err := rp.RealPath(context.Background(), filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing local realpath error = %v", err)
	}
	if _, err := rp.RealPath(context.Background(), ""); !errors.Is(err, fs.ErrInvalid) {
		t.Fatalf("empty local realpath error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := rp.RealPath(ctx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled local realpath error = %v", err)
	}
}
