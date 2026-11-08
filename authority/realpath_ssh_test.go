//go:build unix

package authority_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
)

func TestSSHRealPath(t *testing.T) {
	srv := newFakeSSH(t)
	srv.runLocally()
	a := srv.connect(t, authority.SSHOptions{})
	defer func() { _ = a.Close() }()
	rp, ok := authority.RealPathResolver(a.Files())
	if !ok {
		t.Fatal("the SSH file backend has no RealPather")
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	got, err := rp.RealPath(ctx, filepath.Join(link, "a.txt"))
	if err != nil {
		t.Fatalf("remote realpath: %v", err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got != want || !filepath.IsAbs(got) {
		t.Fatalf("remote realpath = %q, want %q", got, want)
	}
	// A missing final component is not-found on the remote too (realpath/readlink -f
	// semantics can differ, so the helper checks existence explicitly).
	if _, err := rp.RealPath(ctx, filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing remote realpath error = %v", err)
	}
	// File names may legally end in LF, CR, or be whitespace-only: the returned path must
	// keep those bytes exactly instead of trimming them as protocol newlines.
	for _, name := range []string{"a\nb", "c\rd", " ", "trailing\n\n", "pad "} {
		full := filepath.Join(dir, name)
		if err := os.WriteFile(full, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := rp.RealPath(ctx, full)
		if err != nil {
			t.Fatalf("realpath %q: %v", name, err)
		}
		want, err := filepath.EvalSymlinks(full)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("realpath %q = %q, want %q (bytes % x vs % x)", name, got, want, []byte(got), []byte(want))
		}
	}
	// A path that does not exist after resolution, including a broken symlink, is
	// not-found, never a guessed path.
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "gone"), broken); err == nil {
		if _, err := rp.RealPath(ctx, broken); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("broken-symlink realpath error = %v", err)
		}
	}
	cancelCtx, stop := context.WithCancel(context.Background())
	stop()
	if _, err := rp.RealPath(cancelCtx, dir); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled remote realpath error = %v", err)
	}
}

func TestSSHRealPathRejectsBadAnswers(t *testing.T) {
	// The helper's answer must be an absolute canonical path; a relative or empty answer
	// is a protocol-level error, never a guessed path.
	srv := newFakeSSH(t)
	srv.execFunc = func(_ string, _ io.Reader, stdout, _ io.Writer) int {
		_, _ = io.WriteString(stdout, "relative/path\n")
		return 0
	}
	a := srv.connect(t, authority.SSHOptions{})
	defer func() { _ = a.Close() }()
	rp, _ := authority.RealPathResolver(a.Files())
	if _, err := rp.RealPath(context.Background(), "/srv/proj/x"); err == nil || !strings.Contains(err.Error(), "unexpected answer") {
		t.Fatalf("relative-answer realpath = %v", err)
	}

	// An over-cap answer is a clear transport error, not a truncated path.
	srv2 := newFakeSSH(t)
	srv2.execFunc = func(_ string, _ io.Reader, stdout, _ io.Writer) int {
		_, _ = io.WriteString(stdout, "/"+strings.Repeat("x", 70<<10)+"\n")
		return 0
	}
	a2 := srv2.connect(t, authority.SSHOptions{})
	defer func() { _ = a2.Close() }()
	rp2, _ := authority.RealPathResolver(a2.Files())
	if _, err := rp2.RealPath(context.Background(), "/srv/proj/x"); err == nil || !strings.Contains(err.Error(), "exceeded") {
		t.Fatalf("oversize-answer realpath = %v", err)
	}
}
