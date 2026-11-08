package daemon

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/session"
)

// newRegistry returns a registry over a fresh temp directory.
func newRegistry(t *testing.T) *Registry {
	t.Helper()
	reg, err := OpenRegistry(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return reg
}

// workspaceFileAt writes a raw WorkspaceFile into the registry, bypassing Save, so a
// test can seed legacy/foreign/future files.
func workspaceFileAt(t *testing.T, reg *Registry, file WorkspaceFile) string {
	t.Helper()
	if file.Version == 0 {
		file.Version = WorkspaceVersion
	}
	path := reg.path(file.Root, file.StableID)
	if err := writeWorkspaceFile(path, file); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRegistryCreateListReopen(t *testing.T) {
	root, err := CanonicalRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := newRegistry(t)
	ws, created, err := reg.Create(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("Create reported an existing workspace")
	}
	if len(ws.File.StableID) != 32 || strings.Trim(ws.File.StableID, "0123456789abcdef") != "" {
		t.Fatalf("stable id = %q", ws.File.StableID)
	}
	if ws.File.Label != filepath.Base(root) {
		t.Fatalf("label = %q, want the root basename", ws.File.Label)
	}
	if ws.File.Root != root {
		t.Fatalf("root = %q, want canonical %q", ws.File.Root, root)
	}
	if base := filepath.Base(ws.Path); strings.ContainsRune(base, '/') || !strings.HasSuffix(base, ws.File.StableID+".json") {
		t.Fatalf("file name = %q", base)
	}
	if _, err := os.Stat(ws.Path); err != nil {
		t.Fatalf("registry file: %v", err)
	}

	// Reopening sees the same workspace and the same identity.
	reopened, err := OpenRegistry(reg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	list, problems := reopened.List()
	if len(problems) != 0 || len(list) != 1 || list[0].File.StableID != ws.File.StableID {
		t.Fatalf("list after reopen = %+v, problems %v", list, problems)
	}
	// Another spelling of the same canonical root is the same workspace.
	again, created, err := reopened.Create(root+"/.", "")
	if err != nil || created || again.File.StableID != ws.File.StableID {
		t.Fatalf("Create of the same root = %+v, created %v, %v", again, created, err)
	}
}

func TestRegistrySymlinkRootIsOneWorkspace(t *testing.T) {
	real, err := CanonicalRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	reg := newRegistry(t)
	viaLink, created, err := reg.Create(link, "")
	if err != nil || !created {
		t.Fatalf("Create(symlink) = %+v, %v, %v", viaLink, created, err)
	}
	if viaLink.File.Root != real {
		t.Fatalf("root = %q, want the resolved %q", viaLink.File.Root, real)
	}
	viaReal, created, err := reg.Create(real, "")
	if err != nil || created || viaReal.File.StableID != viaLink.File.StableID {
		t.Fatalf("Create(real) = %+v, created %v, %v", viaReal, created, err)
	}
	list, _ := reg.List()
	if len(list) != 1 {
		t.Fatalf("list = %+v, want one workspace", list)
	}
}

func TestRegistryDedupKeepsFreshest(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := newRegistry(t)
	older := WorkspaceFile{StableID: strings.Repeat("a", 32), Root: canonical, Label: "old", LastFocusedAt: 100}
	newer := WorkspaceFile{StableID: strings.Repeat("b", 32), Root: canonical, Label: "new", LastFocusedAt: 200}
	workspaceFileAt(t, reg, older)
	workspaceFileAt(t, reg, newer)
	list, problems := reg.List()
	if len(problems) != 0 {
		t.Fatalf("problems: %v", problems)
	}
	if len(list) != 1 || list[0].File.StableID != newer.StableID || list[0].File.Label != "new" {
		t.Fatalf("dedup = %+v, want the freshest workspace", list)
	}
}

func TestRegistryFutureVersionNeverRewritten(t *testing.T) {
	root := t.TempDir()
	canonical, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	reg := newRegistry(t)
	future := WorkspaceFile{Version: WorkspaceVersion + 1, StableID: strings.Repeat("c", 32), Root: canonical, Label: "future", LastFocusedAt: 1}
	path := workspaceFileAt(t, reg, future)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	list, problems := reg.List()
	// The future file is skipped with a diagnostic and never touched.
	if len(problems) == 0 || !strings.Contains(problems[0].Error(), "newer") {
		t.Fatalf("problems = %v, want a newer-version diagnostic", problems)
	}
	if len(list) != 0 {
		t.Fatalf("a future-version file was listed: %+v", list)
	}
	// Create for the same root writes its own file and leaves the future file alone.
	ws, created, err := reg.Create(root, "current")
	if err != nil || !created {
		t.Fatalf("Create after a future version = %+v, created %v, %v", ws, created, err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the future-version file changed: %v", err)
	}
	if ws.Path == path {
		t.Fatal("Create reused the future-version file's path")
	}
	// Save refuses to overwrite a file that has become newer in the meantime.
	if err := os.WriteFile(ws.Path, mustJSON(t, WorkspaceFile{Version: WorkspaceVersion + 1, StableID: ws.File.StableID, Root: canonical}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Save(ws); !errors.Is(err, ErrWorkspaceVersionTooNew) {
		t.Fatalf("Save over a future version = %v, want ErrWorkspaceVersionTooNew", err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestRegistryRenameTouchLayoutPersist(t *testing.T) {
	root := t.TempDir()
	reg := newRegistry(t)
	ws, _, err := reg.Create(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Rename(ws.File.StableID, "  My Project  "); err != nil {
		t.Fatal(err)
	}
	when := time.Unix(1700000000, 0)
	if _, err := reg.Touch(ws.File.StableID, when); err != nil {
		t.Fatal(err)
	}
	layout := &session.Session{Version: 1, Root: root, Active: 1, Files: []session.FileState{{Path: filepath.Join(root, "a.go"), Line: 3}}}
	if _, err := reg.UpdateLayout(ws.File.StableID, layout); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenRegistry(reg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := reopened.Get(ws.File.StableID)
	if err != nil || !ok {
		t.Fatalf("Get = %+v, %v, %v", got, ok, err)
	}
	if got.File.Label != "My Project" {
		t.Fatalf("label = %q", got.File.Label)
	}
	if got.File.LastFocusedAt != when.UnixMilli() {
		t.Fatalf("last focused = %d, want %d", got.File.LastFocusedAt, when.UnixMilli())
	}
	if got.File.Layout == nil || got.File.Layout.Active != 1 || len(got.File.Layout.Files) != 1 || got.File.Layout.Files[0].Line != 3 {
		t.Fatalf("layout = %+v", got.File.Layout)
	}
}

func TestRegistryRemoveKeepsSessionData(t *testing.T) {
	root := t.TempDir()
	reg := newRegistry(t)
	ws, _, err := reg.Create(root, "")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := reg.SessionDir(ws.File.StableID)
	if err != nil {
		t.Fatal(err)
	}
	stash := filepath.Join(dir, "stash")
	if err := os.WriteFile(stash, []byte("unsaved text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := reg.Remove(ws.File.StableID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := reg.Get(ws.File.StableID); ok {
		t.Fatal("the workspace is still listed after Remove")
	}
	if _, err := os.Stat(stash); err != nil {
		t.Fatalf("Remove deleted session data: %v", err)
	}
	if err := reg.RemoveSessionData(ws.File.StableID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stash); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("RemoveSessionData left the stash: %v", err)
	}
}

func TestRegistryCorruptFileIsSkippedNotDeleted(t *testing.T) {
	reg := newRegistry(t)
	bad := filepath.Join(reg.Dir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	list, problems := reg.List()
	if len(list) != 0 || len(problems) != 1 {
		t.Fatalf("list = %+v, problems = %v", list, problems)
	}
	if _, err := os.Stat(bad); err != nil {
		t.Fatalf("the corrupt file was deleted: %v", err)
	}
}

func TestRegistrySaveIsAtomic(t *testing.T) {
	root := t.TempDir()
	reg := newRegistry(t)
	ws, _, err := reg.Create(root, "")
	if err != nil {
		t.Fatal(err)
	}
	ws.File.Label = "renamed"
	if _, err := reg.Save(ws); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(reg.Dir())
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range entries {
		if strings.HasPrefix(de.Name(), ".workspace-") || strings.HasSuffix(de.Name(), ".tmp") {
			t.Fatalf("a temp file was left behind: %s", de.Name())
		}
	}
	// A failed save (read-only directory) leaves the old file unchanged.
	info, err := os.Stat(ws.Path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(ws.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(reg.Dir(), 0o500); err != nil {
		t.Fatal(err)
	}
	ws.File.Label = "never written"
	_, saveErr := reg.Save(ws)
	_ = os.Chmod(reg.Dir(), 0o700)
	after, err := os.ReadFile(ws.Path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("a failed save changed the file: %v", err)
	}
	if saveErr == nil {
		// Running as root can write into a 0500 directory; that is not a failure of
		// the registry, so accept it but still verify the file parses.
		if _, err := readWorkspaceFile(ws.Path); err != nil {
			t.Fatalf("read after a save under a read-only dir: %v", err)
		}
	}
	_ = info
}

func TestEncodeRootIsOneSafeComponent(t *testing.T) {
	enc := encodeRoot("/tmp/a b/c:d")
	if strings.ContainsAny(enc, "/\\") {
		t.Fatalf("encodeRoot = %q contains a separator", enc)
	}
	if !strings.Contains(enc, "%2F") || !strings.Contains(enc, "%20") {
		t.Fatalf("encodeRoot = %q is not percent encoding", enc)
	}
	long := "/" + strings.Repeat("x", 300)
	if got := encodeRoot(long); !strings.HasPrefix(got, "sha256-") {
		t.Fatalf("encodeRoot(long) = %q, want the sha256 fallback", got)
	}
}

func TestWorkspaceDirOverride(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ws")
	t.Setenv("GVIM_WORKSPACE_DIR", dir)
	got, err := WorkspaceDir()
	if err != nil || got != dir {
		t.Fatalf("WorkspaceDir = %q, %v", got, err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("the override is not a directory: %v", err)
	}
}

func TestCanonicalRootRejectsFiles(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := CanonicalRoot(file); err == nil {
		t.Fatal("CanonicalRoot accepted a file")
	}
	if _, err := CanonicalRoot(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("CanonicalRoot accepted a missing path")
	}
}

// TestDiscoverForegroundAndDormant checks daemon-start discovery: the launch root is the
// foreground workspace (created when unknown), every other workspace is dormant, and no
// runtime flags are set by the registry.
func TestDiscoverForegroundAndDormant(t *testing.T) {
	a, err := CanonicalRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	b, err := CanonicalRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reg := newRegistry(t)
	if _, _, err := reg.Create(b, "second"); err != nil {
		t.Fatal(err)
	}
	d, err := reg.Discover(a)
	if err != nil {
		t.Fatal(err)
	}
	if d.Foreground.File.Root != a {
		t.Fatalf("foreground root = %q, want %q", d.Foreground.File.Root, a)
	}
	if d.Foreground.Materialized || d.Foreground.Active {
		t.Fatal("the registry set runtime flags")
	}
	if d.Foreground.File.StableID == "" {
		t.Fatal("the foreground workspace has no stable id")
	}
	// The discovered foreground is registered (a second call reuses it).
	again, err := reg.Discover(a)
	if err != nil {
		t.Fatal(err)
	}
	if again.Foreground.File.StableID != d.Foreground.File.StableID {
		t.Fatal("Discover minted a second workspace for the same root")
	}
	if len(d.Dormant) != 1 || d.Dormant[0].File.Root != b {
		t.Fatalf("dormant = %+v, want the other workspace", d.Dormant)
	}
	if list, _ := reg.List(); len(list) != 2 {
		t.Fatal("discovery did not register the foreground workspace")
	}
}
