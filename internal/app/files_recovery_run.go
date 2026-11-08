package app

import (
	"fmt"
	"time"

	"github.com/tbxark/gvim/internal/config"
	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/ui"
)

// recoveryRootFor is the folder of the crash-recovery copies for a program run with session
// directory sessionDir, or "" when there is none. Only a run that starts the program
// (real is set) keeps a copy.
func recoveryRootFor(sessionDir string, real bool) string {
	if sessionDir == "" || !real {
		return ""
	}
	return recoveryDir(sessionDir)
}

// recoveryDirFor is the directory of this process's own crash-recovery copy, or "".
func recoveryDirFor(sessionDir string, real bool) string {
	root := recoveryRootFor(sessionDir, real)
	if root == "" {
		return ""
	}
	return ownRecoveryDir(root)
}

// recoveryPort returns the writer of the crash-recovery copy and its interval, or nil
// when editor.recovery_enabled is off or there is no place to write it.
func recoveryPort(ed *editor.Editor, cfg config.Config, sessionDir string, real bool) (ui.RecoveryPort, time.Duration) {
	dir := recoveryDirFor(sessionDir, real)
	every := time.Duration(cfg.AutoRecoverySaveIntervalSecs) * time.Second
	if dir == "" || !cfg.RecoveryEnabled || every <= 0 {
		return nil, 0
	}
	return newRecoveryWriter(ed, dir), every
}

// recoverFromCrash restores the unsaved text that earlier runs left in the recovery
// folder of sessionDir, except the copy of a run that is still going, and returns the notice
// for the status row, or "" when there was nothing to recover. The copies it restored from
// are removed once port, when it is not nil, has written the copy of this run.
func recoverFromCrash(ed *editor.Editor, sessionDir string, real bool, port ui.RecoveryPort) string {
	root := recoveryRootFor(sessionDir, real)
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
