package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/lsp"
	"github.com/tbxark/gvim/plugin/hostos"
)

// fakeLSPProc is an authority long-running child that answers one LSP initialize request
// on its piped stdio, so the spawner test exercises the real transport adapter and the
// real lsp client handshake without a local server process.
type fakeLSPProc struct {
	mu       sync.Mutex
	in       bytes.Buffer
	out      chan hostos.ProcChunk
	done     chan struct{}
	doneOnce sync.Once
	replied  bool
}

func newFakeLSPProc() *fakeLSPProc {
	return &fakeLSPProc{out: make(chan hostos.ProcChunk, 8), done: make(chan struct{})}
}

func (p *fakeLSPProc) PID() int { return 4242 }

func (p *fakeLSPProc) Write(data []byte) error {
	p.mu.Lock()
	p.in.Write(data)
	buffered := p.in.Bytes()
	replied := p.replied
	p.mu.Unlock()
	if replied || !bytes.Contains(buffered, []byte(`"method":"initialize"`)) {
		return nil
	}
	head := bytes.Index(buffered, []byte("\r\n\r\n"))
	if head < 0 {
		return nil
	}
	body := buffered[head+4:]
	var req struct {
		ID     int    `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(body, &req); err != nil || req.Method != "initialize" {
		return nil
	}
	result, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"capabilities": map[string]any{}}})
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(result), result)
	p.mu.Lock()
	p.replied = true
	p.mu.Unlock()
	p.out <- hostos.ProcChunk{Data: []byte(frame)}
	return nil
}

func (p *fakeLSPProc) CloseStdin() {}

func (p *fakeLSPProc) Kill(string) error { p.Terminate(0); return nil }

func (p *fakeLSPProc) Terminate(time.Duration) { p.doneOnce.Do(func() { close(p.done) }) }

func (p *fakeLSPProc) Output(ctx context.Context) (hostos.ProcChunk, bool) {
	select {
	case chunk := <-p.out:
		return chunk, true
	case <-p.done:
		return hostos.ProcChunk{}, false
	case <-ctx.Done():
		return hostos.ProcChunk{}, false
	}
}

func (p *fakeLSPProc) Wait(ctx context.Context) (hostos.ProcResult, error) {
	select {
	case <-p.done:
		return hostos.ProcResult{}, nil
	case <-ctx.Done():
		return hostos.ProcResult{}, ctx.Err()
	}
}

// fakeLongRunner records the start of a remote child.
type fakeLongRunner struct {
	proc hostos.Proc
	argv []string
	opts hostos.ProcOptions
}

func (f *fakeLongRunner) Start(_ context.Context, argv []string, o hostos.ProcOptions) (hostos.Proc, error) {
	f.argv = append([]string(nil), argv...)
	f.opts = o
	return f.proc, nil
}

var _ authority.LongRunning = (*fakeLongRunner)(nil)

// TestRemoteLSPSpawnerRunsOnTheAuthority checks LSP over SSH at the seam: the server
// starts on the remote authority (workspace cwd, the configured command verbatim, the
// locally resolved Program ignored), its stdio completes a real initialize handshake, and
// process limits are recorded as not applicable remotely.
func TestRemoteLSPSpawnerRunsOnTheAuthority(t *testing.T) {
	proc := newFakeLSPProc()
	runner := &fakeLongRunner{proc: proc}
	slot := authoritytest.New()
	slot.LongRunner = runner
	spawn := remoteLSPSpawner(slot, "/remote/proj")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	mem := uint32(50)
	client, err := spawn(ctx, lsp.Config{
		Command: []string{"gopls", "serve"},
		Program: "/local/resolved/gopls",
		RootURI: "file:///remote/proj",
		Limits:  lsp.ProcessLimits{Enabled: true, MaxMemoryPercent: &mem},
	})
	if err != nil {
		t.Fatalf("spawner: %v", err)
	}
	defer func() { _ = client.Close() }()
	if len(runner.argv) != 2 || runner.argv[0] != "gopls" || runner.argv[1] != "serve" {
		t.Fatalf("remote argv = %v, want the configured command verbatim", runner.argv)
	}
	if runner.opts.Cwd != "/remote/proj" {
		t.Fatalf("remote cwd = %q, want the remote workspace", runner.opts.Cwd)
	}
	res, err := client.Initialize(ctx)
	if err != nil {
		t.Fatalf("initialize over the authority transport: %v", err)
	}
	if res == nil {
		t.Fatal("no initialize result")
	}
	if note := client.LimitsNote(); !strings.Contains(note, "not applied remotely") {
		t.Fatalf("LimitsNote = %q, want the remote limits note", note)
	}
}

// TestRemoteLSPSpawnerUsesTheManagersRoot: a manager created for a nested root_markers
// project starts its server in that root, not in the outer workspace. This keeps two
// managers of the same language/name on their own workspace roots.
func TestRemoteLSPSpawnerUsesTheManagersRoot(t *testing.T) {
	proc := newFakeLSPProc()
	runner := &fakeLongRunner{proc: proc}
	slot := authoritytest.New()
	slot.LongRunner = runner
	spawn := remoteLSPSpawner(slot, "/remote/proj")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := spawn(ctx, lsp.Config{
		Command: []string{"gopls", "serve"},
		RootURI: lsp.PathURI("/remote/proj/nested"),
	})
	if err != nil {
		t.Fatalf("spawner: %v", err)
	}
	defer func() { _ = client.Close() }()
	if runner.opts.Cwd != "/remote/proj/nested" {
		t.Fatalf("remote cwd = %q, want the manager's nested root", runner.opts.Cwd)
	}
}

// markerFS answers one ReadDir per directory for the remote root_markers probe.
type markerFS struct {
	authority.UnsupportedFiles
	dirs map[string][]hostos.DirEntry
}

func (f *markerFS) ReadDir(_ context.Context, dir string, _ bool) ([]hostos.DirEntry, error) {
	entries, ok := f.dirs[dir]
	if !ok {
		return nil, fmt.Errorf("missing directory %s", dir)
	}
	return entries, nil
}

// TestRemoteLSMarkerChecker: the authority probe matches exact markers and globs with one
// ReadDir, and reports a disconnected authority instead of falling back to local paths.
func TestRemoteLSMarkerChecker(t *testing.T) {
	slot := authoritytest.New()
	slot.FileSystem = &markerFS{dirs: map[string][]hostos.DirEntry{
		"/remote/ws":     {{Name: "README.md", Kind: "file"}},
		"/remote/ws/sub": {{Name: "go.mod", Kind: "file"}, {Name: "proj.csproj", Kind: "file"}},
	}}
	check := remoteLSMarkerChecker(slot)
	ok, err := check(context.Background(), "/remote/ws/sub", []string{"go.mod"})
	if err != nil || !ok {
		t.Fatalf("exact marker = %v, %v", ok, err)
	}
	ok, err = check(context.Background(), "/remote/ws/sub", []string{"*.csproj"})
	if err != nil || !ok {
		t.Fatalf("glob marker = %v, %v", ok, err)
	}
	if _, err := check(context.Background(), "/remote/ws", []string{"go.mod"}); err != nil {
		t.Fatalf("read-only negative check failed: %v", err)
	}
	slot.Disconnect("carrier down")
	if _, err := check(context.Background(), "/remote/ws/sub", []string{"go.mod"}); err == nil {
		t.Fatal("disconnected checker did not report an error")
	}
}

// TestExpandRemotePathOtherUser: a validated ~user is expanded by the remote shell and
// its path suffix is appended, all through the authority one-shot; invalid user names
// never reach the shell.
func TestExpandRemotePathOtherUser(t *testing.T) {
	slot := authoritytest.New()
	var calls int
	var last []string
	one := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		calls++
		last = append([]string(nil), argv...)
		return hostos.ProcResult{Code: 0, Stdout: []byte("/home/alice")}, nil
	}}
	slot.OneShotter = one
	ctx := context.Background()
	got, err := expandRemotePath(ctx, slot, "~alice/proj/sub")
	if err != nil || got != "/home/alice/proj/sub" {
		t.Fatalf("~alice/proj/sub = %q, %v", got, err)
	}
	if calls != 1 || len(last) != 5 || last[0] != "sh" || last[1] != "-c" || last[3] != "sh" || last[4] != "alice" {
		t.Fatalf("authority argv = %v", last)
	}
	if _, err := expandRemotePath(ctx, slot, "~bad;user/x"); err == nil || calls != 1 {
		t.Fatalf("invalid ~user reached the shell: err=%v calls=%d", err, calls)
	}
	// A shell that cannot expand the name leaves the literal ~user; report it honestly.
	one.script = func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{Code: 0, Stdout: []byte("~nobody")}, nil
	}
	if _, err := expandRemotePath(ctx, slot, "~nobody/x"); err == nil {
		t.Fatal("an unexpanded ~user was accepted")
	}
}

// TestRemoteLSPSpawnerExpandsOtherUserRoot: a remote workspace specified as ~user/proj
// resolves through the remote shell before the server starts; previously the LSP spawner
// refused it even though the rest of the remote file access accepted the root.
func TestRemoteLSPSpawnerExpandsOtherUserRoot(t *testing.T) {
	proc := newFakeLSPProc()
	runner := &fakeLongRunner{proc: proc}
	one := &fakeOneShot{script: func(_ context.Context, argv []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		if len(argv) != 5 || argv[4] != "alice" {
			return hostos.ProcResult{}, fmt.Errorf("unexpected home probe argv %v", argv)
		}
		return hostos.ProcResult{Code: 0, Stdout: []byte("/home/alice")}, nil
	}}
	slot := authoritytest.New()
	slot.OneShotter = one
	slot.LongRunner = runner
	spawn := remoteLSPSpawner(slot, "~alice/proj")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := spawn(ctx, lsp.Config{Command: []string{"gopls", "serve"}})
	if err != nil {
		t.Fatalf("spawner: %v", err)
	}
	defer func() { _ = client.Close() }()
	if runner.opts.Cwd != "/home/alice/proj" {
		t.Fatalf("remote LSP cwd = %q, want the expanded ~user workspace", runner.opts.Cwd)
	}
}

// TestExpandRemotePathRejectsShellTildeNames: POSIX ~- (OLDPWD), bash ~-N/~+N stack
// entries and ~+ (PWD) are shell syntax, not user names; they must be refused before the
// authority runs a command. Normal names containing an internal dash still expand.
func TestExpandRemotePathRejectsShellTildeNames(t *testing.T) {
	slot := authoritytest.New()
	calls := 0
	one := &fakeOneShot{script: func(_ context.Context, _ []string, _ hostos.ProcOptions) (hostos.ProcResult, error) {
		calls++
		return hostos.ProcResult{Code: 0, Stdout: []byte("/home/normal")}, nil
	}}
	slot.OneShotter = one
	for _, path := range []string{"~-/proj", "~-1/proj", "~+/proj", "~+1/proj", "~--/proj"} {
		if _, err := expandRemotePath(context.Background(), slot, path); err == nil {
			t.Fatalf("%s was accepted as a user home", path)
		}
	}
	if calls != 0 {
		t.Fatalf("shell-special tilde names ran %d remote commands", calls)
	}
	got, err := expandRemotePath(context.Background(), slot, "~alice-1/proj")
	if err != nil || got != "/home/normal/proj" {
		t.Fatalf("normal ~alice-1 = %q, %v", got, err)
	}
}
