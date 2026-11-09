package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteOpsFake is an in-memory authority.FileSystem + FileMutations for the remote
// explorer write tests. A real local decoy tree must never be touched.
type remoteOpsFake struct {
	mu        sync.Mutex
	files     map[string][]byte
	dirs      map[string]bool
	mtime     map[string]int64
	reads     []string
	writes    []string
	writeOpts []hostos.WriteOptions
	mkdirs    []string
	mkdirNews []string
	dirReads  []string
	renames   [][2]string
	removes   []string
	symlinks  map[string]string
	// Exclusive create (authority.FileCreator): creates records successful creates,
	// createModes their options, and the flags force the backend failures.
	creates           []string
	createModes       []hostos.WriteOptions
	createErr         error
	createUnsupported bool
	// onWrite runs after a successful write, outside the lock (a test can cancel a
	// context to exercise mid-copy cancellation).
	onWrite func()
	// makeErr makes the next mutation of that kind fail.
	writeErr, mkdirErr, renameErr, removeErr error
	statErr                                  error
	// FileMetadata bookkeeping: modes and the mtime map are updated by SetMetadata, and
	// metaErr makes it fail for the best-effort warning test.
	modes       map[string]fs.FileMode
	metaEntries []string
	metaErr     error
}

func newRemoteOpsFake() *remoteOpsFake {
	return &remoteOpsFake{files: map[string][]byte{}, dirs: map[string]bool{}, mtime: map[string]int64{}, modes: map[string]fs.FileMode{}, symlinks: map[string]string{}}
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

// putMode stores a file or directory with a mode and mtime for the metadata tests.
func (f *remoteOpsFake) putMode(p, text string, mode fs.FileMode, mtime time.Time) *remoteOpsFake {
	f.put(p, text)
	key := strings.TrimSuffix(p, "/")
	f.mu.Lock()
	f.modes[key] = mode
	f.mtime[key] = mtime.Unix()
	f.mu.Unlock()
	return f
}

// SetMetadata implements authority.FileMetadata for the copy tests.
func (f *remoteOpsFake) SetMetadata(_ context.Context, p string, mode fs.FileMode, mtime time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.metaErr != nil {
		return f.metaErr
	}
	if mode != 0 {
		f.modes[p] = mode.Perm()
	}
	if !mtime.IsZero() {
		f.mtime[p] = mtime.Unix()
	}
	f.metaEntries = append(f.metaEntries, p)
	return nil
}

func (f *remoteOpsFake) putSymlink(p, target string) *remoteOpsFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.symlinks[p] = target
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
	f.writeOpts = append(f.writeOpts, o)
	f.files[p] = append([]byte(nil), data...)
	hook := f.onWrite
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	f.mu.Lock()
	return nil
}

// CreateNew implements authority.FileCreator: an O_EXCL-like in-memory create. The mutex
// is the atomicity point a concurrent-creator test relies on.
func (f *remoteOpsFake) CreateNew(ctx context.Context, p string, o hostos.WriteOptions) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createUnsupported {
		return authority.ErrUnsupportedFileIO
	}
	if f.createErr != nil {
		return f.createErr
	}
	if _, ok := f.files[p]; ok {
		return &os.PathError{Op: "create", Path: p, Err: fs.ErrExist}
	}
	if _, ok := f.symlinks[p]; ok {
		return &os.PathError{Op: "create", Path: p, Err: fs.ErrExist}
	}
	if f.dirs[p] {
		return &os.PathError{Op: "create", Path: p, Err: fs.ErrExist}
	}
	f.files[p] = []byte{}
	f.mtime[p] = 1
	f.creates = append(f.creates, p)
	f.createModes = append(f.createModes, o)
	return nil
}

