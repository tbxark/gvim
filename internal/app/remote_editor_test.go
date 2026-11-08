package app

import (
	"context"
	"errors"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/authority/authoritytest"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/plugin/hostos"
)

// remoteFS is the in-memory authority.FileSystem of the routing tests: the map is the
// "remote host", and nothing here touches the local filesystem.
type remoteFS struct {
	mu    sync.Mutex
	files map[string][]byte
	mod   map[string]time.Time
	dirs  map[string]bool
	err   error
}

func newRemoteFS() *remoteFS {
	return &remoteFS{files: map[string][]byte{}, mod: map[string]time.Time{}, dirs: map[string]bool{}}
}

func (f *remoteFS) put(path, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[path] = []byte(content)
	f.mod[path] = time.Unix(100, 0)
}

func (f *remoteFS) setErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

func (f *remoteFS) ReadRange(context.Context, string, int64, int64) ([]byte, error) {
	return nil, errors.New("not needed by the remote editor tests")
}

func (f *remoteFS) ReadFile(_ context.Context, path string, max int64) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.files[path]
	if !ok {
		return nil, iofs.ErrNotExist
	}
	if int64(len(data)) > max {
		return nil, errors.New(path + ": larger than the read cap")
	}
	return append([]byte(nil), data...), nil
}

func (f *remoteFS) WriteFile(_ context.Context, path string, data []byte, _ hostos.WriteOptions) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.files[path] = append([]byte(nil), data...)
	f.mod[path] = time.Unix(200, 0)
	return nil
}

func (f *remoteFS) Stat(_ context.Context, path string) (hostos.FileStat, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return hostos.FileStat{}, f.err
	}
	if f.dirs[path] {
		return hostos.FileStat{Kind: "dir", MTime: time.Unix(50, 0)}, nil
	}
	data, ok := f.files[path]
	if !ok {
		return hostos.FileStat{}, iofs.ErrNotExist
	}
	return hostos.FileStat{Kind: "file", Size: int64(len(data)), MTime: f.mod[path]}, nil
}

func (f *remoteFS) ReadDir(context.Context, string, bool) ([]hostos.DirEntry, error) {
	return nil, errors.New("not needed by the routing tests")
}

func (f *remoteFS) Watch(context.Context, string, hostos.WatchOptions) (hostos.Watch, error) {
	return nil, errors.New("not needed by the routing tests")
}

// setupRemote creates an editor and a fake authority slot over fs and installs the remote
// file access, as buildRuntime does after a remote slot connects.
func setupRemote(t *testing.T, fs *remoteFS, path string) (*editor.Editor, string) {
	t.Helper()
	slot := authoritytest.New()
	slot.FileSystem = fs
	spec := &authority.RemoteSpec{Path: path}
	ed := editor.New(editor.Options{Empty: true})
	startup := setupRemoteEditing(ed, slot, spec)
	return ed, startup
}

// TestRemoteOpenAndSaveGoThroughTheAccess checks that a remote file is opened and saved
// solely through the document file access: the local path never exists, before and after
// the save, so no local fallback can hide there.
func TestRemoteOpenAndSaveGoThroughTheAccess(t *testing.T) {
	local := filepath.Join(t.TempDir(), "remote-looking.txt")
	fs := newRemoteFS()
	fs.put(local, "remote text")
	ed, startup := setupRemote(t, fs, local)
	if startup != local {
		t.Fatalf("startup path = %q, want %q", startup, local)
	}
	if err := ed.OpenFile(local); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if got := ed.Active().Buffer.Text(); got != "remote text" {
		t.Fatalf("opened text = %q, want the fake remote content", got)
	}
	if _, err := os.Stat(local); !errors.Is(err, iofs.ErrNotExist) {
		t.Fatalf("the local path exists after a remote open: %v", err)
	}
	ed.Active().InsertText("X")
	if err := ed.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := string(fs.files[local]); got != "Xremote text" {
		t.Fatalf("remote content = %q, want the saved edit", got)
	}
	if _, err := os.Stat(local); !errors.Is(err, iofs.ErrNotExist) {
		t.Fatalf("a local file was created by a remote save: %v", err)
	}
}

// TestRemoteSaveDetectsAConflictAndFailsSafely checks the disk-state check goes to the
// remote stat: a remote file changed since the load refuses the save and changes nothing.
func TestRemoteSaveDetectsAConflictAndFailsSafely(t *testing.T) {
	path := "/remote/a.txt"
	fs := newRemoteFS()
	fs.put(path, "one")
	ed, _ := setupRemote(t, fs, path)
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	ed.Active().InsertText("X")
	fs.mu.Lock()
	fs.mod[path] = time.Unix(300, 0)
	fs.mu.Unlock()
	if err := ed.Save(); err == nil || !strings.Contains(err.Error(), "changed on disk") {
		t.Fatalf("Save after a remote change = %v, want the changed-on-disk error", err)
	}
	if got := string(fs.files[path]); got != "one" {
		t.Fatalf("the refused save wrote %q", got)
	}
}

