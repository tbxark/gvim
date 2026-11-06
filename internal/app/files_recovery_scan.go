package app

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/tbxark/gvim/internal/editor"
	"github.com/tbxark/gvim/internal/session"
)

// ownRecoveryDir is the directory inside root where this process keeps its crash-recovery
// copy. Each running gvim has one of its own, so two runs that share a session directory do
// not replace each other's stash files.
func ownRecoveryDir(root string) string {
	return filepath.Join(root, strconv.Itoa(os.Getpid()))
}

// recoverAbandoned restores the unsaved text that earlier runs left in root, with
// recoverCrash. It looks at root itself, where earlier versions kept one copy, and at each
// directory named for a process id. A directory of a process that alive reports running is
// left alone, because that run is still editing. The directory of this process's own pid is
// recovered whatever alive says: the pid is ours again, so its owner is gone. A directory with
// nothing left in it is removed. It returns the files it could not restore and the number it
// did.
func recoverAbandoned(ed *editor.Editor, root string, alive func(pid int) bool) ([]unrestored, int) {
	failed, n := recoverCrash(ed, root)
	entries, err := os.ReadDir(root)
	if err != nil {
		return failed, n
	}
	var pids []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() && strconv.Itoa(pid) == e.Name() {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	for _, pid := range pids {
		if pid != os.Getpid() && alive(pid) {
			continue
		}
		dir := filepath.Join(root, strconv.Itoa(pid))
		f, k := recoverCrash(ed, dir)
		failed, n = append(failed, f...), n+k
		if s, ok, err := session.Load(dir); err == nil && (!ok || len(s.Files) == 0) {
			_ = os.RemoveAll(dir)
		}
	}
	return failed, n
}
