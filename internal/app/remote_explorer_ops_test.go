package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteOpsFake is an in-memory authority.FileSystem + FileMutations for the remote
// explorer write tests. A real local decoy tree must never be touched.
type remoteOpsFake struct {
	mu      sync.Mutex
	files   map[string][]byte
	dirs    map[string]bool
	mtime   map[string]int64
	reads   []string
	writes  []string
	mkdirs  []string
	renames [][2]string
	removes []string
	// makeErr makes the next mutation of that kind fail.
	writeErr, mkdirErr, renameErr, removeErr error
	statErr                                  error
}

func newRemoteOpsFake() *remoteOpsFake {
	return &remoteOpsFake{files: map[string][]byte{}, dirs: map[string]bool{}, mtime: map[string]int64{}}
}

func (f *remoteOpsFake) put(path, text string) *remoteOpsFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.HasSuffix(path, "/") {
		f.dirs[strings.TrimSuffix(path, "/")] = true
		return f
	}
	f.files[path] = []byte(text)
	f.mtime[path] = 1
	return f
}

func (f *remoteOpsFake) ReadFile(ctx context.Context, p string, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, p)
	data, ok := f.files[p]
	if !ok {
		return nil, &os.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
	}
	return append([]byte(nil), data...), nil
}

func (f *remoteOpsFake) ReadRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, errors.New("not used by the explorer ops")
}

func (f *remoteOpsFake) WriteFile(ctx context.Context, p string, data []byte, o hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.writeErr != nil {
		return f.writeErr
	}
	f.writes = append(f.writes, p)
	f.files[p] = append([]byte(nil), data...)
	return nil
}

func (f *remoteOpsFake) Stat(_ context.Context, p string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statErr != nil {
		return hostos.FileStat{}, f.statErr
	}
	if f.dirs[p] {
		return hostos.FileStat{Kind: "dir"}, nil
	}
	if data, ok := f.files[p]; ok {
		return hostos.FileStat{Kind: "file", Size: int64(len(data)), MTime: timeUnix(f.mtime[p])}, nil
	}
	return hostos.FileStat{}, &os.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

func (f *remoteOpsFake) ReadDir(context.Context, string, bool) ([]hostos.DirEntry, error) {
	return nil, errors.New("not used by the explorer ops")
}

func (f *remoteOpsFake) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, errors.New("not used by the explorer ops")
}

