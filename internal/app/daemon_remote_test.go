package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/internal/daemon"
	"github.com/tbxark/gvim/internal/pluginhost"
	"github.com/tbxark/gvim/plugin/hostos"
)

// daemonRemoteFS is the in-memory remote file system of the jump-box tests: the map is the
// remote host, nothing here touches the local disk.
type daemonRemoteFS struct {
	authority.UnsupportedFiles
	mu    sync.Mutex
	dirs  map[string][]hostos.DirEntry
	files map[string][]byte
}

func newDaemonRemoteFS() *daemonRemoteFS {
	return &daemonRemoteFS{dirs: map[string][]hostos.DirEntry{}, files: map[string][]byte{}}
}

func (f *daemonRemoteFS) put(path, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = []byte(content)
	dir := filepath.ToSlash(filepath.Dir(path))
	name := filepath.Base(path)
	f.dirs[dir] = append(f.dirs[dir], hostos.DirEntry{Name: name, Kind: "file"})
}

func (f *daemonRemoteFS) ReadDir(_ context.Context, path string, hidden bool) ([]hostos.DirEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entries, ok := f.dirs[path]
	if !ok {
		return nil, fmt.Errorf("%s: no such remote directory", path)
	}
	return append([]hostos.DirEntry(nil), entries...), nil
}

func (f *daemonRemoteFS) ReadFile(_ context.Context, path string, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("%s: no such remote file", path)
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s: larger than %d bytes", path, max)
	}
	return append([]byte(nil), data...), nil
}

func (f *daemonRemoteFS) ReadRange(_ context.Context, path string, off, n int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.files[path]
	if !ok {
		return nil, fmt.Errorf("%s: no such remote file", path)
	}
	if off >= int64(len(data)) {
		return nil, nil
	}
	end := off + n
	if end > int64(len(data)) {
		end = int64(len(data))
	}
	return append([]byte(nil), data[off:end]...), nil
}

func (f *daemonRemoteFS) Stat(_ context.Context, path string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if data, ok := f.files[path]; ok {
		return hostos.FileStat{Kind: "file", Size: int64(len(data)), Mode: 0o644}, nil
	}
	if _, ok := f.dirs[path]; ok {
		return hostos.FileStat{Kind: "dir", Mode: 0o755}, nil
	}
	return hostos.FileStat{}, fmt.Errorf("%s: no such remote path", path)
}

func (f *daemonRemoteFS) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, errors.New("remote watch is not needed by the jump-box tests")
}

// daemonNoopOneShot answers remote one-shots with an empty successful result; the tests
// exercise the authority wiring, not git/search output.
type daemonNoopOneShot struct{}

func (daemonNoopOneShot) Run(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
	return hostos.ProcResult{}, nil
}

// jumpBoxSlot returns a connected fake SSH slot over fs.
func jumpBoxSlot(fs authority.FileSystem) authority.Slot {
	slot := authoritytest.New()
	slot.FileSystem = fs
	slot.OneShotter = daemonNoopOneShot{}
	slot.LabelText = "u@h"
	return slot
}

// stubMountAuthority replaces the authority mount for one test. The fake slot ignores the
// launch options (the test controls the remote file system).
func stubMountAuthority(t *testing.T, slot authority.Slot) {
	t.Helper()
	old := mountAuthority
	mountAuthority = func(Options, *pluginhost.Wiring, authority.Gate, authority.GateEnv, authority.EnvProvider) authority.Slot {
		return slot
	}
	t.Cleanup(func() { mountAuthority = old })
}

// remoteJumpBoxOptions are the daemon options of the jump-box tests.
func remoteJumpBoxOptions(t *testing.T, name string, spec *authority.RemoteSpec) Options {
	t.Helper()
	return Options{
		Remote:     spec,
		SessionDir: t.TempDir(),
		NoPlugins:  true,
		Daemon:     &DaemonOptions{Mode: DaemonServe, Name: name},
	}
}

