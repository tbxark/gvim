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
	"os"
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
	// closeWatch makes watcher/fsnotify cleanup idempotent across the startup-failure
	// path, Close, and the goroutine exiting on session cancellation.
	closeWatch sync.Once
	// dead is true once the watcher resources were released (natural end, startup failure
	// or Close); Env: Reload may then start a fresh watcher.
	dead atomic.Bool

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
	// Ownership of watcher transfers to this constructor: every early return must release
	// a started fsnotify watcher instead of leaking its goroutine/descriptors.
	closeWatcher := func() {
		if watcher != nil {
			_ = watcher.Close()
		}
	}
	if provider == nil {
		closeWatcher()
		return nil
	}
	if root == "" {
		closeWatcher()
		provider.SetWatchNotice(appTextVars("gvim.notice.local_env_watch_root_failed", "Local environment: cannot resolve the workspace root (%{error}); changed detector markers are picked up on Env: Reload", map[string]any{"error": "no workspace root"}))
		return nil
	}
	if err != nil {
		closeWatcher()
		provider.SetWatchNotice(appTextVars("gvim.notice.local_env_watch_unavailable", "Local environment: file watching is unavailable (%{error}); changed detector markers are picked up on Env: Reload", map[string]any{"error": err}))
		return nil
	}
	if watcher == nil {
		provider.SetWatchNotice(appTextVars("gvim.notice.local_env_watch_unavailable", "Local environment: file watching is unavailable (%{error}); changed detector markers are picked up on Env: Reload", map[string]any{"error": "the file watcher is unavailable"}))
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	lw := &localEnvWatch{
		ctx: ctx, cancel: cancel, provider: provider, root: root, watcher: watcher,
		replan: make(chan struct{}, 1), added: map[string]bool{},
	}
	lw.plan.Store(buildEnvWatchPlan(provider.Detectors()))
	if !lw.armDirs() {
		// The workspace root could not be watched: fsnotify is unusable for this launch,
		// but Env: Reload may retry once the root becomes watchable.
		lw.provider.SetWatchRetryNeeded(true)
		lw.closeResources()
		cancel()
		return nil
	}
	lw.provider.RefreshDetection()
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
	lw.closeResources()
	lw.wg.Wait()
}

// closeResources releases the file watcher exactly once, whether startup failed, the
// session context ended, or Close was called.
func (lw *localEnvWatch) closeResources() {
	lw.closeWatch.Do(func() {
		lw.dead.Store(true)
		if lw.watcher != nil {
			_ = lw.watcher.Close()
		}
	})
}

// Dead reports whether the watcher's resources were released and a retry may start a new
// watcher (Env: Reload after a root-Add failure recovers this way).
func (lw *localEnvWatch) Dead() bool { return lw == nil || lw.dead.Load() }

// localEnvWatchRetry owns the replace-on-demand lifecycle of the local detector watcher.
// The first start can fail when the workspace root is not watchable yet (fsnotify Add
// fails); the provider keeps the Env: Reload notice. Env: Reload (an env-manager set/clear
// action) calls Retry, which starts a fresh bounded watcher off the UI goroutine, closes a
// dead predecessor once, and installs the new watcher for runtime detector re-plans. A
// live watcher is left alone, so ordinary activations do not churn fsnotify.
type localEnvWatchRetry struct {
	mu      sync.Mutex
	ctx     context.Context
	current *localEnvWatch
	start   func() *localEnvWatch
	install func(*localEnvWatch)
}

// Current returns the installed watcher (tests/status); nil before the first success.
func (r *localEnvWatchRetry) Current() *localEnvWatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

// SetInstall sets the callback that records the recovered watcher (settings service). It
// may be called after construction, so startup does not need an unsynchronized settings
// pointer read from the retry goroutine; the callback is installed under the same mutex
// Retry uses and is applied to an already-recovered watcher.
func (r *localEnvWatchRetry) SetInstall(install func(*localEnvWatch)) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.install = install
	cur := r.current
	r.mu.Unlock()
	if cur != nil && install != nil {
		install(cur)
	}
}

func (r *localEnvWatchRetry) Retry() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx != nil && r.ctx.Err() != nil {
		return
	}
	if r.current != nil && !r.current.Dead() {
		return
	}
	lw := r.start()
	if lw == nil {
		return
	}
	old := r.current
	r.current = lw
	if old != nil {
		old.Close()
	}
	if r.install != nil {
		r.install(lw)
	}
}

func (lw *localEnvWatch) run() {
	defer lw.wg.Done()
	// Session cancellation ends the goroutine; no caller necessarily calls Close in
	// production, so release the fsnotify watcher here (idempotent with Close).
	defer lw.closeResources()
	for {
		select {
		case <-lw.ctx.Done():
			return
		case <-lw.replan:
			lw.armDirs()
			lw.provider.RefreshDetection()
		case changed, ok := <-lw.watcher.Events():
			if !ok {
				lw.provider.SetWatchNotice(appText("gvim.notice.local_env_watch_ended", "Local environment: the file watcher ended; changed detector markers are picked up on Env: Reload"))
				lw.provider.SetWatchRetryNeeded(true)
				return
			}
			plan := lw.plan.Load()
			if plan == nil || !localEnvChangeRelevant(changed, plan) {
				continue
			}
			// Arm first: a newly created nested marker parent directory must be added before
			// the re-detection, otherwise a file created in the gap between the refresh and
			// the Add could be missed by both the root event and the (late) nested watch.
			lw.armDirs()
			lw.provider.RefreshDetection()
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
		lw.provider.SetWatchNotice(appTextVars("gvim.notice.local_env_watch_root_failed", "Local environment: cannot resolve the workspace root (%{error}); changed detector markers are picked up on Env: Reload", map[string]any{"error": err}))
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
		lw.provider.SetWatchNotice(appText("gvim.notice.local_env_watch_root_unwatchable", "Local environment: the workspace root cannot be watched; changed detector markers are picked up on Env: Reload"))
		lw.provider.SetWatchRetryNeeded(true)
		return false
	case failed > 0:
		lw.provider.SetWatchNotice(appTextVars("gvim.notice.local_env_watch_dirs", "Local environment: %{count} detector marker director(ies) cannot be watched; changed markers there are picked up on Env: Reload", map[string]any{"count": failed}))
		// The root itself is watched; only optional parent dirs are missing, so an Env:
		// Reload does not need to replace the watcher.
		lw.provider.SetWatchRetryNeeded(false)
	case plan.omitted > 0 || plan.omittedDirs > 0:
		lw.provider.SetWatchNotice(localEnvWatchLimitNotice(plan))
	default:
		lw.provider.SetWatchNotice("")
		lw.provider.SetWatchRetryNeeded(false)
	}
	return true
}
