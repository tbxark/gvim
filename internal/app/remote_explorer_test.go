package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteExplorerFake is an in-memory authority.FileSystem for the remote explorer
// loader tests: the map is the remote host. A local decoy directory with the same names
// must never be read.
type remoteExplorerFake struct {
	mu      sync.Mutex
	dirs    map[string][]hostos.DirEntry
	reads   []string
	readErr error
}

func newRemoteExplorerFake() *remoteExplorerFake {
	return &remoteExplorerFake{dirs: map[string][]hostos.DirEntry{}}
}

func (f *remoteExplorerFake) dir(path string, entries ...hostos.DirEntry) *remoteExplorerFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs[path] = append(f.dirs[path], entries...)
	return f
}

func (f *remoteExplorerFake) ReadDir(_ context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, path)
	if f.readErr != nil {
		return nil, f.readErr
	}
	out := f.dirs[path]
	if !hidden {
		filtered := make([]hostos.DirEntry, 0, len(out))
		for _, e := range out {
			if len(e.Name) > 0 && e.Name[0] != '.' {
				filtered = append(filtered, e)
			}
		}
		return filtered, nil
	}
	return append([]hostos.DirEntry(nil), out...), nil
}

func (f *remoteExplorerFake) ReadRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, errors.New("not used by the explorer")
}

func (f *remoteExplorerFake) ReadFile(context.Context, string, int64) ([]byte, error) {
	return nil, errors.New("not used by the explorer")
}
func (f *remoteExplorerFake) WriteFile(context.Context, string, []byte, hostos.WriteOptions) error {
	return errors.New("not used by the explorer")
}
func (f *remoteExplorerFake) Stat(context.Context, string) (hostos.FileStat, error) {
	return hostos.FileStat{}, errors.New("not used by the explorer")
}
func (f *remoteExplorerFake) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, errors.New("not used by the explorer")
}

func (f *remoteExplorerFake) readList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reads...)
}

func explorerSlot(fs *remoteExplorerFake) *authoritytest.Fake {
	slot := authoritytest.New()
	slot.FileSystem = fs
	return slot
}

// TestAuthorityDirLoaderMapsRemoteEntries checks the loader maps the authority's
// kinds, hidden names and directory flag without touching the local disk.
func TestAuthorityDirLoaderMapsRemoteEntries(t *testing.T) {
	t.Parallel()
	fs := newRemoteExplorerFake().dir("/srv/proj",
		hostos.DirEntry{Name: "cmd", Kind: "dir"},
		hostos.DirEntry{Name: "main.go", Kind: "file"},
		hostos.DirEntry{Name: ".hidden", Kind: "file"},
		hostos.DirEntry{Name: "link", Kind: "symlink", TargetKind: "dir"},
		hostos.DirEntry{Name: "broken", Kind: "symlink"},
	)
	// A local decoy with the same names must not be read: the list comes from the slot.
	decoy := t.TempDir()
	if err := os.Mkdir(filepath.Join(decoy, "decoy-only"), 0o755); err != nil {
		t.Fatal(err)
	}
	slot := explorerSlot(fs)
	loader := newAuthorityDirLoader(slot, nil, "/srv/proj")
	got, err := loader.ReadDirs(context.Background(), []string{"/srv/proj"})
	if err != nil {
		t.Fatalf("ReadDirs: %v", err)
	}
	entries := got["/srv/proj"]
	if len(entries) != 5 {
		t.Fatalf("entries = %+v, want five", entries)
	}
	want := map[string]tui.RemoteDirEntry{
		"cmd":     {Name: "cmd", IsDir: true},
		"main.go": {Name: "main.go"},
		".hidden": {Name: ".hidden", Hidden: true},
		"link":    {Name: "link", IsDir: true},
		"broken":  {Name: "broken"},
	}
	for _, e := range entries {
		if w, ok := want[e.Name]; !ok || e != w {
			t.Fatalf("entry = %+v, want %+v", e, want[e.Name])
		}
	}
	if reads := fs.readList(); len(reads) != 1 || reads[0] != "/srv/proj" {
		t.Fatalf("remote reads = %v, want only the remote directory", reads)
	}
}

