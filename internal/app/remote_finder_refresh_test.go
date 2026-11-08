package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/plugin/hostos"
)

// refreshFS is one in-memory remote file system of the finder-refresh test.
type refreshFS struct {
	authority.UnsupportedFiles
	mu    sync.Mutex
	dirs  map[string][]hostos.DirEntry
	reads []string
}

func newRefreshFS(root string, names ...string) *refreshFS {
	f := &refreshFS{dirs: map[string][]hostos.DirEntry{}}
	for _, name := range names {
		f.dirs[root] = append(f.dirs[root], hostos.DirEntry{Name: name, Kind: "file"})
	}
	return f
}

func (f *refreshFS) ReadDir(_ context.Context, path string, _ bool) ([]hostos.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, path)
	entries, ok := f.dirs[path]
	if !ok {
		return nil, errors.New("no such remote directory: " + path)
	}
	return append([]hostos.DirEntry(nil), entries...), nil
}

func (f *refreshFS) readList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reads...)
}

// dynamicFilesSlot is an authority slot whose file system changes, as a reconnect replaces
// the carrier behind the same slot.
type dynamicFilesSlot struct {
	*authoritytest.Fake
	mu sync.Mutex
	fs authority.FileSystem
}

func (s *dynamicFilesSlot) Files() authority.FileSystem {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fs
}

func (s *dynamicFilesSlot) setFiles(fs authority.FileSystem) {
	s.mu.Lock()
	s.fs = fs
	s.mu.Unlock()
}

// TestRemoteFileIndexRestartUsesRecoveredFilesystem checks the remote finder refresh path:
// the same Index (the palette's Files pointer) re-enumerates through the CURRENT slot file
// system after a reconnect, dropping the dead carrier's snapshot, and no local file is
// listed.
func TestRemoteFileIndexRestartUsesRecoveredFilesystem(t *testing.T) {
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "decoy.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldFS := newRefreshFS("/remote/w", "a.go")
	newFS := newRefreshFS("/remote/w", "b.go")
	slot := &dynamicFilesSlot{Fake: authoritytest.New(), fs: oldFS}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, root := newRemoteFileIndex(ctx, slot, "/remote/w")
	if root != "/remote/w" {
		t.Fatalf("root = %q", root)
	}
	waitRemoteIndex(t, idx)
	if files, _ := idx.Snapshot(); len(files) != 1 || files[0] != "a.go" {
		t.Fatalf("first snapshot = %v", files)
	}
	// The reconnect swaps the carrier; the stale file system must not be read again.
	slot.setFiles(newFS)
	idx.Restart()
	waitRemoteIndex(t, idx)
	files, loading := idx.Snapshot()
	if loading || len(files) != 1 || files[0] != "b.go" {
		t.Fatalf("snapshot after refresh = %v (loading %v), want b.go", files, loading)
	}
	for _, r := range oldFS.readList() {
		if r != "/remote/w" {
			t.Fatalf("the stale file system was read: %q", r)
		}
	}
	if len(newFS.readList()) == 0 {
		t.Fatal("the recovered file system was not read")
	}
	for _, f := range files {
		if f == "decoy.go" {
			t.Fatal("a local file leaked into the remote index")
		}
	}
}

// waitRemoteIndex waits for the current run of idx to finish.
func waitRemoteIndex(t *testing.T, idx interface{ Done() <-chan struct{} }) {
	t.Helper()
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the remote index did not finish")
	}
}
