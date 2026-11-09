package app

// Remote environment input-file watching (#R13-13). The remote provider's detection and
// captured delta are invalidated when a workspace env-input file changes, so a new
// .envrc/mise.toml is picked up without a manual Env: Reload. The watch is the authority's
// own file watch (a polled remote watcher), started off the UI update loop; there is no
// local fallback (os.Stat/fsnotify are never consulted for the remote root).
//
// Safe degradation: a backend/file surface without Watch capability (ErrUnsupportedFileIO)
// stops the watcher, records a Settings notice and leaves the manual Env: Reload path
// intact; any other watch failure is retried with the same bounded backoff as the remote
// document watch, and a successful re-arm clears the notice. Invalidating is cheap and
// asynchronous; the actual re-detection/capture runs in a warm goroutine, trust-gated by
// env.Remote exactly like any other activation.

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/plugin/hostos"
)

// envWatchPlan is the immutable, bounded watch plan derived from the provider's current
// detector set plus the fixed env-input files. Replanning swaps the pointer atomically and
// wakes the watcher, which re-arms; an in-flight arm keeps using its snapshot.
type envWatchPlan struct {
	names       map[string]bool
	rearmNames  map[string]bool
	dirs        map[string]map[string]bool
	limitNotice string
}

func buildEnvWatchPlan(detectors []config.EnvDetector) *envWatchPlan {
	names := make(map[string]bool)
	for _, name := range env.EnvInputRootNames() {
		names[name] = true
	}
	rearm := make(map[string]bool)
	for _, dir := range env.EnvInputNestedDirs() {
		rearm[dir] = true
	}
	spec := env.DetectorWatchPaths(detectors)
	for name := range spec.RootNames {
		names[name] = true
	}
	for _, children := range spec.Dirs {
		for child := range children {
			names[child] = true
		}
	}
	for name := range spec.RearmNames {
		rearm[name] = true
	}
	limitNotice := ""
	if spec.Omitted > 0 || spec.OmittedDirs > 0 {
		limitNotice = fmt.Sprintf("Remote environment: %d detector marker path(s) in %d director(ies) are not watched (watch bound reached); use Env: Reload after changing them", spec.Omitted, spec.OmittedDirs)
	}
	return &envWatchPlan{names: names, rearmNames: rearm, dirs: spec.Dirs, limitNotice: limitNotice}
}

type remoteEnvWatch struct {
	ctx      context.Context
	cancel   context.CancelFunc
	slot     authority.Slot
	provider *env.Remote
	root     string
	plan     atomic.Pointer[envWatchPlan]
	// replan is poked when the detector config changes at runtime; the run loop stops its
	// current watches and re-arms against the new bounded plan (no local read).
	replan chan struct{}

	wg sync.WaitGroup

	mu         sync.Mutex
	closed     bool
	refreshing bool
	refreshWG  sync.WaitGroup
}

// startRemoteEnvWatch starts the env-input watcher for a remote session. It returns nil for
// a missing provider/slot or an empty root (nothing to watch). The caller may ignore the
// result: the session context cancels it, and Close is available to tests.
func startRemoteEnvWatch(parent context.Context, slot authority.Slot, provider *env.Remote, root string) *remoteEnvWatch {
	if slot == nil || provider == nil {
		return nil
	}
	if root == "" {
		root = provider.Root()
	}
	if root == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(parent)
	w := &remoteEnvWatch{ctx: ctx, cancel: cancel, slot: slot, provider: provider, root: root, replan: make(chan struct{}, 1)}
	w.plan.Store(buildEnvWatchPlan(provider.Detectors()))
	w.wg.Add(1)
	go w.run()
	return w
}

// Close stops the watcher and waits for its goroutines (the normal path is session-context
// cancellation).
func (w *remoteEnvWatch) Close() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	w.closed = true
	w.mu.Unlock()
	w.cancel()
	w.wg.Wait()
	w.refreshWG.Wait()
}

// Replan applies a runtime detector-config change: the provider gets the new detectors
// (invalidating detection/capture so the next Warm re-probes) and the watcher re-arms its
// bounded watch set from a fresh plan. It never reads the local filesystem and the
// existing session context still bounds every probe/watch. Safe to call when watching is
// unavailable: the old degradation notice remains until a watch can be armed.
func (w *remoteEnvWatch) Replan(detectors []config.EnvDetector) {
	if w == nil {
		return
	}
	w.provider.SetDetectors(detectors)
	w.plan.Store(buildEnvWatchPlan(detectors))
	// Re-detect/re-capture with the new detector set in the background; the watcher
	// re-arm itself is only about matching future changes.
	w.refresh()
	select {
	case w.replan <- struct{}{}:
	default:
	}
}