// TestDaemonRemoteJumpBoxStartReattachRestart checks a daemon started with a remote target
// creates its remote foreground workspace through the authority (fake SSH), serves it to an
// attach client, survives a detach/reattach, and a restart reuses the persisted identity and
// spec instead of minting a duplicate.
func TestDaemonRemoteJumpBoxStartReattachRestart(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-test-remote-jump")
	workspaces := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspaces)
	fs := newDaemonRemoteFS()
	fs.put("/srv/proj/main.go", "package main\n")
	stubMountAuthority(t, jumpBoxSlot(fs))
	spec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/main.go", Raw: "u@h:/srv/proj/main.go"}
	opts := remoteJumpBoxOptions(t, name, spec)
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if list.Active == "" || len(list.Entries) != 1 {
		t.Fatalf("workspace list = %+v, want the one remote foreground", list)
	}
	active := list.Entries[0]
	if active.Root != "/srv/proj" || active.Label != "proj" || !active.Materialized || !active.Active {
		t.Fatalf("remote foreground = %+v", active)
	}
	reg, err := daemon.OpenRegistry(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	stored, ok, err := reg.Get(active.ID)
	if err != nil || !ok {
		t.Fatalf("stored remote workspace = %+v, %v, %v", stored, ok, err)
	}
	if stored.File.AuthoritySpec != spec.Raw || stored.File.Root != "/srv/proj" {
		t.Fatalf("persisted remote workspace = %+v", stored.File)
	}
	// Detach and reattach: the daemon owns the session and SSH.
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	conn2 := attachDaemonTest(t, name)
	if again := daemonWorkspaceCall(t, conn2, daemon.TypeWorkspaceList, nil); again.Active != active.ID || len(again.Entries) != 1 {
		t.Fatalf("reattach list = %+v", again)
	}
	quitDaemon(t, conn2, done)
	// Restart with the same remote target: the same identity/spec is reused.
	done2 := startDaemonTest(t, opts, name)
	conn3 := attachDaemonTest(t, name)
	restarted := daemonWorkspaceCall(t, conn3, daemon.TypeWorkspaceList, nil)
	if restarted.Active != active.ID || len(restarted.Entries) != 1 {
		t.Fatalf("restart list = %+v, want stable id %q reused", restarted, active.ID)
	}
	quitDaemon(t, conn3, done2)
}

// TestDaemonRemoteLazyDormantWorkspace checks a remote workspace registered by a previous
// daemon run is dormant in a local daemon and materialises its own authority lazily when
// switched to, while the local workspace keeps no authority.
func TestDaemonRemoteLazyDormantWorkspace(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-test-remote-lazy")
	workspaces := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspaces)
	reg, err := daemon.OpenRegistry(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	remote, _, err := reg.CreateWithAuthority("/srv/proj", "", "u@h:/srv/proj")
	if err != nil {
		t.Fatal(err)
	}
	fs := newDaemonRemoteFS()
	fs.put("/srv/proj/main.go", "package main\n")
	stubMountAuthority(t, jumpBoxSlot(fs))
	localRoot := t.TempDir()
	opts := Options{Root: localRoot, SessionDir: t.TempDir(), NoPlugins: true, Daemon: &DaemonOptions{Mode: DaemonServe, Name: name}}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	list := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if list.Active == remote.File.StableID {
		t.Fatalf("the remote workspace is already active: %+v", list)
	}
	var dormant *daemon.WorkspaceInfo
	for i := range list.Entries {
		if list.Entries[i].ID == remote.File.StableID {
			dormant = &list.Entries[i]
		}
	}
	if dormant == nil || dormant.Materialized {
		t.Fatalf("remote workspace not dormant: %+v", list)
	}
	list = daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceSelect, daemon.WorkspaceSelect{ID: remote.File.StableID})
	if list.Active != remote.File.StableID {
		t.Fatalf("lazy remote switch did not activate the workspace: %+v", list)
	}
	for _, e := range list.Entries {
		if e.ID == remote.File.StableID && (!e.Materialized || e.Root != "/srv/proj") {
			t.Fatalf("lazy remote workspace = %+v", e)
		}
	}
	quitDaemon(t, conn, done)
}

