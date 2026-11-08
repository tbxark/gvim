package restore

import (
	"fmt"
	"time"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
)

// RecoveryRoot is the folder of the crash-recovery copies for a program run with session
// directory sessionDir, or "" when there is none. Only a run that starts the program
// (real is set) keeps a copy.
func RecoveryRoot(sessionDir string, real bool) string {
	if sessionDir == "" || !real {
		return ""
	}
	return RecoveryPath(sessionDir)
}

// RecoveryDirFor is the directory of this process's own crash-recovery copy, or "".
func RecoveryDirFor(sessionDir string, real bool) string {
	root := RecoveryRoot(sessionDir, real)
	if root == "" {
		return ""
	}
	return OwnRecoveryDir(root)
}

// RecoveryPort returns the writer of the crash-recovery copy and its interval, or nil
// when editor.recovery_enabled is off or there is no place to write it.
func RecoveryPort(ed *editor.Editor, cfg config.Config, sessionDir string, real bool) (tui.RecoveryPort, time.Duration) {
	dir := RecoveryDirFor(sessionDir, real)
	every := time.Duration(cfg.AutoRecoverySaveIntervalSecs) * time.Second
	if dir == "" || !cfg.RecoveryEnabled || every <= 0 {
		return nil, 0
	}
	return NewRecoveryWriter(ed, dir), every
}

// RecoverFromCrash restores the unsaved text that earlier runs left in the recovery
// folder of sessionDir, except the copy of a run that is still going, and returns the notice
// for the status row, or "" when there was nothing to recover. The copies it restored from
// are removed once port, when it is not nil, has written the copy of this run.
func RecoverFromCrash(ed *editor.Editor, sessionDir string, real bool, port tui.RecoveryPort) string {
	root := RecoveryRoot(sessionDir, real)
	if root == "" {
		return ""
	}
	var protect func() error
	if port != nil {
		protect = func() error { return port.Plan()() }
	}
	failed, n := recoverAbandoned(ed, root, processAlive, protect)
	switch {
	case len(failed) > 0:
		return fmt.Sprintf("recovery: %d file(s) restored; the unsaved text of %d more is kept in the .failed folders of %s", n, len(failed), root)
	case n > 0:
		return fmt.Sprintf("recovered unsaved text of %d file(s) after an unclean exit", n)
	}
	return ""
}
