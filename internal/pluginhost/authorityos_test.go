package pluginhost

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/plugin/plugintest"
	"github.com/tbxark/gvim/pluginapi"
)

// fakeRemoteFiles is an authority.FileSystem over an in-memory remote tree. It records
// every path the plugin OS asks of it; the local decoy (plugintest.FakeOS) records the
// same calls, so a test can prove nothing went to the local machine.
type fakeRemoteFiles struct {
	mu    sync.Mutex
	files map[string]string
	dirs  map[string][]hostos.DirEntry
	reads []string
	// sizes overrides Stat's Size (a test can claim a huge file without allocating it).
	sizes map[string]int64
	// links records symlink paths and their (display) targets; Stat reports them as
	// symlinks and ReadDir never descends them.
	links map[string]string
	// beforeReadDir, when set, runs at the start of every ReadDir (cancellation hooks).
	beforeReadDir func(string)
}

func newFakeRemoteFiles() *fakeRemoteFiles {
	return &fakeRemoteFiles{
		files: map[string]string{"/srv/proj/remote.txt": "remote data"},
		dirs: map[string][]hostos.DirEntry{
			"/srv/proj": {{Name: "remote.txt", Kind: "file", Size: 11}},
		},
	}
}
func (f *fakeRemoteFiles) ReadFile(_ context.Context, p string, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads = append(f.reads, p)
	data, ok := f.files[p]
	if !ok {
		return nil, io.ErrUnexpectedEOF
	}
	return []byte(data), nil
}
func (f *fakeRemoteFiles) ReadRange(ctx context.Context, p string, off, n int64) ([]byte, error) {
	data, err := f.ReadFile(ctx, p, n)
	if err != nil {
		return nil, err
	}
	if off > int64(len(data)) {
		return nil, io.EOF
	}
	end := min(off+n, int64(len(data)))
	return data[off:end], nil
}
func (f *fakeRemoteFiles) WriteFile(_ context.Context, p string, data []byte, _ hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[p] = string(data)
	return nil
}
func (f *fakeRemoteFiles) Stat(_ context.Context, p string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if data, ok := f.files[p]; ok {
		size := int64(len(data))
		if override, ok := f.sizes[p]; ok {
			size = override
		}
		return hostos.FileStat{Kind: "file", Size: size}, nil
	}
	if target, ok := f.links[p]; ok {
		return hostos.FileStat{Kind: "symlink", Size: int64(len(target))}, nil
	}
	if _, ok := f.dirs[p]; ok {
		return hostos.FileStat{Kind: "dir"}, nil
	}
	return hostos.FileStat{}, pluginapi.Errf(pluginapi.CodeNotFound, "%s: no such file", p)
}
func (f *fakeRemoteFiles) ReadDir(_ context.Context, p string, _ bool) ([]hostos.DirEntry, error) {
	if f.beforeReadDir != nil {
		f.beforeReadDir(p)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, ok := f.dirs[p]
	if !ok {
		return nil, pluginapi.Errf(pluginapi.CodeNotFound, "%s: no such directory", p)
	}
	return append([]hostos.DirEntry(nil), entries...), nil
}
func (f *fakeRemoteFiles) Watch(_ context.Context, p string, _ hostos.WatchOptions) (hostos.Watch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return &fakeRemoteWatch{events: []hostos.WatchEvent{{Path: p, Kind: "write"}}}, nil
}

// fakeMutableFiles adds the FileMutations surface.
type fakeMutableFiles struct {
	*fakeRemoteFiles
	mu      sync.Mutex
	mkdirs  []string
	removes []string
	renames [][2]string
}

func (f *fakeMutableFiles) Mkdir(_ context.Context, p string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mkdirs = append(f.mkdirs, p)
	return nil
}
func (f *fakeMutableFiles) Remove(_ context.Context, p string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes = append(f.removes, p)
	return nil
}
func (f *fakeMutableFiles) Rename(_ context.Context, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renames = append(f.renames, [2]string{from, to})
	return nil
}

type fakeOneShot struct {
	mu   sync.Mutex
	args []string
	cwd  string
	env  map[string]string
	opts hostos.ProcOptions
}

func (f *fakeOneShot) Run(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.ProcResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.args, f.cwd, f.env, f.opts = append([]string(nil), argv...), o.Cwd, o.Env, o
	return hostos.ProcResult{Code: 0, Stdout: []byte("remote out")}, nil
}

type fakeLongRunning struct {
	mu    sync.Mutex
	args  []string
	start bool
}

func (f *fakeLongRunning) Start(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.Proc, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.args, f.start = append([]string(nil), argv...), true
	return &fakeAuthorityProc{}, nil
}

type fakeAuthorityProc struct{}

func (*fakeAuthorityProc) PID() int                { return 42 }
func (*fakeAuthorityProc) Write([]byte) error      { return nil }
func (*fakeAuthorityProc) CloseStdin()             {}
func (*fakeAuthorityProc) Kill(string) error       { return nil }
func (*fakeAuthorityProc) Terminate(time.Duration) {}
func (*fakeAuthorityProc) Output(context.Context) (hostos.ProcChunk, bool) {
	return hostos.ProcChunk{}, false
}
func (*fakeAuthorityProc) Wait(context.Context) (hostos.ProcResult, error) {
	return hostos.ProcResult{Code: 0}, nil
}

type fakeRemoteWatch struct {
	events []hostos.WatchEvent
}

func (w *fakeRemoteWatch) Next(context.Context) (hostos.WatchEvent, bool) {
	if len(w.events) == 0 {
		return hostos.WatchEvent{}, false
	}
	e := w.events[0]
	w.events = w.events[1:]
	return e, true
}
func (w *fakeRemoteWatch) Stop() {}

// authorityTestSlot mounts a fake authority over files and the scripted process surfaces.
func authorityTestSlot(files authority.FileSystem) (authority.Slot, *fakeOneShot, *fakeLongRunning) {
	one, long := &fakeOneShot{}, &fakeLongRunning{}
	slot := authority.New(authority.Parts{
		Files:       files,
		OneShot:     one,
		LongRunning: long,
		Label:       "SSH:u@h",
		RemoteSpec:  &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/main.go", Raw: "u@h:/srv/proj/main.go"},
	})
	return slot, one, long
}

// TestAuthorityOSRoutesFilesProcessesAndWatch: gvim.fs/gvim.proc calls land on the
// authority's remote stub and never on the local OS; relative paths resolve against the
// remote workspace root; the watch is the authority's watch.
func TestAuthorityOSRoutesFilesProcessesAndWatch(t *testing.T) {
	remote := newFakeRemoteFiles()
	mutable := &fakeMutableFiles{fakeRemoteFiles: remote}
	slot, one, long := authorityTestSlot(mutable)
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	ctx := context.Background()

	if got, err := osys.ReadFile(ctx, "remote.txt", 1<<20); err != nil || string(got) != "remote data" {
		t.Fatalf("ReadFile = %q, %v; want the remote stub's data", got, err)
	}
	if err := osys.WriteFile(ctx, "new.txt", []byte("written"), hostos.WriteOptions{}); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if got, err := osys.Stat(ctx, "new.txt"); err != nil || got.Size != 7 {
		t.Fatalf("Stat = %+v, %v", got, err)
	}
	if entries, err := osys.ReadDir(ctx, ".", false); err != nil || len(entries) != 1 || entries[0].Name != "remote.txt" {
		t.Fatalf("ReadDir = %+v, %v", entries, err)
	}
	if err := osys.Mkdir(ctx, "dir", true); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if err := osys.Rename(ctx, "a.txt", "b.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := osys.Remove(ctx, "c.txt", false); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(mutable.mkdirs) != 1 || mutable.mkdirs[0] != "/srv/proj/dir" {
		t.Fatalf("remote mkdirs = %v, want the resolved workspace path", mutable.mkdirs)
	}
	if len(mutable.renames) != 1 || mutable.renames[0] != [2]string{"/srv/proj/a.txt", "/srv/proj/b.txt"} {
		t.Fatalf("remote renames = %v", mutable.renames)
	}
	if len(mutable.removes) != 1 || mutable.removes[0] != "/srv/proj/c.txt" {
		t.Fatalf("remote removes = %v", mutable.removes)
	}

	res, err := osys.Run(ctx, []string{"git", "status"}, hostos.ProcOptions{Cwd: "sub", Env: map[string]string{"A": "B"}, Timeout: 5 * time.Second, MaxOutput: 4096})
	if err != nil || res.Code != 0 || string(res.Stdout) != "remote out" {
		t.Fatalf("Run = %+v, %v", res, err)
	}
	if one.cwd != "/srv/proj/sub" || one.env["A"] != "B" || one.opts.Timeout != 5*time.Second || one.opts.MaxOutput != 4096 {
		t.Fatalf("remote one-shot = cwd %q env %v opts %+v", one.cwd, one.env, one.opts)
	}
	if proc, err := osys.Start(ctx, []string{"agent"}, hostos.ProcOptions{}); err != nil || proc == nil {
		t.Fatalf("Start = %v, %v", proc, err)
	}
	if len(long.args) != 1 || long.args[0] != "agent" {
		t.Fatalf("remote stdio process = %v", long.args)
	}

	watch, err := osys.Watch(ctx, "remote.txt", hostos.WatchOptions{})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	event, ok := watch.Next(ctx)
	if !ok || event.Kind != "write" {
		t.Fatalf("watch event = %+v, %v; want the authority's event", event, ok)
	}
	watch.Stop()

	// Nothing about the files, processes or watch went to the local decoy.
	if got := local.Calls(); len(got) != 0 {
		t.Fatalf("local OS file calls = %v, want none", got)
	}
	if got := local.Runs(); len(got) != 0 {
		t.Fatalf("local OS runs = %v, want none", got)
	}
	if got := local.Watches(); len(got) != 0 {
		t.Fatalf("local OS watches = %v, want none", got)
	}
}

// TestAuthorityOSUnsupportedSurfacesAreClear: a backend without FileMutations refuses
// mkdir/remove/rename with CodeUnsupported, and RealPath has no remote readlink
// primitive, so it says so instead of touching the local machine (copy/walk are covered
// by their own tests and use the remote file surface).
func TestAuthorityOSUnsupportedSurfacesAreClear(t *testing.T) {
	slot, _, _ := authorityTestSlot(newFakeRemoteFiles())
	local := plugintest.NewFakeOS()
	osys := NewAuthorityOS(slot, local, nil)
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"mkdir", func() error { return osys.Mkdir(ctx, "d", false) }},
		{"remove", func() error { return osys.Remove(ctx, "d", false) }},
		{"rename", func() error { return osys.Rename(ctx, "a", "b") }},
		{"realpath", func() error {
			_, err := osys.RealPath(ctx, "a")
			return err
		}},
	} {
		err := tc.call()
		if err == nil {
			t.Fatalf("%s: no error", tc.name)
		}
		if got := pluginapi.CodeOf(err); got != pluginapi.CodeUnsupported {
			t.Fatalf("%s: code = %q (%v), want unsupported", tc.name, got, err)
		}
	}
	if got := local.Calls(); len(got) != 0 {
		t.Fatalf("unsupported surfaces touched the local OS: %v", got)
	}
}

