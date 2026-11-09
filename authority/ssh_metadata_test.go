//go:build unix

package authority_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
)

// TestSSHSetMetadata checks the remote metadata helper: mode and mtime are applied through
// one bounded helper on the real local shell used by the fake SSH server.
func TestSSHSetMetadata(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeLocal(t, p, "x")
	a := fsSlot(t)
	meta := authority.Metadata(a.Files())
	if meta == nil {
		t.Fatal("the SSH backend has no FileMetadata")
	}
	when := time.Unix(1_700_000_000, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := meta.SetMetadata(ctx, p, 0o640, when); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o640 || !st.ModTime().Equal(when) {
		t.Fatalf("metadata = %v, %v; want 0640 and %v", st.Mode(), st.ModTime(), when)
	}
	// A zero field is skipped; a missing path fails explicitly.
	if err := meta.SetMetadata(ctx, p, 0, time.Time{}); err != nil {
		t.Fatalf("zero metadata: %v", err)
	}
	if err := meta.SetMetadata(ctx, filepath.Join(dir, "missing"), 0o600, when); err == nil {
		t.Fatal("metadata on a missing path succeeded")
	}
}
