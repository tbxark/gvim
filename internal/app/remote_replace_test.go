package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteReplaceFake is an in-memory authority.FileSystem for the remote replace tests.
// Reads/writes/stat calls are recorded; hooks simulate a file changing between the read
// and the write, a write failure or a cancellation.
type remoteReplaceFake struct {
	mu      sync.Mutex
	files   map[string][]byte
	mtime   map[string]time.Time
	kind    map[string]string
	reads   []string
	writes  []string
	written map[string][]byte
	stats   int
	// onStat lets a test change the second stat of a path (a changed file).
	onStat func(path string, n int)
	// onRead runs after the data is read (a test can cancel the context there).
	onRead   func(path string)
	writeErr map[string]error
}

func newRemoteReplaceFake() *remoteReplaceFake {
	return &remoteReplaceFake{files: map[string][]byte{}, mtime: map[string]time.Time{}, kind: map[string]string{}, written: map[string][]byte{}, writeErr: map[string]error{}}
}

func (f *remoteReplaceFake) put(path, text string) *remoteReplaceFake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = []byte(text)
	f.mtime[path] = time.Unix(100, 0)
	f.kind[path] = "file"
	return f
}

func (f *remoteReplaceFake) ReadRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, errors.New("not used by replace")
}

func (f *remoteReplaceFake) ReadFile(ctx context.Context, path string, max int64) ([]byte, error) {
	f.mu.Lock()
	data, ok := f.files[path]
	hook := f.onRead
	f.reads = append(f.reads, path)
	f.mu.Unlock()
	if !ok {
		return nil, os.ErrNotExist
	}
	if hook != nil {
		hook(path)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return append([]byte(nil), data...), nil
}

func (f *remoteReplaceFake) WriteFile(ctx context.Context, path string, data []byte, o hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes = append(f.writes, path)
	if err := f.writeErr[path]; err != nil {
		return err
	}
	if !o.Atomic {
		return errors.New("the remote replace wrote without the atomic flag")
	}
	f.files[path] = append([]byte(nil), data...)
	f.written[path] = append([]byte(nil), data...)
	f.mtime[path] = f.mtime[path].Add(time.Second)
	return nil
}

func (f *remoteReplaceFake) Stat(_ context.Context, path string) (hostos.FileStat, error) {
	f.mu.Lock()
	f.stats++
	n := 0
	for _, r := range f.reads {
		if r == path {
			n++
		}
	}
	hook := f.onStat
	f.mu.Unlock()
	// The hook runs without the lock: a test may mutate the file's mtime there, and the
	// value returned below is the value the adapter sees for this stat.
	if hook != nil {
		hook(path, n)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[path]
	if !ok {
		return hostos.FileStat{Kind: "missing"}, os.ErrNotExist
	}
	return hostos.FileStat{Kind: f.kind[path], Size: int64(len(data)), MTime: f.mtime[path]}, nil
}

func (f *remoteReplaceFake) ReadDir(context.Context, string, bool) ([]hostos.DirEntry, error) {
	return nil, errors.New("not used by replace")
}
func (f *remoteReplaceFake) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, errors.New("not used by replace")
}

func (f *remoteReplaceFake) writeList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.writes...)
}

func remoteReplaceReq(files ...searchpanel.ReplaceFile) searchpanel.ReplaceRequest {
	return searchpanel.ReplaceRequest{
		Root: "/remote/w", Query: "old", Replacement: "new",
		Files: files, Matches: len(files), Limit: searchpanel.DefaultLimit,
	}
}

