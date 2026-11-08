package tui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/tbxark/gvim/explorer"
)

// fakeRemoteLoader is an in-memory RemoteDirLoader: the map is the remote host, every
// call is recorded, and a configured error fails the batch.
type fakeRemoteLoader struct {
	mu     sync.Mutex
	dirs   map[string][]RemoteDirEntry
	calls  [][]string
	err    error
	notice string
}

func (l *fakeRemoteLoader) set(dir string, entries ...RemoteDirEntry) *fakeRemoteLoader {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dirs == nil {
		l.dirs = map[string][]RemoteDirEntry{}
	}
	l.dirs[dir] = entries
	return l
}

func (l *fakeRemoteLoader) ReadDirs(_ context.Context, dirs []string) (map[string][]RemoteDirEntry, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, append([]string(nil), dirs...))
	if l.err != nil {
		return nil, l.err
	}
	out := make(map[string][]RemoteDirEntry, len(dirs))
	for _, d := range dirs {
		if e, ok := l.dirs[d]; ok {
			out[d] = append([]RemoteDirEntry(nil), e...)
		}
	}
	return out, nil
}

// TakeNotice implements the optional load-notice surface the tree forwards to the root.
func (l *fakeRemoteLoader) TakeNotice() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	notice := l.notice
	l.notice = ""
	return notice
}

func (l *fakeRemoteLoader) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

func remoteTreeTestLoader() *fakeRemoteLoader {
	return (&fakeRemoteLoader{}).
		set("/srv/proj",
			RemoteDirEntry{Name: "cmd", IsDir: true},
			RemoteDirEntry{Name: "main.go"},
			RemoteDirEntry{Name: ".hidden.go", Hidden: true}).
		set("/srv/proj/cmd", RemoteDirEntry{Name: "app.go"})
}

func remoteRowNames(rows []explorer.Row) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}

func wantRemoteRows(t *testing.T, tree *RemoteExplorerTree, want ...string) {
	t.Helper()
	got := remoteRowNames(tree.Rows())
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rows = %v, want %v", got, want)
		}
	}
}

// TestRemoteTreeStartsWithOnlyTheRoot checks the tree reads nothing before the first
// command: the root row is visible, no loader call has happened.
func TestRemoteTreeStartsWithOnlyTheRoot(t *testing.T) {
	loader := remoteTreeTestLoader()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	wantRemoteRows(t, tree, "proj")
	if loader.callCount() != 0 {
		t.Fatalf("the tree listed %d directories before any command", loader.callCount())
	}
	if sel, ok := tree.Selected(); !ok || sel.Path != "/srv/proj" {
		t.Fatalf("selection = %+v, want the root", sel)
	}
}

// TestRemoteTreeToggleLoadsInTheCommand checks a toggle returns a command and does no
// remote read until the command runs; the listing then shows the directory expanded,
// directories first, with hidden entries filtered until the toggle is on.
func TestRemoteTreeToggleLoadsInTheCommand(t *testing.T) {
	loader := remoteTreeTestLoader()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	cmd := tree.ToggleCmd("/srv/proj")
	if cmd == nil {
		t.Fatal("ToggleCmd returned no command for an unloaded directory")
	}
	if loader.callCount() != 0 {
		t.Fatalf("the toggle read %d directories on the update loop", loader.callCount())
	}
	msg, ok := cmd().(remoteTreeToggleMsg)
	if !ok {
		t.Fatalf("toggle command returned %T", cmd())
	}
	if err := tree.applyToggle(msg); err != nil {
		t.Fatalf("applyToggle: %v", err)
	}
	if loader.callCount() != 1 {
		t.Fatalf("loader calls = %d, want one", loader.callCount())
	}
	wantRemoteRows(t, tree, "proj", "cmd", "main.go")
	tree.SetShowHidden(true)
	wantRemoteRows(t, tree, "proj", "cmd", ".hidden.go", "main.go")
	// An already loaded directory toggles without a command and without a read.
	if cmd := tree.ToggleCmd("/srv/proj"); cmd != nil {
		t.Fatalf("toggling a loaded directory returned a command")
	}
	wantRemoteRows(t, tree, "proj")
	if loader.callCount() != 1 {
		t.Fatalf("collapsing read the directory again: %d calls", loader.callCount())
	}
}

