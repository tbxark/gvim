package app

// Local environment detector watching (#R13-13). The remote provider watches its detector
// marker/require paths through the authority; a LOCAL provider detects only at
// construction/SetDetectors, so a changed local marker (creating/removing .envrc, .nvmrc,
// .venv evidence, ...) would leave the Settings Environment rows and the detected
// environment stale until a Reload. This watcher closes that gap with the editor's local
// fsnotify watcher:
//   - bounded: the same env.DetectorWatchPaths plan as the remote watcher
//     (MaxDetectorWatchPaths/MaxDetectorWatchDirs), root + existing parent directories,
//   - cancellable: session context/Close stop the goroutine and the fsnotify watcher,
//   - non-blocking: events and the passive re-detection run on the watcher goroutine, never
//     on the UI update loop,
//   - clear degradation: if fsnotify cannot start (or the root cannot be watched), a
//     Settings Environment notice says detection changes need Env: Reload; no fallback to
//     polling the whole tree or to the remote watcher.
// It never reads remote paths and only runs for a local launch.

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/internal/filewatch"
)

// envFileWatch is the slice of filewatch.Watcher the local env watcher uses; tests inject a
// fake so the invalidation logic is exercised without fsnotify/flaky timers.
type envFileWatch interface {
	Add(dir string) error
	Remove(dir string) error
	Events() <-chan filewatch.Changed
	Close() error
}

type localEnvWatch struct {
	ctx      context.Context
	cancel   context.CancelFunc
	provider *env.Provider
	root     string
	watcher  envFileWatch

	plan atomic.Pointer[envWatchPlan]
	// replan is poked when env.detectors changes at runtime; the goroutine re-arms the
	// bounded watch set and re-detects with the new list.
	replan chan struct{}

	wg sync.WaitGroup

	mu     sync.Mutex
	added  map[string]bool
	closed bool
}

// startLocalEnvWatch starts the local detector watcher. It returns nil when fsnotify is
// unavailable; the provider then carries a clear degradation notice and detection changes
// wait for Env: Reload.
func startLocalEnvWatch(parent context.Context, provider *env.Provider, root string) *localEnvWatch {
	if provider == nil || root == "" {
		return nil
	}
	watcher, err := filewatch.New(filewatch.Options{Delay: remoteWatchDebounce})
	return startLocalEnvWatchWith(parent, provider, root, watcher, err)
}

func startLocalEnvWatchWith(parent context.Context, provider *env.Provider, root string, watcher envFileWatch, err error) *localEnvWatch {
	if provider == nil || root == "" {
		return nil
	}
	if err != nil {
		provider.SetWatchNotice(fmt.Sprintf("Local environment: file watching is unavailable (%v); changed detector markers are picked up on Env: Reload", err))
		return nil
	}
	if watcher == nil {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	lw := &localEnvWatch{
		ctx: ctx, cancel: cancel, provider: provider, root: root, watcher: watcher,
		replan: make(chan struct{}, 1), added: map[string]bool{},
	}
	lw.plan.Store(buildEnvWatchPlan(provider.Detectors()))
	if !lw.armDirs() {
		// The workspace root could not be watched: fsnotify is unusable for this launch.
		_ = watcher.Close()
		cancel()
		return nil
	}
	lw.wg.Add(1)
	go lw.run()
	return lw
}

// Replan applies a runtime env.detectors change to the local provider and re-arms the
// bounded watch set with the new list.
func (lw *localEnvWatch) Replan(detectors []config.EnvDetector) {
	if lw == nil {
		return
	}
	lw.provider.SetDetectors(detectors)
	lw.plan.Store(buildEnvWatchPlan(detectors))
	select {
	case lw.replan <- struct{}{}:
	default:
	}
}

// Close stops the watcher and waits for its goroutine.
func (lw *localEnvWatch) Close() {
	if lw == nil {
		return
	}
	lw.mu.Lock()
	if lw.closed {
		lw.mu.Unlock()
		return
	}
	lw.closed = true
	lw.mu.Unlock()
	lw.cancel()
	_ = lw.watcher.Close()
	lw.wg.Wait()
}

func (lw *localEnvWatch) run() {
	defer lw.wg.Done()
	for {
		select {
		case <-lw.ctx.Done():
			return
		case <-lw.replan:
			lw.armDirs()
			lw.provider.RefreshDetection()
		case changed, ok := <-lw.watcher.Events():
			if !ok {
				lw.provider.SetWatchNotice("Local environment: the file watcher ended; changed detector markers are picked up on Env: Reload")
				return
			}
			plan := lw.plan.Load()
			if plan == nil || !localEnvChangeRelevant(changed, plan) {
				continue
			}
			lw.provider.RefreshDetection()
			// A newly created nested marker parent directory needs a new filewatch.Add.
			lw.armDirs()
		}
	}
}

func localEnvChangeRelevant(changed filewatch.Changed, plan *envWatchPlan) bool {
	if len(changed.Files) > 0 {
		for _, file := range changed.Files {
			if plan.names[filepath.Base(file)] {
				return true
			}
		}
		return false
	}
	// An overflow reports every watched directory and no file: refresh conservatively.
	return len(changed.Dirs) > 0
}

// armDirs adds/removes the bounded watch set: the workspace root plus existing parent
// directories of the fixed env inputs and the detector markers/requires. Missing
// directories are skipped (a later root event re-arms after they appear).
func (lw *localEnvWatch) armDirs() bool {
	plan := lw.plan.Load()
	if plan == nil {
		return false
	}
	desired := map[string]bool{}
	rootAbs, err := filepath.Abs(lw.root)
	if err != nil {
		lw.provider.SetWatchNotice(fmt.Sprintf("Local environment: cannot resolve the workspace root (%v); changed detector markers are picked up on Env: Reload", err))
		return false
	}
	desired[rootAbs] = true
	addExisting := func(rel string) {
		abs, err := filepath.Abs(filepath.Join(lw.root, filepath.FromSlash(rel)))
		if err != nil {
			return
		}
		if st, err := os.Stat(abs); err == nil && st.IsDir() {
			desired[abs] = true
		}
	}
	for _, dir := range env.EnvInputNestedDirs() {
		addExisting(dir)
	}
	for dir := range plan.dirs {
		addExisting(dir)
	}

	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.closed {
		return false
	}
	failed := 0
	rootFailed := false
	for dir := range desired {
		if lw.added[dir] {
			continue
		}
		if err := lw.watcher.Add(dir); err != nil {
			failed++
			if dir == rootAbs {
				rootFailed = true
			}
			continue
		}
		lw.added[dir] = true
	}
	for dir := range lw.added {
		if !desired[dir] {
			_ = lw.watcher.Remove(dir)
			delete(lw.added, dir)
		}
	}
	switch {
	case rootFailed:
		lw.provider.SetWatchNotice("Local environment: the workspace root cannot be watched; changed detector markers are picked up on Env: Reload")
		return false
	case failed > 0:
		lw.provider.SetWatchNotice(fmt.Sprintf("Local environment: %d detector marker director(ies) cannot be watched; changed markers there are picked up on Env: Reload", failed))
	case plan.limitNotice != "":
		lw.provider.SetWatchNotice(plan.limitNotice)
	default:
		lw.provider.SetWatchNotice("")
	}
	return true
}

var _ = path.Clean