// TestAuthorityOSLocalOnlyAPIsStayLocal: LookPath/Getenv/Dirs/Now are documented
// local-machine APIs, so they read the local OS and not the remote slot.
func TestAuthorityOSLocalOnlyAPIsStayLocal(t *testing.T) {
	slot, _, _ := authorityTestSlot(newFakeRemoteFiles())
	local := plugintest.NewFakeOS()
	local.Env = map[string]string{"HOME": "/local/home"}
	local.Bin = map[string]string{"git": "/usr/bin/git"}
	local.Dir = hostos.Dirs{Config: "/local/cfg", Home: "/local/home", Temp: "/local/tmp", Cwd: "/local/cwd", Root: "/local/cwd"}
	osys := NewAuthorityOS(slot, local, nil)
	if got, ok := osys.LookPath("git"); !ok || got != "/usr/bin/git" {
		t.Fatalf("LookPath = %q, %v; want the local lookup", got, ok)
	}
	if got, ok := osys.Getenv("HOME"); !ok || got != "/local/home" {
		t.Fatalf("Getenv = %q, %v", got, ok)
	}
	if got := osys.Dirs(); got.Root != "/local/cwd" || got.Config != "/local/cfg" {
		t.Fatalf("Dirs = %+v; want the local dirs", got)
	}
	if got := osys.Now(); got.IsZero() {
		t.Fatal("Now returned the zero time")
	}
}