// TestRemoteFailedOpenIsNotAnEmptyBuffer checks a failed/refused open (disconnected too)
// is an error with no tab bound to the remote path, never an empty buffer that a save
// could write over the remote file.
func TestRemoteFailedOpenIsNotAnEmptyBuffer(t *testing.T) {
	path := "/remote/a.txt"
	fs := newRemoteFS()
	fs.setErr(errors.New("connection is down"))
	ed, _ := setupRemote(t, fs, path)
	if err := ed.OpenFile(path); err == nil {
		t.Fatal("OpenFile on a failing access succeeded")
	}
	for _, d := range ed.Tabs() {
		if d.Path == path {
			t.Fatalf("a failed remote open created a tab for %s (empty buffer risk)", path)
		}
	}
}

// TestRemoteDirectoryIsNotAnEmptyBuffer checks a directory target is an error, not a new
// buffer (the remote explorer is a recorded residual).
func TestRemoteDirectoryIsNotAnEmptyBuffer(t *testing.T) {
	path := "/remote/proj"
	fs := newRemoteFS()
	fs.dirs[path] = true
	ed, _ := setupRemote(t, fs, path)
	if err := ed.OpenFile(path); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("OpenFile of a remote directory = %v, want the directory error", err)
	}
	if len(ed.Tabs()) != 0 {
		t.Fatalf("a remote directory opened %d tabs", len(ed.Tabs()))
	}
}

// TestRemoteSaveAsAndRevertGoThroughTheAccess checks Save As and Reload/Reopen use the
// same remote access, including a new remote path.
func TestRemoteSaveAsAndRevertGoThroughTheAccess(t *testing.T) {
	path := "/remote/a.txt"
	fs := newRemoteFS()
	fs.put(path, "one")
	ed, _ := setupRemote(t, fs, path)
	if err := ed.OpenFile(path); err != nil {
		t.Fatal(err)
	}
	ed.Active().InsertText("X")
	if err := ed.SaveAs("/remote/b.txt"); err != nil {
		t.Fatalf("SaveAs: %v", err)
	}
	if got := string(fs.files["/remote/b.txt"]); got != "Xone" {
		t.Fatalf("save as wrote %q", got)
	}
	fs.put("/remote/b.txt", "fresh")
	if err := ed.Revert(true); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if got := ed.Active().Buffer.Text(); got != "fresh" {
		t.Fatalf("reverted text = %q, want the remote content", got)
	}
}

// TestRemoteStartupPathIsNotTrimmed checks a remote file name with leading/trailing
// spaces survives setup and a save unchanged: the remote path is used exactly as the spec
// gave it, and no trimmed variant is ever read or written.
func TestRemoteStartupPathIsNotTrimmed(t *testing.T) {
	dir := "/remote/  a b .txt "
	fs := newRemoteFS()
	fs.put(dir, "remote text")
	ed, startup := setupRemote(t, fs, dir)
	if startup != dir {
		t.Fatalf("startup path = %q, want the exact spec path %q", startup, dir)
	}
	if err := ed.OpenFile(dir); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if got := ed.Active().Buffer.Text(); got != "remote text" {
		t.Fatalf("opened text = %q, want the exact-path content", got)
	}
	ed.Active().InsertText("X")
	if err := ed.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := string(fs.files[dir]); got != "Xremote text" {
		t.Fatalf("exact remote file = %q, want the saved edit", got)
	}
	if trimmed := strings.TrimSpace(dir); trimmed != dir {
		if _, ok := fs.files[trimmed]; ok {
			t.Fatalf("a trimmed path %q was written", trimmed)
		}
	}
}

// TestRemoteEditingNoticeDescribesLSP checks the startup notice matches what works now:
// LSP over SSH is available (and restarted after a reconnect), the notice no longer claims
// it is unavailable, and the remaining gaps are named.
func TestRemoteEditingNoticeDescribesLSP(t *testing.T) {
	fileSpec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/remote/a.txt"}
	file := remoteEditingNotice(fileSpec, "/remote/a.txt")
	if !strings.Contains(file, "language servers run on the remote host") ||
		!strings.Contains(file, "restarted after a reconnect") {
		t.Fatalf("file notice = %q, want the landed LSP description", file)
	}
	for _, stale := range []string{"LSP over SSH are not available", "LSP over SSH is not available"} {
		if strings.Contains(file, stale) {
			t.Fatalf("file notice still claims LSP over SSH is unavailable: %q", file)
		}
	}
	for _, want := range []string{"Remote explorer", "crash recovery", "file changes on the remote host are watched", "local files are unchanged"} {
		if !strings.Contains(file, want) {
			t.Fatalf("file notice = %q, missing %q", file, want)
		}
	}
	if strings.Contains(file, "external-change watching and crash recovery are not available") {
		t.Fatalf("file notice still calls remote watching unavailable: %q", file)
	}

	dirSpec := &authority.RemoteSpec{User: "u", Host: "h", Path: "/remote/proj/"}
	dir := remoteEditingNotice(dirSpec, "")
	if !strings.Contains(dir, "language servers run on the remote host") {
		t.Fatalf("workspace notice = %q, want the landed LSP description", dir)
	}
	if !strings.Contains(dir, "Remote file browsing is not available yet") {
		t.Fatalf("workspace notice = %q, want the remaining browsing gap", dir)
	}
	if !strings.Contains(dir, "file changes on the remote host are watched") {
		t.Fatalf("workspace notice = %q, want the watched remote changes", dir)
	}
	for _, stale := range []string{"LSP over SSH are not available", "LSP over SSH is not available"} {
		if strings.Contains(dir, stale) {
			t.Fatalf("workspace notice still claims LSP over SSH is unavailable: %q", dir)
		}
	}
}
