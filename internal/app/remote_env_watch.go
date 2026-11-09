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
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/env"
	"github.com/tbxark/gvim/plugin/hostos"
)

type remoteEnvWatch struct {
	ctx         context.Context
	cancel      context.CancelFunc
	slot        authority.Slot
	provider    *env.Remote
	root        string
	names       map[string]bool
	nestedNames map[string]bool

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
	names := make(map[string]bool)
	for _, name := range env.EnvInputRootNames() {
		names[name] = true
	}
	nested := make(map[string]bool)
	for _, dir := range env.EnvInputNestedDirs() {
		nested[dir] = true
	}
	ctx, cancel := context.WithCancel(parent)
	w := &remoteEnvWatch{ctx: ctx, cancel: cancel, slot: slot, provider: provider, root: root, names: names, nestedNames: nested}
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
				w.provider.SetWatchNotice("Remote environment: this authority cannot watch files, so changed .envrc/mise.toml files are picked up on Env: Reload (or the next reconnect)")
				return
			}
			w.provider.SetWatchNotice(fmt.Sprintf("Remote environment: could not watch the env input files (%v); retrying", armErr))
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
		w.provider.SetWatchNotice("")

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
			select {
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
				name := path.Base(event.Path)
				if !w.names[name] {
					continue
				}
				w.refresh()
				// A nested input's parent (".venv") may have just appeared: re-arm so the
				// new directory is watched for content-only changes the root cannot see.
				if w.nestedNames[name] {
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
	targets := []string{w.root}
	files := w.slot.Files()
	for _, dir := range env.EnvInputNestedDirs() {
		candidate := path.Join(w.root, dir)
		if st, err := files.Stat(ctx, candidate); err == nil && st.Kind == "dir" {
			targets = append(targets, candidate)
		}
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