func (f *remoteOpsFake) Stat(_ context.Context, p string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statErr != nil {
		return hostos.FileStat{}, f.statErr
	}
	if _, ok := f.symlinks[p]; ok {
		if data, ok := f.files[f.symlinks[p]]; ok {
			return hostos.FileStat{Kind: "file", Size: int64(len(data))}, nil
		}
		if f.dirs[f.symlinks[p]] {
			return hostos.FileStat{Kind: "dir"}, nil
		}
		return hostos.FileStat{Kind: "symlink"}, nil
	}
	if f.dirs[p] {
		return hostos.FileStat{Kind: "dir", MTime: timeUnix(f.mtime[p]), Mode: f.modes[p]}, nil
	}
	if data, ok := f.files[p]; ok {
		return hostos.FileStat{Kind: "file", Size: int64(len(data)), MTime: timeUnix(f.mtime[p]), Mode: f.modes[p]}, nil
	}
	return hostos.FileStat{}, &os.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

func (f *remoteOpsFake) ReadDir(_ context.Context, p string, _ bool) ([]hostos.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dirReads = append(f.dirReads, p)
	prefix := strings.TrimSuffix(p, "/") + "/"
	seen := map[string]hostos.DirEntry{}
	add := func(full, kind string, size int64) {
		name := strings.TrimPrefix(full, prefix)
		if name == full || strings.Contains(name, "/") || name == "" {
			return
		}
		seen[name] = hostos.DirEntry{Name: name, Kind: kind, Size: size}
	}
	for full, data := range f.files {
		add(full, "file", int64(len(data)))
	}
	for full := range f.dirs {
		add(full, "dir", 0)
	}
	for full := range f.symlinks {
		add(full, "symlink", 0)
	}
	if len(seen) == 0 {
		if !f.dirs[p] {
			return nil, &os.PathError{Op: "readdir", Path: p, Err: fs.ErrNotExist}
		}
		return nil, nil
	}
	out := make([]hostos.DirEntry, 0, len(seen))
	for _, e := range seen {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
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

// MkdirNew is the atomic DirectoryCreator surface: one no-replace create, a missing
// parent is fs.ErrNotExist, existing files/dirs/symlinks are fs.ErrExist.
func (f *remoteOpsFake) MkdirNew(ctx context.Context, p string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if f.mkdirErr != nil {
		return f.mkdirErr
	}
	if f.dirs[p] {
		return &os.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	if _, ok := f.files[p]; ok {
		return &os.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	if _, ok := f.symlinks[p]; ok {
		return &os.PathError{Op: "mkdir", Path: p, Err: fs.ErrExist}
	}
	parent := path.Dir(p)
	if parent != "/remote/w" && !f.dirs[parent] {
		return &os.PathError{Op: "mkdir", Path: p, Err: fs.ErrNotExist}
	}
	f.mkdirNews = append(f.mkdirNews, p)
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
	if len(fake.creates) != 1 || fake.creates[0] != "/remote/w/a.go" {
		t.Fatalf("exclusive creates = %v, want one create-new call", fake.creates)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("CreateFile fell back to a whole-file write: %v", fake.writes)
	}
	_, err = ops.CreateFile(context.Background(), "/remote/w", "a.go")
	if err == nil || !strings.Contains(err.Error(), "already exists") || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("existing create error = %v, want already exists and fs.ErrExist", err)
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

// TestRemoteExplorerOpsRefuseOutside checks paths outside the remote workspace are
// refused by every operation (the clipboard transfer itself is covered by the copy
// tests).
func TestRemoteExplorerOpsRefuseOutside(t *testing.T) {
	fake := newRemoteOpsFake()
	ops, _ := fake.ops(true)
	if _, err := ops.Move(context.Background(), "/other/a", "/other/b"); err == nil {
		t.Fatal("a move outside the remote workspace was accepted")
	}
	if _, err := ops.Copy(context.Background(), "/other/a", "/other/b", 0); err == nil {
		t.Fatal("a copy outside the remote workspace was accepted")
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

// TestRemoteExplorerCreateFileConcurrentExactlyOne checks the exclusive-create path under
// concurrent creators: exactly one call wins, the rest get the clear already-exists
// refusal, the target stays the empty file the winner created, and no whole-file write
// (which would replace a target) happens.
func TestRemoteExplorerCreateFileConcurrentExactlyOne(t *testing.T) {
	fake := newRemoteOpsFake()
	ops, _ := fake.ops(true)
	const n = 32
	var wg sync.WaitGroup
	results := make(chan error, n)
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := ops.CreateFile(context.Background(), "/remote/w", "race.go")
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins, exists := 0, 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, fs.ErrExist) && strings.Contains(err.Error(), "already exists"):
			exists++
		default:
			t.Fatalf("unexpected concurrent create error: %v", err)
		}
	}
	if wins != 1 || exists != n-1 {
		t.Fatalf("wins = %d, already-exists = %d; want 1 and %d", wins, exists, n-1)
	}
	if got := string(fake.files["/remote/w/race.go"]); got != "" {
		t.Fatalf("raced target content = %q, want the empty created file", got)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("concurrent creates used whole-file writes: %v", fake.writes)
	}
}

// TestRemoteExplorerCreateFileKeepsDanglingSymlink checks a dangling symbolic link is an
// existing entry to the exclusive create (never followed, never replaced or truncated).
func TestRemoteExplorerCreateFileKeepsDanglingSymlink(t *testing.T) {
	fake := newRemoteOpsFake()
	fake.putSymlink("/remote/w/dangling", "/remote/w/missing")
	ops, _ := fake.ops(true)
	if _, err := ops.CreateFile(context.Background(), "/remote/w", "dangling"); err == nil || !errors.Is(err, fs.ErrExist) {
		t.Fatalf("create over a dangling symlink = %v, want fs.ErrExist", err)
	}
	if fake.symlinks["/remote/w/dangling"] != "/remote/w/missing" {
		t.Fatal("the dangling symlink was changed by the refused create")
	}
	if _, ok := fake.files["/remote/w/dangling"]; ok {
		t.Fatal("a regular file replaced the dangling symlink")
	}
	if len(fake.writes) != 0 {
		t.Fatalf("the refused create wrote: %v", fake.writes)
	}
}

// remoteOpsFilesNoCreate is the fake authority filesystem without the FileCreator surface,
// for the explicit-refusal branch when promise.Creators is nil.
type remoteOpsFilesNoCreate struct{ files *remoteOpsFake }

func (f remoteOpsFilesNoCreate) ReadFile(ctx context.Context, p string, max int64) ([]byte, error) {
	return f.files.ReadFile(ctx, p, max)
}
func (f remoteOpsFilesNoCreate) ReadRange(ctx context.Context, p string, off, n int64) ([]byte, error) {
	return f.files.ReadRange(ctx, p, off, n)
}
func (f remoteOpsFilesNoCreate) WriteFile(ctx context.Context, p string, data []byte, o hostos.WriteOptions) error {
	return f.files.WriteFile(ctx, p, data, o)
}
func (f remoteOpsFilesNoCreate) Stat(ctx context.Context, p string) (hostos.FileStat, error) {
	return f.files.Stat(ctx, p)
}
func (f remoteOpsFilesNoCreate) ReadDir(ctx context.Context, p string, hidden bool) ([]hostos.DirEntry, error) {
	return f.files.ReadDir(ctx, p, hidden)
}
func (f remoteOpsFilesNoCreate) Watch(ctx context.Context, p string, o hostos.WatchOptions) (hostos.Watch, error) {
	return f.files.Watch(ctx, p, o)
}

// TestRemoteExplorerCreateFileRefusesWhenNoCreator checks a filesystem without the
// FileCreator interface at all is refused explicitly, with an existing target preserved and
// no Stat + WriteFile fallback.
func TestRemoteExplorerCreateFileRefusesWhenNoCreator(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/a.go", "precious")
	slot := authoritytest.New()
	slot.FileSystem = remoteOpsFilesNoCreate{files: fake}
	ops := newRemoteExplorerOps(slot, "/remote/w", "SSH:u@h", nil, nil)
	_, err := ops.CreateFile(context.Background(), "/remote/w", "a.go")
	if err == nil || !strings.Contains(err.Error(), "cannot create a file exclusively") {
		t.Fatalf("create without a creator = %v, want the explicit exclusive-create refusal", err)
	}
	if got := string(fake.files["/remote/w/a.go"]); got != "precious" {
		t.Fatalf("existing target changed to %q", got)
	}
	if len(fake.writes) != 0 {
		t.Fatalf("a fallback whole-file write ran: %v", fake.writes)
	}
}

// TestRemoteExplorerCreateFileRefusesWithoutExclusiveCreate checks a backend that cannot
// guarantee an exclusive create is refused explicitly, with any existing target preserved
// and no check-then-write fallback.
func TestRemoteExplorerCreateFileRefusesWithoutExclusiveCreate(t *testing.T) {
	for name, fake := range map[string]*remoteOpsFake{
		"unsupported backend": func() *remoteOpsFake {
			f := newRemoteOpsFake().put("/remote/w/a.go", "precious")
			f.createUnsupported = true
			return f
		}(),
		"helper failure": func() *remoteOpsFake {
			f := newRemoteOpsFake().put("/remote/w/a.go", "precious")
			f.createErr = errors.New("carrier dropped")
			return f
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			ops, _ := fake.ops(true)
			_, err := ops.CreateFile(context.Background(), "/remote/w", "a.go")
			if err == nil {
				t.Fatal("the create succeeded on a backend that cannot guarantee exclusivity")
			}
			if strings.Contains(name, "unsupported") && !strings.Contains(err.Error(), "cannot create a file exclusively") {
				t.Fatalf("error = %v, want the explicit exclusive-create refusal", err)
			}
			if got := string(fake.files["/remote/w/a.go"]); got != "precious" {
				t.Fatalf("existing target changed to %q", got)
			}
			if len(fake.writes) != 0 {
				t.Fatalf("a fallback whole-file write ran: %v", fake.writes)
			}
		})
	}
}

// noDirectoryCreatorFS hides the atomic DirectoryCreator surface while still forwarding
// every read/mutation the explorer ops need.
type noDirectoryCreatorFS struct {
	authority.FileSystem
	authority.FileMutations
}

// TestRemoteExplorerMkdirIsAtomic checks New Directory uses the no-replace creator:
// existing entries fail with fs.ErrExist without a second mutation, a missing parent
// reports fs.ErrNotExist, cancellation is surfaced, and no Stat-then-create path runs.
func TestRemoteExplorerMkdirIsAtomic(t *testing.T) {
	fake := newRemoteOpsFake().put("/remote/w/existing/", "").put("/remote/w/a.go", "a")
	fake.statErr = errors.New("Stat must not be used by atomic New Directory")
	ops, _ := fake.ops(true)

	if _, err := ops.Mkdir(context.Background(), "/remote/w", "existing"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("existing directory error = %v, want fs.ErrExist", err)
	}
	if len(fake.mkdirNews) != 0 {
		t.Fatalf("existing directory reached MkdirNew: %v", fake.mkdirNews)
	}
	if _, err := ops.Mkdir(context.Background(), "/remote/w", "sub"); err != nil {
		t.Fatalf("new directory: %v", err)
	}
	if len(fake.mkdirNews) != 1 || !fake.dirs["/remote/w/sub"] {
		t.Fatalf("atomic creates = %v dirs=%v", fake.mkdirNews, fake.dirs)
	}
	if _, err := ops.Mkdir(context.Background(), "/remote/w", "sub"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("second New Directory error = %v, want fs.ErrExist", err)
	}
	if _, err := ops.Mkdir(context.Background(), "/remote/w/a.go", "child"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("parent-is-a-file error = %v, want fs.ErrNotExist", err)
	}

	canceled := newRemoteOpsFake()
	canceled.mkdirErr = context.Canceled
	opsCanceled, _ := canceled.ops(true)
	if _, err := opsCanceled.Mkdir(context.Background(), "/remote/w", "d"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled New Directory error = %v, want context.Canceled", err)
	}
	if len(canceled.dirs) != 0 {
		t.Fatalf("a canceled create left a directory: %v", canceled.dirs)
	}
}

// TestRemoteExplorerMkdirWithoutCapabilityRefuses: no check-then-create fallback. A
// backend without DirectoryCreator reports the inability and touches nothing.
func TestRemoteExplorerMkdirWithoutCapabilityRefuses(t *testing.T) {
	base := newRemoteOpsFake()
	slot := authoritytest.New()
	slot.FileSystem = &noDirectoryCreatorFS{FileSystem: base, FileMutations: base}
	ops := newRemoteExplorerOps(slot, "/remote/w", "SSH:u@h", nil, nil)
	if _, err := ops.Mkdir(context.Background(), "/remote/w", "d"); err == nil || !strings.Contains(err.Error(), "cannot create a directory exclusively") {
		t.Fatalf("no-capability error = %v", err)
	}
	if len(base.mkdirs) != 0 || len(base.mkdirNews) != 0 {
		t.Fatalf("no-capability path mutated the remote: mkdirs=%v new=%v", base.mkdirs, base.mkdirNews)
	}
}

// TestRemoteExplorerMkdirConcurrentSameName: the no-replace create claims a name exactly
// once under concurrent New Directory requests; the losers report fs.ErrExist and no
// partial/merged directory is created.
func TestRemoteExplorerMkdirConcurrentSameName(t *testing.T) {
	fake := newRemoteOpsFake()
	ops, _ := fake.ops(true)
	const n = 8
	results := make(chan error, n)
	for range n {
		go func() {
			_, err := ops.Mkdir(context.Background(), "/remote/w", "race")
			results <- err
		}()
	}
	var wins, exists int
	for range n {
		switch err := <-results; {
		case err == nil:
			wins++
		case errors.Is(err, fs.ErrExist):
			exists++
		default:
			t.Fatalf("concurrent New Directory error = %v", err)
		}
	}
	if wins != 1 || exists != n-1 {
		t.Fatalf("concurrent New Directory wins=%d exists=%d, want 1/%d", wins, exists, n-1)
	}
	if len(fake.mkdirNews) != 1 || !fake.dirs["/remote/w/race"] {
		t.Fatalf("atomic creates = %v dirs=%v", fake.mkdirNews, fake.dirs)
	}
}