// TestRemoteReplaceWritesOnlyRemoteFiles checks the replacement reads and writes the
// remote files through the authority and leaves an identical local decoy tree untouched
// (no local fallback), writing each file atomically.
func TestRemoteReplaceWritesOnlyRemoteFiles(t *testing.T) {
	decoy := t.TempDir()
	if err := os.MkdirAll(filepath.Join(decoy, "w"), 0o755); err != nil {
		t.Fatal(err)
	}
	decoyFile := filepath.Join(decoy, "w", "a.go")
	if err := os.WriteFile(decoyFile, []byte("local decoy old"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := newRemoteReplaceFake().put("/remote/w/a.go", "package a // old\n").put("/remote/w/b.go", "package b // old\n")
	r := newRemoteProjectReplacer(fs, nil, "/remote/w", "SSH:u@h")
	req := remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/remote/w/a.go", Hash: hashText("package a // old\n")},
		searchpanel.ReplaceFile{Path: "/remote/w/b.go", Hash: hashText("package b // old\n")},
	)
	res, err := r.ProjectReplaceRemote(context.Background(), req)
	if err != nil {
		t.Fatalf("ProjectReplaceRemote: %v", err)
	}
	if res.Files != 2 || res.Skipped != 0 {
		t.Fatalf("result = %+v, want two files written", res)
	}
	if got := string(fs.written["/remote/w/a.go"]); got != "package a // new\n" {
		t.Fatalf("remote a.go = %q", got)
	}
	if got := string(fs.written["/remote/w/b.go"]); got != "package b // new\n" {
		t.Fatalf("remote b.go = %q", got)
	}
	if data, err := os.ReadFile(decoyFile); err != nil || string(data) != "local decoy old" {
		t.Fatalf("local decoy changed: %q, %v", data, err)
	}
	if len(fs.writeList()) != 2 {
		t.Fatalf("writes = %v, want the two remote files", fs.writeList())
	}
	// ProjectReplace (the local plan interface) must refuse: a remote plan never becomes
	// local edits.
	if _, err := r.ProjectReplace(req); err == nil {
		t.Fatal("the remote replacer accepted the local plan interface")
	}
}

// TestRemoteReplaceSkipsStaleHash checks a file whose search-time hash no longer matches
// is skipped, never written.
func TestRemoteReplaceSkipsStaleHash(t *testing.T) {
	fs := newRemoteReplaceFake().put("/remote/w/a.go", "now different\n")
	r := newRemoteProjectReplacer(fs, nil, "/remote/w", "SSH:u@h")
	res, err := r.ProjectReplaceRemote(context.Background(), remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/remote/w/a.go", Hash: hashText("was old\n")},
	))
	if err != nil {
		t.Fatalf("ProjectReplaceRemote: %v", err)
	}
	if res.Files != 0 || res.Skipped != 1 {
		t.Fatalf("result = %+v, want the stale file skipped", res)
	}
	if got := fs.writeList(); len(got) != 0 {
		t.Fatalf("writes = %v, want none", got)
	}
}

// TestRemoteReplaceSkipsFileChangedWhileRunning checks the pre-write stat: a file whose
// mtime changed between the read and the write is skipped rather than written at a stale
// snapshot.
func TestRemoteReplaceSkipsFileChangedWhileRunning(t *testing.T) {
	fs := newRemoteReplaceFake().put("/remote/w/a.go", "package a // old\n")
	// The first stat (before the read) and the second stat (before the write) see
	// different mtimes, as if another program saved the file during the run.
	stats := 0
	fs.onStat = func(path string, _ int) {
		if path != "/remote/w/a.go" {
			return
		}
		stats++
		if stats == 2 {
			fs.mu.Lock()
			fs.mtime[path] = fs.mtime[path].Add(time.Minute)
			fs.mu.Unlock()
		}
	}
	r := newRemoteProjectReplacer(fs, nil, "/remote/w", "SSH:u@h")
	res, err := r.ProjectReplaceRemote(context.Background(), remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/remote/w/a.go", Hash: hashText("package a // old\n")},
	))
	if err != nil {
		t.Fatalf("ProjectReplaceRemote: %v", err)
	}
	if res.Files != 0 || res.Skipped != 1 {
		t.Fatalf("result = %+v, want the changed file skipped", res)
	}
	if got := fs.writeList(); len(got) != 0 {
		t.Fatalf("writes = %v, want none for the changed file", got)
	}
}

