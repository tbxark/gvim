package app

// Remote workspace hot-exit / crash recovery (task-143). A remote project has no local
// session directory to reuse, so its unsaved copies are staged under a local directory
// keyed by the remote target: <cache>/gvim/remote-sessions/<hash>/recovery. The staged
// session records the REMOTE paths (session.FileState.Path is the remote path the editor
// opened), so a restart with the same target detects the copy and reopens those paths
// through the authority file access — the text comes from the local staging copy, the
// files are read through the remote backend. It is a deliberate local-staging divergence
// from Fresh (which routes recovery through the active authority); no local file the
// workspace names is ever written or read as its content.
//
// The writer is the ordinary RecoveryWriter (bounded like local recovery: only open
// unsaved documents, one stash per document, the existing interval), driven from the root's
// recovery command, so no update-loop I/O is added. A clean exit clears the staging copy
// through the runtime's recovery directory, exactly like a local run.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/restore"
	"github.com/tbxark/gvim/internal/tui"
)

// remoteRecoveryBaseDir is the local root of the remote staging sessions. Tests set
// GVIM_REMOTE_SESSION_DIR; the default is the user cache so a remote workspace does not
// mix its copies with the local session data.
func remoteRecoveryBaseDir() string {
	if dir := os.Getenv("GVIM_REMOTE_SESSION_DIR"); dir != "" {
		return dir
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(cache, "gvim", "remote-sessions")
}

// remoteRecoverySessionDir is the local staging session directory of spec: deterministic
// per remote target, so the next launch with the same target finds the abandoned copies.
// It is the session directory the recovery writer/reader and the runtime cleanup use.
func remoteRecoverySessionDir(spec *authority.RemoteSpec) string {
	base := remoteRecoveryBaseDir()
	if base == "" || spec == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(remoteTrustTarget(spec)))
	return filepath.Join(base, hex.EncodeToString(sum[:8]), "session")
}

// setupRemoteRecovery stages the unsaved text of the open (remote) documents for a crash
// and recovers copies abandoned by an unclean exit. It must run after the remote file
// access is installed (setupRemoteEditing), so RecoverFromCrash opens the recorded remote
// paths through the authority, never the local disk. It returns the recovery writer, its
// interval and a notice for the status row; all empty when there is no session directory.
func setupRemoteRecovery(ed *editor.Editor, cfg config.Config, sessionDir, target string) (tui.RecoveryPort, time.Duration, string) {
	if sessionDir == "" || ed == nil {
		return nil, 0, ""
	}
	port, every := restore.RecoveryPort(ed, cfg, sessionDir, true)
	notice := restore.RecoverFromCrash(ed, sessionDir, true, port)
	if notice != "" {
		notice = appTextVars("gvim.notice.remote_workspace_restore", "remote workspace %{target}: %{notice}", map[string]any{"target": target, "notice": notice})
	}
	return port, every, notice
}