// TestNewWrapOSInstallsTheAuthorityOS: Options.WrapOS receives the local plugin OS and
// its result is the runtime's OS (the seam the app uses for a remote launch).
func TestNewWrapOSInstallsTheAuthorityOS(t *testing.T) {
	var seen hostos.OS
	wrapped := hostos.NopOS{}
	pw := New(config.Config{}, editor.New(editor.Options{}), func(*editor.Document) {}, Options{
		WrapOS: func(local hostos.OS) hostos.OS {
			seen = local
			return wrapped
		},
	})
	if seen == nil {
		t.Fatal("WrapOS did not receive the local plugin OS")
	}
	if _, ok := seen.(*pluginOS); !ok {
		t.Fatalf("WrapOS local OS = %T, want the plugin OS", seen)
	}
	if pw.OS != wrapped {
		t.Fatalf("Wiring.OS = %T, want the wrapper", pw.OS)
	}
}

// TestAuthorityOSGateRefusesBeforeRemoteSpawn: the workspace trust gate is consulted
// before the authority opens a remote process; a refusal never reaches the slot.
func TestAuthorityOSGateRefusesBeforeRemoteSpawn(t *testing.T) {
	slot, one, long := authorityTestSlot(newFakeRemoteFiles())
	local := plugintest.NewFakeOS()
	var gated [][]string
	gate := func(_ context.Context, argv []string, cwd string, env map[string]string) error {
		gated = append(gated, append([]string(nil), argv...))
		if argv[0] == "blocked" {
			return errors.New("workspace is not trusted")
		}
		return nil
	}
	osys := NewAuthorityOS(slot, local, gate)
	ctx := context.Background()
	if _, err := osys.Run(ctx, []string{"blocked"}, hostos.ProcOptions{}); err == nil {
		t.Fatal("Run of a blocked command succeeded")
	}
	if _, err := osys.Start(ctx, []string{"blocked"}, hostos.ProcOptions{}); err == nil {
		t.Fatal("Start of a blocked command succeeded")
	}
	if len(one.args) != 0 || long.start {
		t.Fatalf("a refused launch reached the authority: one-shot %v long %v", one.args, long.start)
	}
	if _, err := osys.Run(ctx, []string{"git", "status"}, hostos.ProcOptions{}); err != nil {
		t.Fatalf("allowed Run: %v", err)
	}
	if len(gated) != 3 {
		t.Fatalf("gate calls = %d, want one per launch", len(gated))
	}
}

// TestNewWithoutWrapOSKeepsTheLocalPluginOS: a local session leaves Options.WrapOS nil
// and keeps the exact plugin OS implementation it had before.
func TestNewWithoutWrapOSKeepsTheLocalPluginOS(t *testing.T) {
	pw := New(config.Config{}, editor.New(editor.Options{}), func(*editor.Document) {}, Options{})
	if _, ok := pw.OS.(*pluginOS); !ok {
		t.Fatalf("local Wiring.OS = %T, want the local plugin OS", pw.OS)
	}
}
