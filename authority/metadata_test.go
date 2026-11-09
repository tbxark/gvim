package authority_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// TestLocalSetMetadata checks the local FileMetadata surface applies permission bits and
// mtime and leaves a zero field alone; the read-only fallback has no metadata surface.
func TestLocalSetMetadata(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := authority.Local(authority.LocalOptions{})
	meta := authority.Metadata(a.Files())
	if meta == nil {
		t.Fatal("the local backend has no FileMetadata")
	}
	when := time.Unix(1_700_000_000, 0)
	if err := meta.SetMetadata(context.Background(), p, 0o600, when); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 || !st.ModTime().Equal(when) {
		t.Fatalf("metadata = %v, %v; want 0600 and %v", st.Mode(), st.ModTime(), when)
	}
	// A zero mode/mtime is not applied: the file keeps what it has.
	if err := meta.SetMetadata(context.Background(), p, 0, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if st2, err := os.Stat(p); err != nil || st2.Mode().Perm() != 0o600 || !st2.ModTime().Equal(when) {
		t.Fatalf("zero metadata changed the file: %+v, %v", st2, err)
	}
	if authority.Metadata(authority.UnsupportedFiles{}) != nil {
		t.Fatal("the read-only fallback claimed a metadata surface")
	}
	var _ hostos.FileStat
}