// TestRemoteReplaceAbortsOnWriteErrorWithPartialCount checks a write failure aborts the
// run, names the file and reports how many earlier files were already written.
func TestRemoteReplaceAbortsOnWriteErrorWithPartialCount(t *testing.T) {
	fs := newRemoteReplaceFake().put("/remote/w/a.go", "package a // old\n").put("/remote/w/b.go", "package b // old\n")
	fs.writeErr["/remote/w/b.go"] = errors.New("permission denied")
	r := newRemoteProjectReplacer(fs, nil, "/remote/w", "SSH:u@h")
	res, err := r.ProjectReplaceRemote(context.Background(), remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/remote/w/a.go", Hash: hashText("package a // old\n")},
		searchpanel.ReplaceFile{Path: "/remote/w/b.go", Hash: hashText("package b // old\n")},
	))
	if err == nil || !strings.Contains(err.Error(), "/remote/w/b.go") || !strings.Contains(err.Error(), "1 files already written") {
		t.Fatalf("error = %v, want the failed file and the partial count", err)
	}
	if res.Files != 1 {
		t.Fatalf("partial result = %+v, want one written file", res)
	}
	if got := string(fs.written["/remote/w/a.go"]); got != "package a // new\n" {
		t.Fatalf("remote a.go = %q, want the first atomic write", got)
	}
}

// TestRemoteReplaceCancelsMidRun checks a cancelled context stops before the current file
// is written, with the earlier files reported as written.
func TestRemoteReplaceCancelsMidRun(t *testing.T) {
	fs := newRemoteReplaceFake().put("/remote/w/a.go", "package a // old\n").put("/remote/w/b.go", "package b // old\n")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fs.onRead = func(path string) {
		if path == "/remote/w/b.go" {
			cancel()
		}
	}
	r := newRemoteProjectReplacer(fs, nil, "/remote/w", "SSH:u@h")
	res, err := r.ProjectReplaceRemote(ctx, remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/remote/w/a.go", Hash: hashText("package a // old\n")},
		searchpanel.ReplaceFile{Path: "/remote/w/b.go", Hash: hashText("package b // old\n")},
	))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if res.Files != 1 {
		t.Fatalf("partial result = %+v, want one written file", res)
	}
	if _, ok := fs.written["/remote/w/b.go"]; ok {
		t.Fatal("the cancelled file was written")
	}
}

// TestRemoteReplaceTrustGateRefusesBeforeAnyRemoteIO checks the gate runs first and a
// refusal reads and writes nothing.
func TestRemoteReplaceTrustGateRefusesBeforeAnyRemoteIO(t *testing.T) {
	fs := newRemoteReplaceFake().put("/remote/w/a.go", "package a // old\n")
	gateErr := errors.New("workspace is not trusted: remote commands are disabled")
	calls := 0
	r := newRemoteProjectReplacer(fs, func(context.Context, []string, string) error {
		calls++
		return gateErr
	}, "/remote/w", "SSH:u@h")
	_, err := r.ProjectReplaceRemote(context.Background(), remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/remote/w/a.go", Hash: hashText("package a // old\n")},
	))
	if !errors.Is(err, gateErr) {
		t.Fatalf("error = %v, want the gate refusal", err)
	}
	if calls != 1 || fs.stats != 0 || len(fs.reads) != 0 || len(fs.writes) != 0 {
		t.Fatalf("gate calls = %d stats = %d reads = %v writes = %v, want the gate only", calls, fs.stats, fs.reads, fs.writes)
	}
}

// TestRemoteReplaceBounds checks oversized files are skipped and paths outside the remote
// workspace are refused before any write.
func TestRemoteReplaceBounds(t *testing.T) {
	big := strings.Repeat("x", remoteReplaceMaxFile+1)
	fs := newRemoteReplaceFake().put("/remote/w/big.go", big)
	r := newRemoteProjectReplacer(fs, nil, "/remote/w", "SSH:u@h")
	res, err := r.ProjectReplaceRemote(context.Background(), remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/remote/w/big.go", Hash: hashText(big)},
	))
	if err != nil || res.Skipped != 1 || res.Files != 0 {
		t.Fatalf("oversized = %+v, %v; want one skipped file", res, err)
	}
	fs2 := newRemoteReplaceFake().put("/elsewhere/a.go", "package a // old\n")
	r2 := newRemoteProjectReplacer(fs2, nil, "/remote/w", "SSH:u@h")
	if _, err := r2.ProjectReplaceRemote(context.Background(), remoteReplaceReq(
		searchpanel.ReplaceFile{Path: "/elsewhere/a.go", Hash: hashText("package a // old\n")},
	)); err == nil {
		t.Fatal("a path outside the remote workspace was accepted")
	}
}
