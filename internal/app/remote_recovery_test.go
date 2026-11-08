package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/restore"
)

// TestRemoteRecoverySessionDirIsKeyedByTarget checks the staging directory is stable per
// remote target and differs between targets (and honors the test override).
func TestRemoteRecoverySessionDirIsKeyedByTarget(t *testing.T) {
	t.Setenv("GVIM_REMOTE_SESSION_DIR", t.TempDir())
	a := &authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/a.go"}
	b := &authority.RemoteSpec{User: "u", Host: "h", Path: "/other/proj/a.go"}
	first := remoteRecoverySessionDir(a)
	if first == "" {
		t.Fatal("no staging directory")
	}
	if again := remoteRecoverySessionDir(&authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/a.go"}); again != first {
		t.Fatalf("same target got %q then %q", first, again)
	}
	// Two files of the same remote workspace share its session directory (it is keyed by
	// the workspace root, not the launched file).
	if sameWorkspace := remoteRecoverySessionDir(&authority.RemoteSpec{User: "u", Host: "h", Path: "/srv/proj/b.go"}); sameWorkspace != first {
		t.Fatalf("files of one workspace got %q and %q", first, sameWorkspace)
	}
	if other := remoteRecoverySessionDir(b); other == first {
		t.Fatalf("different workspaces share the staging directory %q", first)
	}
	if !strings.HasPrefix(first, os.Getenv("GVIM_REMOTE_SESSION_DIR")) {
		t.Fatalf("staging dir %q ignores the override", first)
	}
}

// TestRemoteRecoveryStagesAndRestoresThroughAuthority checks the hot-exit copy of a remote
// document is staged locally with the remote path recorded, and a restart restores the
// unsaved text on the remote path through the authority file access (not a local file).
func TestRemoteRecoveryStagesAndRestoresThroughAuthority(t *testing.T) {
	t.Setenv("GVIM_REMOTE_SESSION_DIR", t.TempDir())
	remotePath := "/remote/proj/a.go"
	fs := newRemoteFS()
	fs.put(remotePath, "package a\n")
	spec := &authority.RemoteSpec{User: "u", Host: "h", Path: remotePath}
	dir := remoteRecoverySessionDir(spec)
	if dir == "" {
		t.Fatal("no remote staging session directory")
	}

	// Edit the remote file without saving and stage its unsaved text as the recovery
	// writer does on its interval.
	ed, _ := setupRemote(t, fs, remotePath)
	if err := ed.OpenFile(remotePath); err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	ed.Active().InsertText("// draft\n")
	if !ed.Active().Modified() {
		t.Fatal("the edit did not mark the remote buffer modified")
	}
	// The recovery writer lives in restore; drive its plan directly the way the root's
	// recovery command does.
	writer := restore.NewRecoveryWriter(ed, restore.RecoveryDirFor(dir, true))
	if err := writer.Plan()(); err != nil {
		t.Fatalf("staging the recovery copy: %v", err)
	}
	staged, err := os.ReadDir(restore.RecoveryPath(dir))
	if err != nil || len(staged) == 0 {
		t.Fatalf("staging dir = %v, %v; want the recovery session files", staged, err)
	}
	found := false
	walkErr := filepath.WalkDir(restore.RecoveryPath(dir), func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(p)
		if err == nil && strings.Contains(string(data), remotePath) {
			found = true
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk the staging directory: %v", walkErr)
	}
	if !found {
		t.Fatalf("no staged file records the remote path %q", remotePath)
	}
	// The local file named by the remote path does not exist: only the remote backend has
	// it, so the staging copy cannot be a local-file fallback.
	if _, err := os.Stat(remotePath); !os.IsNotExist(err) {
		t.Fatalf("the local path %q exists: %v", remotePath, err)
	}

	// A restart: fresh editor, the same remote file access, detect the staging copy.
	fs2 := newRemoteFS()
	fs2.put(remotePath, "package a\n")
	ed2, _ := setupRemote(t, fs2, remotePath)
	port, every, notice := setupRemoteRecovery(ed2, configForRecovery(t), dir, "SSH:u@h")
	if notice == "" {
		t.Fatal("the restart did not report the recovered remote copy")
	}
	if !strings.Contains(notice, "SSH:u@h") {
		t.Fatalf("notice = %q, want the remote target", notice)
	}
	_ = every
	_ = port
	tabs := ed2.Tabs()
	if len(tabs) != 1 || tabs[0].Path != remotePath {
		t.Fatalf("recovered tabs = %+v, want the remote path", tabs)
	}
	if got := ed2.Active().Buffer.Text(); got != "// draft\npackage a\n" {
		t.Fatalf("recovered text = %q, want the staged unsaved text", got)
	}
	if !ed2.Active().Modified() {
		t.Fatal("the recovered remote document is not marked modified/unsaved")
	}
	// The recovery content never exists as a local file next to the process.
	if _, err := os.Stat(remotePath); !os.IsNotExist(err) {
		t.Fatalf("recovery wrote a local file at %q: %v", remotePath, err)
	}
}

// configForRecovery returns a config with recovery enabled (the zero config leaves it on
// by default; explicit here for clarity).
func configForRecovery(t *testing.T) (cfg config.Config) {
	t.Helper()
	return config.Config{}
}

var _ = editor.Options{}
