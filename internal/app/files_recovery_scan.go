package app

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/session"
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
//
// The restored text lives only in memory until this run writes a copy of its own, so the
// copies it restored from are removed after protect, which writes that copy, has worked.
// When protect fails they stay, and the next start restores them again. protect may be nil.
// The leftover of this process's own pid is the exception: protect writes into it, so it
// is cleared before.
func recoverAbandoned(ed *editor.Editor, root string, alive func(pid int) bool, protect func() error) ([]unrestored, int) {
	failed, n, spent := restoreCrash(ed, root)
	spentDirs := map[string]bool{}
	if spent {
		spentDirs[root] = true
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		finishRecovery(spentDirs, protect)
		return failed, n
	}
	var pids []int
	for _, e := range entries {
		if pid, err := strconv.Atoi(e.Name()); err == nil && e.IsDir() && strconv.Itoa(pid) == e.Name() {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	var dirs []string
	for _, pid := range pids {
		if pid != os.Getpid() && alive(pid) {
			continue
		}
		dir := filepath.Join(root, strconv.Itoa(pid))
		dirs = append(dirs, dir)
		f, k, done := restoreCrash(ed, dir)
		failed, n = append(failed, f...), n+k
		if done {
			if pid == os.Getpid() {
				_ = clearRecovery(dir)
			} else {
				spentDirs[dir] = true
			}
		}
	}
	finishRecovery(spentDirs, protect)
	for _, dir := range dirs {
		if s, ok, err := session.Load(dir); err == nil && (!ok || len(s.Files) == 0) {
			_ = os.RemoveAll(dir)
		}
	}
	return failed, n
}

// finishRecovery removes the copies in spent after protect has written the copy of this
// run. A failing protect keeps them.
func finishRecovery(spent map[string]bool, protect func() error) {
	if len(spent) == 0 {
		return
	}
	if protect != nil && protect() != nil {
		return
	}
	for dir := range spent {
		_ = clearRecovery(dir)
	}
}