// TestDaemonRemoteInvalidSpecFailsSafely checks a stored spec that no longer parses makes
// the lazy switch fail with a notice and keeps the current workspace, never a half-remote
// runtime.
func TestDaemonRemoteInvalidSpecFailsSafely(t *testing.T) {
	isolateConfig(t)
	name := daemonTestHome(t, "app-test-remote-badspec")
	workspaces := t.TempDir()
	t.Setenv("GVIM_WORKSPACE_DIR", workspaces)
	reg, err := daemon.OpenRegistry(workspaces)
	if err != nil {
		t.Fatal(err)
	}
	bad, _, err := reg.CreateWithAuthority("/srv/proj", "", "not a remote target")
	if err != nil {
		t.Fatal(err)
	}
	stubMountAuthority(t, jumpBoxSlot(newDaemonRemoteFS()))
	opts := Options{Root: t.TempDir(), SessionDir: t.TempDir(), NoPlugins: true, Daemon: &DaemonOptions{Mode: DaemonServe, Name: name}}
	done := startDaemonTest(t, opts, name)
	conn := attachDaemonTest(t, name)
	before := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	// Selecting the broken workspace must fail with a notice and leave the active
	// workspace unchanged; read the raw answer because a notice is expected.
	if err := daemon.WriteJSON(conn, daemon.TypeWorkspaceSelect, daemon.WorkspaceSelect{ID: bad.File.StableID}); err != nil {
		t.Fatal(err)
	}
	var notice string
	deadline := time.Now().Add(10 * time.Second)
	for notice == "" {
		if time.Now().After(deadline) {
			t.Fatal("no notice for the broken remote workspace")
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		f, err := daemon.ReadFrame(conn)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			t.Fatal(err)
		}
		if f.Type == daemon.TypeNotice {
			mergeDaemonStash(conn)
			notice = string(f.Payload)
			continue
		}
		stashDaemonOutput(conn, f)
	}
	if !strings.Contains(notice, "remote authority") {
		t.Fatalf("notice = %q, want the remote authority error", notice)
	}
	after := daemonWorkspaceCall(t, conn, daemon.TypeWorkspaceList, nil)
	if after.Active != before.Active {
		t.Fatalf("a broken remote spec switched workspaces: before %+v after %+v", before, after)
	}
	quitDaemon(t, conn, done)
}

// TestAuthoritySpecText checks the persisted target text round-trips and refuses a spec no
// accepted target form can carry.
func TestAuthoritySpecText(t *testing.T) {
	raw := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/main.go", Line: 4, Col: 7, Raw: "u@h:/srv/proj/main.go:4:7"}
	text, err := authoritySpecText(raw)
	if err != nil || text != raw.Raw {
		t.Fatalf("raw spec text = %q, %v", text, err)
	}
	parsed, err := parseAuthoritySpec(text)
	if err != nil || !sameRemoteSpec(*parsed, *raw) {
		t.Fatalf("parsed spec = %+v, %v", parsed, err)
	}
	programmatic := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj", Line: 2}
	text, err = authoritySpecText(programmatic)
	if err != nil {
		t.Fatalf("programmatic default-port spec: %v", err)
	}
	parsed, err = parseAuthoritySpec(text)
	if err != nil || !sameRemoteSpec(*parsed, *programmatic) {
		t.Fatalf("programmatic spec round-trip = %+v, %v, text %q", parsed, err, text)
	}
	if _, err := authoritySpecText(&authority.RemoteSpec{User: "u", Host: "h", Port: 2222, Path: "~"}); err == nil {
		t.Fatal("an unrepresentable spec was accepted")
	}
	if spec, err := parseAuthoritySpec(""); err != nil || spec != nil {
		t.Fatalf("empty spec = %+v, %v", spec, err)
	}
}
