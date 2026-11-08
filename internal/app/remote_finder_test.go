package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteFinderFake is an in-memory authority.FileSystem for the remote finder tests: the
// map is the remote host, and a local decoy path with the same name must never appear.
type remoteFinderFake struct {
	mu    sync.Mutex
	dirs  map[string][]hostos.DirEntry
	err   error
	reads []string
}

func newRemoteFinderFake() *remoteFinderFake {
	return &remoteFinderFake{dirs: map[string][]hostos.DirEntry{}}
}
func (f *remoteFinderFake) dir(path string, names ...string) *remoteFinderFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, name := range names {
		kind := "file"
		if strings.HasSuffix(name, "/") {
			kind = "dir"
			name = strings.TrimSuffix(name, "/")
		}
		f.dirs[path] = append(f.dirs[path], hostos.DirEntry{Name: name, Kind: kind})
	}
	return f
}
func (f *remoteFinderFake) ReadDir(_ context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, path)
	if f.err != nil {
		return nil, f.err
	}
	if !hidden {
		var out []hostos.DirEntry
		for _, e := range f.dirs[path] {
			if !strings.HasPrefix(e.Name, ".") {
				out = append(out, e)
			}
		}
		return out, nil
	}
	return f.dirs[path], nil
}
func (f *remoteFinderFake) ReadFile(context.Context, string, int64) ([]byte, error) {
	return nil, errors.New("not used")
}
func (f *remoteFinderFake) WriteFile(context.Context, string, []byte, hostos.WriteOptions) error {
	return errors.New("not used")
}
func (f *remoteFinderFake) Stat(context.Context, string) (hostos.FileStat, error) {
	return hostos.FileStat{}, errors.New("not used")
}
func (f *remoteFinderFake) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, errors.New("not used")
}
func (f *remoteFinderFake) readList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.reads...)
}

// remoteOneShotFake answers one remote shell command (the ~ expansion).
type remoteOneShotFake struct {
	stdout string
	code   int
	err    error
	calls  []string
}

func (o *remoteOneShotFake) Run(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
	o.calls = append(o.calls, strings.Join(argv, " "))
	return hostos.ProcResult{Stdout: []byte(o.stdout), Code: o.code}, o.err
}

func remoteFinderSlot(fs *remoteFinderFake, one authority.OneShot) authority.Slot {
	return authority.New(authority.Parts{
		Files:   fs,
		OneShot: one,
	})
}

// TestRemoteFileIndexUsesAuthorityOnly checks the remote file mode lists the remote
// workspace through the authority file system: no local file appears even when a local
// path with the same relative name exists.
func TestRemoteFileIndexUsesAuthorityOnly(t *testing.T) {
	fs := newRemoteFinderFake().dir("/remote/w", "a.go", "src/").dir("/remote/w/src", "b.go")
	// A local decoy tree with the same names must never be read.
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "a.go"), []byte("local"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, root := newRemoteFileIndex(ctx, remoteFinderSlot(fs, &remoteOneShotFake{}), "/remote/w")
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the remote index did not finish")
	}
	files, loading := idx.Snapshot()
	sort.Strings(files)
	if loading || strings.Join(files, ",") != "a.go,src/b.go" {
		t.Fatalf("remote files = %v (loading %v), root %q", files, loading, root)
	}
	for _, read := range fs.readList() {
		if !strings.HasPrefix(read, "/remote/") {
			t.Fatalf("the finder read outside the remote workspace: %q", read)
		}
	}
	if _, err := os.Stat(filepath.Join(local, "a.go")); err != nil {
		t.Fatalf("the local decoy changed: %v", err)
	}
}

// TestRemoteFileIndexExpandsTildeRoot checks a ~ workspace root is expanded remotely
// before enumeration and that the palette root becomes the expanded path.
func TestRemoteFileIndexExpandsTildeRoot(t *testing.T) {
	fs := newRemoteFinderFake().dir("/home/u/proj", "main.go")
	one := &remoteOneShotFake{stdout: "/home/u/proj\n"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	idx, root := newRemoteFileIndex(ctx, remoteFinderSlot(fs, one), "~/proj")
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the remote index did not finish")
	}
	if root != "/home/u/proj" {
		t.Fatalf("expanded root = %q", root)
	}
	files, _ := idx.Snapshot()
	if strings.Join(files, ",") != "main.go" {
		t.Fatalf("files = %v", files)
	}
	if len(one.calls) != 1 || !strings.Contains(one.calls[0], "cd") {
		t.Fatalf("expansion commands = %v", one.calls)
	}
}

// TestRemoteFileIndexRootFailureSurfaced checks a root read failure is the index error
// (visible in the palette rows) and nothing local is used instead.
func TestRemoteFileIndexRootFailureSurfaced(t *testing.T) {
	fs := newRemoteFinderFake()
	fs.err = errors.New("connection down")
	idx, _ := newRemoteFileIndex(context.Background(), remoteFinderSlot(fs, &remoteOneShotFake{}), "/remote/w")
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the failing remote index did not finish")
	}
	if err := idx.Err(); err == nil || !strings.Contains(err.Error(), "connection down") {
		t.Fatalf("index error = %v", err)
	}
	if files, loading := idx.Snapshot(); loading || len(files) != 0 {
		t.Fatalf("failed remote index = %v, loading %v", files, loading)
	}
}

// TestRemoteProjectsRefusesLocalSwitch checks the remote launch cannot fall back to a
// local project opener (which would start a local file enumeration).
func TestRemoteProjectsRefusesLocalSwitch(t *testing.T) {
	if _, err := (remoteProjects{}).Open("/tmp/local"); err == nil {
		t.Fatal("the remote project opener allowed a local switch")
	}
}