func (w *remoteEnvWatch) run() {
	defer w.wg.Done()
	backoff := remoteWatchRetryMin
	for w.ctx.Err() == nil {
		armCtx, armCancel := context.WithCancel(w.ctx)
		targets := w.targets(armCtx)
		fs := w.slot.Files()
		var watches []hostos.Watch
		var armErr error
		for _, target := range targets {
			watch, err := fs.Watch(armCtx, target, hostos.WatchOptions{Debounce: remoteWatchDebounce})
			if err != nil {
				armErr = err
				break
			}
			watches = append(watches, watch)
		}
		if armErr != nil {
			for _, watch := range watches {
				watch.Stop()
			}
			armCancel()
			if errors.Is(armErr, authority.ErrUnsupportedFileIO) {
				w.provider.SetWatchNotice(appText("gvim.notice.remote_env_watch_unsupported", "Remote environment: this authority cannot watch files, so changed .envrc/mise.toml files are picked up on Env: Reload (or the next reconnect)"))
				return
			}
			w.provider.SetWatchNotice(appTextVars("gvim.notice.remote_env_watch_failed", "Remote environment: could not watch the env input files (%{error}); retrying", map[string]any{"error": armErr}))
			if !sleepContext(w.ctx, backoff) {
				return
			}
			backoff *= 2
			if backoff > remoteWatchRetryMax {
				backoff = remoteWatchRetryMax
			}
			continue
		}
		backoff = remoteWatchRetryMin
		if plan := w.plan.Load(); plan != nil && plan.limitNotice != "" {
			w.provider.SetWatchNotice(plan.limitNotice)
		} else {
			w.provider.SetWatchNotice("")
		}

		events := make(chan hostos.WatchEvent, 64)
		var pumps sync.WaitGroup
		for _, watch := range watches {
			pumps.Add(1)
			go func(watch hostos.Watch) {
				defer pumps.Done()
				for {
					event, ok := watch.Next(armCtx)
					if !ok {
						return
					}
					select {
					case events <- event:
					case <-armCtx.Done():
						return
					}
				}
			}(watch)
		}

		rearm := false
		for !rearm {
			plan := w.plan.Load()
			select {
			case <-w.replan:
				rearm = true
				continue
			case <-w.ctx.Done():
				armCancel()
				for _, watch := range watches {
					watch.Stop()
				}
				pumps.Wait()
				return
			case event := <-events:
				if event.Kind == "error" {
					// The poll had a transient failure; the watcher keeps running and the
					// next successful snapshot reports any real change.
					continue
				}
				if plan == nil {
					continue
				}
				name := path.Base(event.Path)
				if !plan.names[name] {
					continue
				}
				w.refresh()
				// A nested input's parent (".venv") may have just appeared: re-arm so the
				// new directory is watched for content-only changes the root cannot see.
				if plan.rearmNames[name] {
					rearm = true
				}
			}
		}
		armCancel()
		for _, watch := range watches {
			watch.Stop()
		}
		pumps.Wait()
		if w.ctx.Err() != nil {
			return
		}
		// Let a just-created directory settle before re-arming; the event already
		// invalidated the environment.
		if !sleepContext(w.ctx, 100*time.Millisecond) {
			return
		}
	}
}

// targets is the root plus every existing nested env-input directory (".venv"), so a
// content change to .venv/pyvenv.cfg is visible even though it does not change the .venv
// directory entry the root watch snapshots.
func (w *remoteEnvWatch) targets(ctx context.Context) []string {
	plan := w.plan.Load()
	targets := []string{w.root}
	files := w.slot.Files()
	seen := map[string]bool{}
	addDir := func(dir string) {
		dir = path.Clean(dir)
		if dir == "" || dir == "." || dir == "/" || seen[dir] {
			return
		}
		candidate := path.Join(w.root, dir)
		if st, err := files.Stat(ctx, candidate); err == nil && st.Kind == "dir" {
			seen[dir] = true
			targets = append(targets, candidate)
		}
	}
	for _, dir := range env.EnvInputNestedDirs() {
		addDir(dir)
	}
	if plan == nil {
		return targets
	}
	dirs := make([]string, 0, len(plan.dirs))
	for dir := range plan.dirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		addDir(dir)
	}
	return targets
}

// refresh invalidates the provider and warms the new detection/capture in one background
// goroutine; a burst of events collapses into one refresh. Overlay reads keep serving the
// last captured delta while this runs, so no UI path blocks.
func (w *remoteEnvWatch) refresh() {
	w.mu.Lock()
	if w.closed || w.refreshing {
		w.mu.Unlock()
		return
	}
	w.refreshing = true
	w.refreshWG.Add(1)
	w.mu.Unlock()
	go func() {
		defer w.refreshWG.Done()
		defer func() {
			w.mu.Lock()
			w.refreshing = false
			w.mu.Unlock()
		}()
		w.provider.Refresh()
		w.provider.Warm(w.ctx)
	}()
}

func sleepContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