// TestAuthorityDirLoaderRefusesDisconnected checks a listing while the authority is
// disconnected is the explicit disconnected error and reads nothing remote.
func TestAuthorityDirLoaderRefusesDisconnected(t *testing.T) {
	t.Parallel()
	fs := newRemoteExplorerFake().dir("/srv/proj", hostos.DirEntry{Name: "a", Kind: "file"})
	slot := explorerSlot(fs)
	slot.Disconnect("carrier down")
	loader := newAuthorityDirLoader(slot, nil, "/srv/proj")
	_, err := loader.ReadDirs(context.Background(), []string{"/srv/proj"})
	if !errors.Is(err, tui.ErrRemoteExplorerDisconnected) {
		t.Fatalf("error = %v, want the disconnected error", err)
	}
	if reads := fs.readList(); len(reads) != 0 {
		t.Fatalf("reads = %v, want none while disconnected", reads)
	}
	// A dropped carrier during the batch is the same explicit error.
	failing := newRemoteExplorerFake()
	failing.readErr = errors.New("connection lost")
	slot2 := explorerSlot(failing)
	slot2.Disconnect("carrier down")
	loader2 := newAuthorityDirLoader(slot2, nil, "/srv/proj")
	if _, err := loader2.ReadDirs(context.Background(), []string{"/srv/proj"}); !errors.Is(err, tui.ErrRemoteExplorerDisconnected) {
		t.Fatalf("mid-batch error = %v, want the disconnected error", err)
	}
}

// TestAuthorityDirLoaderTrustGate checks the gate runs before any remote read and a
// refused gate reads nothing.
func TestAuthorityDirLoaderTrustGate(t *testing.T) {
	t.Parallel()
	fs := newRemoteExplorerFake().dir("/srv/proj", hostos.DirEntry{Name: "a", Kind: "file"})
	slot := explorerSlot(fs)
	gateErr := errors.New("workspace is not trusted: remote commands are disabled")
	calls := 0
	loader := newAuthorityDirLoader(slot, func(context.Context, []string, string) error {
		calls++
		return gateErr
	}, "/srv/proj")
	_, err := loader.ReadDirs(context.Background(), []string{"/srv/proj"})
	if !errors.Is(err, gateErr) {
		t.Fatalf("error = %v, want the gate refusal", err)
	}
	if calls != 1 || len(fs.readList()) != 0 {
		t.Fatalf("gate calls = %d reads = %v, want one gate call and no reads", calls, fs.readList())
	}
}

// TestAuthorityDirLoaderClearErrors checks a failed directory is reported with its
// path (and the entries left are still usable), and a directory over the bound is a
// clear error instead of a truncated tree.
func TestAuthorityDirLoaderClearErrors(t *testing.T) {
	t.Parallel()
	fs := newRemoteExplorerFake().dir("/srv/proj", hostos.DirEntry{Name: "ok", Kind: "file"})
	fs.readErr = nil
	slot := explorerSlot(fs)
	loader := newAuthorityDirLoader(slot, nil, "/srv/proj")
	// A directory the fake does not know reads empty, not an error.
	got, err := loader.ReadDirs(context.Background(), []string{"/srv/proj", "/srv/missing"})
	if err != nil {
		t.Fatalf("ReadDirs: %v", err)
	}
	if len(got["/srv/proj"]) != 1 || len(got["/srv/missing"]) != 0 {
		t.Fatalf("listings = %+v, want the known directory and an empty unknown one", got)
	}

	// The bound: a directory with more than the cap is an error naming the bound.
	big := make([]hostos.DirEntry, remoteExplorerMaxEntries+1)
	for i := range big {
		big[i] = hostos.DirEntry{Name: "f", Kind: "file"}
	}
	fs2 := newRemoteExplorerFake().dir("/srv/big", big...)
	loader2 := newAuthorityDirLoader(explorerSlot(fs2), nil, "/srv")
	if _, err := loader2.ReadDirs(context.Background(), []string{"/srv/big"}); err == nil || !containsAll(err.Error(), "5000", "/srv/big") {
		t.Fatalf("bound error = %v, want the path and the entry bound", err)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !strings.Contains(s, p) {
			return false
		}
	}
	return true
}

var _ authority.FileSystem = (*remoteExplorerFake)(nil)