func (f *remoteOpsFake) Mkdir(_ context.Context, p string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	if f.dirs[p] {
		return &os.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	f.mkdirs = append(f.mkdirs, p)
	f.dirs[p] = true
	return nil
}

func (f *remoteOpsFake) Rename(_ context.Context, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.renameErr != nil {
		return f.renameErr
	}
	if data, ok := f.files[from]; ok {
		delete(f.files, from)
		f.files[to] = data
		f.renames = append(f.renames, [2]string{from, to})
		return nil
	}
	if f.dirs[from] {
		delete(f.dirs, from)
		f.dirs[to] = true
		f.renames = append(f.renames, [2]string{from, to})
		return nil
	}
	return &os.PathError{Op: "rename", Path: from, Err: fs.ErrNotExist}
}

func (f *remoteOpsFake) Remove(_ context.Context, p string, recursive bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	if recursive {
		for path := range f.files {
			if path == p || strings.HasPrefix(path, p+"/") {
				delete(f.files, path)
			}
		}
		for path := range f.dirs {
			if path == p || strings.HasPrefix(path, p+"/") {
				delete(f.dirs, path)
			}
		}
	}
	delete(f.files, p)
	delete(f.dirs, p)
	f.removes = append(f.removes, p)
	return nil
}

func (f *remoteOpsFake) ops(slotConnected bool) (*remoteExplorerOps, *authoritytest.Fake) {
	slot := authoritytest.New()
	slot.FileSystem = f
	if !slotConnected {
		slot.Disconnect("carrier down")
	}
	return newRemoteExplorerOps(slot, "/remote/w", "SSH:u@h", nil, nil), slot
}

func timeUnix(n int64) (t time.Time) { return time.Unix(n, 0) }

// TestRemoteExplorerCreateFile checks an empty remote file is created through the
// authority, a local decoy stays untouched, and an existing entry is refused.
func TestRemoteExplorerCreateFile(t *testing.T) {
	decoy := filepath.Join(t.TempDir(), "w")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(decoy)
	if err != nil {
		t.Fatal(err)
	}
	fake := newRemoteOpsFake()
	ops, _ := fake.ops(true)
	p, err := ops.CreateFile(context.Background(), "/remote/w", "a.go")
	if err != nil {
		t.Fatalf("CreateFile: %v", err)
	}
	if p != "/remote/w/a.go" || string(fake.files["/remote/w/a.go"]) != "" {
		t.Fatalf("created = %q, remote file %q", p, fake.files["/remote/w/a.go"])
	}
	if _, err := ops.CreateFile(context.Background(), "/remote/w", "a.go"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("existing create error = %v, want already exists", err)
	}
	after, err := os.ReadDir(decoy)
	if err != nil || len(after) != len(before) {
		t.Fatalf("local decoy changed: %v, %v", after, err)
	}
	if _, err := ops.CreateFile(context.Background(), "/remote/w/../x", "b"); err == nil {
		t.Fatal("a name escaping the remote workspace was accepted")
	}
}

// TestRemoteExplorerMkdirAndRename checks directory creation and rename go through the
// mutations surface, and a rename onto an existing entry is refused.
func TestRemoteExplorerMkdirAndRename(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/a.go", "a").put("/remote/w/b.go", "b")
	ops, _ := fake.ops(true)
	p, err := ops.Mkdir(context.Background(), "/remote/w", "sub")
	if err != nil || p != "/remote/w/sub" || !fake.dirs[p] {
		t.Fatalf("Mkdir = %q, %v dirs=%v", p, err, fake.dirs)
	}
	np, err := ops.Rename(context.Background(), "/remote/w/a.go", "c.go")
	if err != nil || np != "/remote/w/c.go" || len(fake.renames) != 1 {
		t.Fatalf("Rename = %q, %v renames=%v", np, err, fake.renames)
	}
	if got := string(fake.files["/remote/w/c.go"]); got != "a" {
		t.Fatalf("renamed content = %q", got)
	}
	if _, err := ops.Rename(context.Background(), "/remote/w/c.go", "b.go"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("rename onto existing error = %v", err)
	}
	if same, err := ops.Rename(context.Background(), "/remote/w/c.go", "c.go"); err != nil || same != "/remote/w/c.go" {
		t.Fatalf("rename to same name = %q, %v", same, err)
	}
}

// TestRemoteExplorerDeleteIsPermanent checks a file delete is a non-recursive Remove and a
// directory delete is recursive (there is no remote trash).
func TestRemoteExplorerDeleteIsPermanent(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/a.go", "a").put("/remote/w/sub/", "")
	fake.put("/remote/w/sub/x.go", "x")
	ops, _ := fake.ops(true)
	if err := ops.Trash(context.Background(), "/remote/w/a.go"); err != nil {
		t.Fatalf("file delete: %v", err)
	}
	if _, ok := fake.files["/remote/w/a.go"]; ok {
		t.Fatal("the remote file survived the delete")
	}
	if err := ops.Trash(context.Background(), "/remote/w/sub"); err != nil {
		t.Fatalf("dir delete: %v", err)
	}
	if _, ok := fake.files["/remote/w/sub/x.go"]; ok || fake.dirs["/remote/w/sub"] {
		t.Fatal("the remote directory tree survived the recursive delete")
	}
	if len(fake.removes) != 2 {
		t.Fatalf("removes = %v, want two", fake.removes)
	}
}

// TestRemoteExplorerOpsRefuseWhenDisconnected checks every operation returns the explicit
// disconnected error and touches nothing.
func TestRemoteExplorerOpsRefuseWhenDisconnected(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/a.go", "a")
	ops, _ := fake.ops(false)
	ctx := context.Background()
	if _, err := ops.CreateFile(ctx, "/remote/w", "b"); !errors.Is(err, errRemoteExplorerDisconnected) {
		t.Fatalf("create error = %v", err)
	}
	if _, err := ops.Mkdir(ctx, "/remote/w", "d"); !errors.Is(err, errRemoteExplorerDisconnected) {
		t.Fatalf("mkdir error = %v", err)
	}
	if _, err := ops.Rename(ctx, "/remote/w/a.go", "b.go"); !errors.Is(err, errRemoteExplorerDisconnected) {
		t.Fatalf("rename error = %v", err)
	}
	if err := ops.Trash(ctx, "/remote/w/a.go"); !errors.Is(err, errRemoteExplorerDisconnected) {
		t.Fatalf("delete error = %v", err)
	}
	if len(fake.writes) != 0 || len(fake.mkdirs) != 0 || len(fake.renames) != 0 || len(fake.removes) != 0 {
		t.Fatalf("a disconnected operation touched the remote: %+v", fake)
	}
}

// TestRemoteExplorerOpsTrustGate checks the workspace trust gate runs before any mutation.
func TestRemoteExplorerOpsTrustGate(t *testing.T) {
	fake := newRemoteOpsFake()
	slot := authoritytest.New()
	slot.FileSystem = fake
	gateErr := errors.New("workspace is not trusted: remote commands are disabled")
	calls := 0
	ops := newRemoteExplorerOps(slot, "/remote/w", "SSH:u@h", func(context.Context, []string, string) error {
		calls++
		return gateErr
	}, nil)
	if _, err := ops.CreateFile(context.Background(), "/remote/w", "a.go"); !errors.Is(err, gateErr) {
		t.Fatalf("create gate error = %v", err)
	}
	if calls != 1 || len(fake.writes) != 0 {
		t.Fatalf("gate calls = %d writes = %v", calls, fake.writes)
	}
	if _, err := ops.Mkdir(context.Background(), "/remote/w", "d"); !errors.Is(err, gateErr) {
		t.Fatalf("mkdir gate error = %v", err)
	}
	if err := ops.Trash(context.Background(), "/remote/w/d"); !errors.Is(err, gateErr) {
		t.Fatalf("delete gate error = %v", err)
	}
}

// TestRemoteExplorerOpsRefuseClipboardAndOutside checks move/copy are explicitly
// unsupported and paths outside the remote workspace are refused.
func TestRemoteExplorerOpsRefuseClipboardAndOutside(t *testing.T) {
	fake := newRemoteOpsFake()
	ops, _ := fake.ops(true)
	if _, err := ops.Move(context.Background(), "/remote/w/a", "/remote/w/b"); err == nil {
		t.Fatal("remote Move was accepted")
	}
	if _, err := ops.Copy(context.Background(), "/remote/w/a", "/remote/w/b", 0); err == nil {
		t.Fatal("remote Copy was accepted")
	}
	if _, err := ops.CreateFile(context.Background(), "/other", "a"); err == nil {
		t.Fatal("a create outside the remote workspace was accepted")
	}
	if _, err := ops.Rename(context.Background(), "/other/a", "b"); err == nil {
		t.Fatal("a rename outside the remote workspace was accepted")
	}
	if err := ops.Trash(context.Background(), "/other/a"); err == nil {
		t.Fatal("a delete outside the remote workspace was accepted")
	}
	if got := ops.RemoteTarget(); got != "SSH:u@h:/remote/w" {
		t.Fatalf("RemoteTarget = %q", got)
	}
}