// TestRemoteTreeNaturalOrder checks the rows follow the local tree: directories first,
// names naturally (chapter-2 before chapter-10), case-insensitively.
func TestRemoteTreeNaturalOrder(t *testing.T) {
	loader := (&fakeRemoteLoader{}).set("/r",
		RemoteDirEntry{Name: "chapter-10.md"},
		RemoteDirEntry{Name: "chapter-2.md"},
		RemoteDirEntry{Name: "Zoo", IsDir: true},
		RemoteDirEntry{Name: "alpha", IsDir: true},
	)
	tree := NewRemoteExplorerTree("/r", loader)
	applyRemoteInitial(t, tree)
	wantRemoteRows(t, tree, "r", "alpha", "Zoo", "chapter-2.md", "chapter-10.md")
}

// applyRemoteInitial runs the initial reload command and applies it.
func applyRemoteInitial(t *testing.T, tree *RemoteExplorerTree) {
	t.Helper()
	cmd := tree.ReloadCmd("")
	if cmd == nil {
		t.Fatal("ReloadCmd returned no command")
	}
	msg, ok := cmd().(remoteTreeLoadedMsg)
	if !ok {
		t.Fatalf("reload command returned %T", cmd())
	}
	if err := tree.ApplyReload(msg); err != nil {
		t.Fatalf("ApplyReload: %v", err)
	}
}

// TestRemoteTreeStaleToggleIsDropped checks a toggle result from before a newer reload is
// thrown away, so a slow expansion cannot overwrite the fresh tree.
func TestRemoteTreeStaleToggleIsDropped(t *testing.T) {
	loader := remoteTreeTestLoader()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	staleToggle := tree.ToggleCmd("/srv/proj")
	reload := tree.ReloadCmd("")
	if reload == nil {
		t.Fatal("ReloadCmd returned no command")
	}
	toggleMsg := staleToggle().(remoteTreeToggleMsg)
	reloadMsg := reload().(remoteTreeLoadedMsg)
	// The reload's result is newer: applying the old toggle first or after must not
	// change the rows beyond the reload's listing.
	if err := tree.ApplyReload(reloadMsg); err != nil {
		t.Fatalf("ApplyReload: %v", err)
	}
	if err := tree.applyToggle(toggleMsg); err != nil {
		t.Fatalf("stale toggle error = %v, want it dropped", err)
	}
	wantRemoteRows(t, tree, "proj", "cmd", "main.go")
}

// TestRemoteTreeReloadPrunesAndKeepsExpansion checks a reload drops entries that are
// gone, keeps the loaded subdirectory and its expansion.
func TestRemoteTreeReloadPrunesAndKeepsExpansion(t *testing.T) {
	loader := remoteTreeTestLoader()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	applyRemoteInitial(t, tree)
	openCmd := tree.ToggleCmd("/srv/proj/cmd")
	if err := tree.applyToggle(openCmd().(remoteTreeToggleMsg)); err != nil {
		t.Fatalf("toggle cmd: %v", err)
	}
	wantRemoteRows(t, tree, "proj", "cmd", "app.go", "main.go")
	// The remote side removes main.go; a reload lists the loaded directories, so the
	// removed entry goes and cmd stays expanded.
	loader.set("/srv/proj", RemoteDirEntry{Name: "cmd", IsDir: true}).set("/srv/proj/cmd", RemoteDirEntry{Name: "app.go"})
	applyRemoteInitial(t, tree)
	wantRemoteRows(t, tree, "proj", "cmd", "app.go")
}

// TestRemoteTreeErrorAndRetry checks a failed listing is returned once (the root shows
// it) and a later successful reload recovers the tree.
func TestRemoteTreeErrorAndRetry(t *testing.T) {
	loader := remoteTreeTestLoader()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	loader.mu.Lock()
	loader.err = ErrRemoteExplorerDisconnected
	loader.mu.Unlock()
	msg := tree.ReloadCmd("")().(remoteTreeLoadedMsg)
	if err := tree.ApplyReload(msg); !errors.Is(err, ErrRemoteExplorerDisconnected) {
		t.Fatalf("reload error = %v, want the disconnected error", err)
	}
	loader.mu.Lock()
	loader.err = nil
	loader.mu.Unlock()
	applyRemoteInitial(t, tree)
	wantRemoteRows(t, tree, "proj", "cmd", "main.go")
}

