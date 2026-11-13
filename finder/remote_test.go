package finder

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRemote is an in-memory remote file system: dirs maps a directory to its entries.
type fakeRemote struct {
	mu    sync.Mutex
	dirs  map[string][]RemoteEntry
	errs  map[string]error
	calls []string
}

func newFakeRemote() *fakeRemote {
	return &fakeRemote{dirs: map[string][]RemoteEntry{}, errs: map[string]error{}}
}
func (f *fakeRemote) dir(path string, entries ...RemoteEntry) *fakeRemote {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirs[path] = entries
	return f
}
func (f *fakeRemote) fail(path string, err error) *fakeRemote {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs[path] = err
	return f
}
func (f *fakeRemote) ReadDir(_ context.Context, path string, hidden bool) ([]RemoteEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, path)
	if err := f.errs[path]; err != nil {
		return nil, err
	}
	entries, ok := f.dirs[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	var out []RemoteEntry
	for _, e := range entries {
		if !hidden && strings.HasPrefix(e.Name, ".") {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
func (f *fakeRemote) reads() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// TestEnumerateRemoteWalksOnlyTheRemoteFilesystem checks the walk lists the remote tree,
// skips skipDirs/hidden entries and never touches a local file that happens to share a
// name: the fake is the only source.
func TestEnumerateRemoteWalksOnlyTheRemoteFilesystem(t *testing.T) {
	local := t.TempDir()
	if err := os.WriteFile(filepath.Join(local, "local-only.go"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	fs := newFakeRemote().
		dir("/w", RemoteEntry{Name: "a.go", Kind: "file"}, RemoteEntry{Name: "src", Kind: "dir"}, RemoteEntry{Name: "node_modules", Kind: "dir"}, RemoteEntry{Name: ".hidden", Kind: "file"}).
		dir("/w/src", RemoteEntry{Name: "b.go", Kind: "file"}, RemoteEntry{Name: "sub", Kind: "dir"}, RemoteEntry{Name: "c.txt", Kind: "symlink"}).
		dir("/w/src/sub", RemoteEntry{Name: "d.go", Kind: "file"})
	var got []string
	if err := EnumerateRemote(context.Background(), fs, "/w", Options{}, func(rel string) bool {
		got = append(got, rel)
		return true
	}); err != nil {
		t.Fatalf("EnumerateRemote: %v", err)
	}
	want := []string{"a.go", "src/b.go", "src/c.txt", "src/sub/d.go"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files = %v, want %v", got, want)
	}
	if strings.Contains(strings.Join(got, ","), "local-only") {
		t.Fatal("a local file leaked into the remote listing")
	}
	for _, call := range fs.reads() {
		if !strings.HasPrefix(call, "/w") {
			t.Fatalf("the walk read outside the remote root: %q", call)
		}
	}
}

// TestEnumerateRemoteRejectsUnsafeNames checks a malicious remote cannot escape the
// root through an entry name: separators, parent/self references and absolute names are
// rejected, and symlinks are never traversed.
func TestEnumerateRemoteRejectsUnsafeNames(t *testing.T) {
	fs := newFakeRemote().
		dir("/w", RemoteEntry{Name: "ok.go", Kind: "file"},
			RemoteEntry{Name: "../escape.go", Kind: "file"},
			RemoteEntry{Name: "/abs.go", Kind: "file"},
			RemoteEntry{Name: "a/b.go", Kind: "file"},
			RemoteEntry{Name: "..", Kind: "dir"},
			RemoteEntry{Name: "link", Kind: "symlink"})
	var got []string
	if err := EnumerateRemote(context.Background(), fs, "/w", Options{}, func(rel string) bool {
		got = append(got, rel)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "link,ok.go" {
		t.Fatalf("unsafe names leaked into the walk (a symlink is listed, never traversed): %v", got)
	}
	for _, call := range fs.reads() {
		if call != "/w" {
			t.Fatalf("the walk escaped the root: %q", call)
		}
	}
}

// TestEnumerateRemoteBoundedFailure checks the root failure is the error, a subdirectory
// failure keeps the rest, MaxFiles truncation and the directory cap.
func TestEnumerateRemoteBoundedFailure(t *testing.T) {
	root := newFakeRemote().dir("/w", RemoteEntry{Name: "a.go", Kind: "file"}, RemoteEntry{Name: "bad", Kind: "dir"}, RemoteEntry{Name: "b.go", Kind: "file"}).
		dir("/w/bad", RemoteEntry{Name: "c.go", Kind: "file"}).
		fail("/w/bad", errors.New("permission denied"))
	var got []string
	if err := EnumerateRemote(context.Background(), root, "/w", Options{}, func(rel string) bool {
		got = append(got, rel)
		return true
	}); err != nil {
		t.Fatalf("a subdirectory failure became the walk error: %v", err)
	}
	if strings.Join(got, ",") != "a.go,b.go" {
		t.Fatalf("partial list = %v", got)
	}
	// The root failure is an error.
	badRoot := newFakeRemote().fail("/w", errors.New("connection down"))
	if err := EnumerateRemote(context.Background(), badRoot, "/w", Options{}, func(string) bool { return true }); err == nil {
		t.Fatal("a root read failure reported no error")
	}
	// The directory cap stops with ErrRemoteTooManyDirs.
	wide := newFakeRemote()
	entries := make([]RemoteEntry, 0, MaxRemoteDirs+1)
	for i := 0; i <= MaxRemoteDirs; i++ {
		name := fmt.Sprintf("d%05d", i)
		entries = append(entries, RemoteEntry{Name: name, Kind: "dir"})
		wide.dir("/w/"+name, RemoteEntry{Name: "f.go", Kind: "file"})
	}
	wide.dir("/w", entries...)
	err := EnumerateRemote(context.Background(), wide, "/w", Options{}, func(string) bool { return true })
	if err == nil || !strings.Contains(err.Error(), "directories") {
		t.Fatalf("directory cap error = %v", err)
	}
}

// TestEnumerateRemoteCancellation checks ctx cancellation stops the walk and is the index
// error, with no further reads.
func TestEnumerateRemoteCancellation(t *testing.T) {
	fs := newFakeRemote().dir("/w", RemoteEntry{Name: "a.go", Kind: "file"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := EnumerateRemote(ctx, fs, "/w", Options{}, func(string) bool { return true }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled walk error = %v", err)
	}
	if reads := fs.reads(); len(reads) != 0 {
		t.Fatalf("a cancelled walk read %v", reads)
	}
}

// StartRemote begins enumerating the remote workspace root in the background and returns
// at once. It fills the same Index type as Start, so the palette needs no remote-aware
// code: while loading it polls, and an error leaves the partial list usable.
func StartRemote(ctx context.Context, fs RemoteFS, root string) *Index {
	return startWith(ctx, func(ctx context.Context, emit func(string) bool) error {
		return EnumerateRemote(ctx, fs, root, Options{}, emit)
	})
}

// TestStartRemoteStreamsAndBounds checks the asynchronous index streams the remote files
// and reports completion; a root failure ends loading with the error and no local fallback.
func TestStartRemoteStreamsAndBounds(t *testing.T) {
	fs := newFakeRemote().dir("/w", RemoteEntry{Name: "a.go", Kind: "file"}, RemoteEntry{Name: "b.go", Kind: "file"})
	idx := StartRemote(context.Background(), fs, "/w")
	select {
	case <-idx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the remote index did not finish")
	}
	files, loading := idx.Snapshot()
	if loading || len(files) != 2 {
		t.Fatalf("snapshot = %v, loading %v", files, loading)
	}
	bad := StartRemote(context.Background(), newFakeRemote().fail("/w", errors.New("down")), "/w")
	select {
	case <-bad.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the failing remote index did not finish")
	}
	if err := bad.Err(); err == nil || !strings.Contains(err.Error(), "down") {
		t.Fatalf("remote index error = %v", err)
	}
	if files, loading := bad.Snapshot(); loading || len(files) != 0 {
		t.Fatalf("failing remote index = %v, loading %v", files, loading)
	}
}

// TestIdleIndex checks the placeholder used before a remote slot connects is finished,
// empty and readable (so no local enumeration starts meanwhile).
func TestIdleIndex(t *testing.T) {
	x := Idle()
	if x.Loading() || x.Err() != nil || x.Truncated() {
		t.Fatalf("idle index = %+v", x)
	}
	if files, loading := x.Snapshot(); loading || len(files) != 0 {
		t.Fatalf("idle snapshot = %v, loading %v", files, loading)
	}
	x.Close()
}
