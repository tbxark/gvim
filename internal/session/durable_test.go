package session

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// TestDurabilityHelpersRunOnTempDir checks that the sync and removal helpers
// finish without error on an ordinary directory. It cannot prove the data reached
// the disk; it checks that the calls the durability path depends on work.
func TestDurabilityHelpersRunOnTempDir(t *testing.T) {
	dir := t.TempDir()

	if err := syncDir(dir); err != nil {
		t.Fatalf("syncDir(temp dir) error = %v", err)
	}

	const name = "session.json"
	if err := writeAtomic(dir, name, []byte(`{"version":1}`)); err != nil {
		t.Fatalf("writeAtomic error = %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || string(got) != `{"version":1}` {
		t.Fatalf("ReadFile after writeAtomic = %q, %v; want the written bytes", got, err)
	}

	stash, err := StashContent(dir, "unsaved")
	if err != nil {
		t.Fatalf("StashContent error = %v", err)
	}
	if err := removeStashes(dir, []string{stash, "stash-missing.txt", "session.json"}); err != nil {
		t.Fatalf("removeStashes error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, stash)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stash %s still exists after removeStashes, stat error = %v", stash, err)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatalf("removeStashes removed a non-stash file: %v", err)
	}
}