// TestRemoteTreeReveal checks a loaded reveal selects the row and an unloaded one is a
// not-found so the root reloads the waypoints and reveals it in the new listing.
func TestRemoteTreeReveal(t *testing.T) {
	loader := remoteTreeTestLoader()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	applyRemoteInitial(t, tree)
	if err := tree.RevealLoaded("/srv/proj/main.go"); err != nil {
		t.Fatalf("RevealLoaded loaded path: %v", err)
	}
	if sel, _ := tree.Selected(); sel.Path != "/srv/proj/main.go" {
		t.Fatalf("selection = %q, want the revealed file", sel.Path)
	}
	if err := tree.RevealLoaded("/srv/proj/deep/x.go"); !errors.Is(err, explorer.ErrNotFound) {
		t.Fatalf("RevealLoaded unloaded path = %v, want not found", err)
	}
	// The remote side now has deep/x.go: the root listing names the new directory, and
	// the waypoint listing loads it so the reveal can select the file.
	loader.set("/srv/proj",
		RemoteDirEntry{Name: "cmd", IsDir: true},
		RemoteDirEntry{Name: "main.go"},
		RemoteDirEntry{Name: "deep", IsDir: true},
	).set("/srv/proj/deep", RemoteDirEntry{Name: "x.go"})
	cmd := tree.ReloadCmd("/srv/proj/deep/x.go")
	apply := cmd().(remoteTreeLoadedMsg)
	if err := tree.ApplyReload(apply); err != nil {
		t.Fatalf("ApplyReload: %v", err)
	}
	wantRemoteRows(t, tree, "proj", "cmd", "deep", "x.go", "main.go")
	if sel, _ := tree.Selected(); sel.Path != "/srv/proj/deep/x.go" {
		t.Fatalf("selection = %q, want the revealed deep file", sel.Path)
	}
}

// TestRemoteTreeWriteCapabilities checks the tree advertises the capability shape the
// panel and the root consult: file creation/rename/delete and the bounded subtree
// clipboard are available, and remote gitignore filtering stays unsupported.
func TestRemoteTreeWriteCapabilities(t *testing.T) {
	tree := NewRemoteExplorerTree("/srv/proj", remoteTreeTestLoader())
	if tree.ReadOnly() {
		t.Fatal("the remote tree reports read-only although file operations are available")
	}
	if tree.ClipboardDisabled() {
		t.Fatal("the remote tree still reports the clipboard unavailable")
	}
	if tree.ShowIgnored() {
		t.Fatal("the remote tree lists ignored entries by default")
	}
	tree.SetShowIgnored(true)
	if !tree.ShowIgnored() {
		t.Fatal("SetShowIgnored did not include the ignored entries")
	}
	tree.SetShowIgnored(false)
	if tree.ShowIgnored() {
		t.Fatal("SetShowIgnored did not hide the ignored entries again")
	}
}

// TestRemoteTreeHidesIgnoredRows checks the loader's Ignored flag hides rows by default
// and the show-ignored toggle shows them, as the local tree does.
func TestRemoteTreeHidesIgnoredRows(t *testing.T) {
	loader := (&fakeRemoteLoader{}).set("/srv/proj",
		RemoteDirEntry{Name: "a.go"},
		RemoteDirEntry{Name: "b.log", Ignored: true},
	)
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	applyRemoteInitial(t, tree)
	wantRemoteRows(t, tree, "proj", "a.go")
	tree.SetShowIgnored(true)
	wantRemoteRows(t, tree, "proj", "a.go", "b.log")
	tree.SetShowIgnored(false)
	wantRemoteRows(t, tree, "proj", "a.go")
}

// TestRemoteTreeTakeLoadNotice checks a degraded load/watch notice is handed to the root
// once.
func TestRemoteTreeTakeLoadNotice(t *testing.T) {
	loader := remoteTreeTestLoader()
	tree := NewRemoteExplorerTree("/srv/proj", loader)
	loader.mu.Lock()
	loader.notice = "remote directory watching is not supported by this backend; refresh manually (Ctrl+R)"
	loader.mu.Unlock()
	if got := tree.TakeLoadNotice(); !strings.Contains(got, "not supported by this backend") {
		t.Fatalf("notice = %q", got)
	}
	if got := tree.TakeLoadNotice(); got != "" {
		t.Fatalf("the notice repeated: %q", got)
	}
}

// TestRemoteTreeErrorNotice checks the notice text names the disconnected workspace.
func TestRemoteTreeErrorNotice(t *testing.T) {
	if got := noticeRemoteExplorerError(ErrRemoteExplorerDisconnected); got != ErrRemoteExplorerDisconnected.Error() {
		t.Fatalf("notice = %q, want the disconnected notice", got)
	}
	if got := noticeRemoteExplorerError(errors.New("boom")); got != "boom" {
		t.Fatalf("notice = %q, want the error text", got)
	}
}
