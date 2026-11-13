package app

import (
	"context"
	"path"
	"sort"
	"sync"
	"time"

	"github.com/tbxark/gvim/authority"
	"github.com/tbxark/gvim/internal/filewatch"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/plugin/hostos"
)

// Remote external-change watching (docs/authority.md): a remote workspace's open
// documents are watched on the remote host through the authority file surface
// (Files().Watch, a polled remote watcher), and the events are bridged into the root's
// existing watch pipeline (RootPorts.Watch/RootPorts.WatchDirs) so the tree reload and
// auto-revert work exactly as for a local file. There is no local filesystem fallback:
// the adapter never uses filewatch, fsnotify or os.Stat for the remote paths.
//
// The bridge is asynchronous: each watch is a goroutine with its own cancellable context,
// the events are coalesced off the update goroutine, and the adapter is closed with the
// runtime.
const (
	// remoteWatchDebounce is the quiet window used both for the authority's own watch
	// debounce and for coalescing events into one filewatch.Changed.
	remoteWatchDebounce = 150 * time.Millisecond
	// remoteWatchRetryMin and remoteWatchRetryMax bound the backoff after a watch fails
	// or ends while the authority still reports connected.
	remoteWatchRetryMin = time.Second
	remoteWatchRetryMax = 10 * time.Second
	// remoteWatchCheckEvery is how often a waiting supervisor checks Connected(), so a
	// missed reconnect signal cannot wedge it.
	remoteWatchCheckEvery = 250 * time.Millisecond
)

// remoteWatchCancel is the cancel record of one directory's watch. The pointer identity
// lets a watch that is ending remove only its own record, never a newer watch's.
type remoteWatchCancel struct {
	cancel context.CancelFunc
}

// remoteWatchUpdate is one raw remote change on its way to the coalescer.
type remoteWatchUpdate struct {
	gen  uint64
	dir  string
	file string
}

// remoteWatch bridges authority remote watches into the root's watch channel. All
// methods are safe for concurrent use.
type remoteWatch struct {
	ctx    context.Context
	cancel context.CancelFunc
	slot   authority.Slot

	events  chan filewatch.Changed
	updates chan remoteWatchUpdate

	mu        sync.Mutex
	dirs      map[string]bool
	cancels   map[string]*remoteWatchCancel
	gen       uint64
	closed    bool
	wg        sync.WaitGroup
	reconnect chan struct{}
	closeOnce sync.Once
}

// newRemoteWatch starts the bridge for slot under parent. Nothing is watched until a
// directory is added (Add), and Close ends every watch and closes Events.
func newRemoteWatch(parent context.Context, slot authority.Slot) *remoteWatch {
	ctx, cancel := context.WithCancel(parent)
	w := &remoteWatch{
		ctx:       ctx,
		cancel:    cancel,
		slot:      slot,
		events:    make(chan filewatch.Changed, 1),
		updates:   make(chan remoteWatchUpdate, 128),
		dirs:      make(map[string]bool),
		cancels:   make(map[string]*remoteWatchCancel),
		reconnect: make(chan struct{}, 1),
	}
	w.wg.Add(1)
	go w.collect()
	return w
}

// setupRemoteWatch creates the bridge and wires it into the root
// ports for a remote launch. It returns nil for a local launch (spec nil) or a missing
// slot, so the local file watcher wiring stays untouched.
func setupRemoteWatch(ctx context.Context, deps *tui.Deps, slot authority.Slot, spec *authority.RemoteSpec) *remoteWatch {
	if deps == nil || slot == nil || spec == nil {
		return nil
	}
	w := newRemoteWatch(ctx, slot)
	deps.Root.Watch = w.Events()
	deps.Root.WatchDirs = w
	return w
}

// Events returns the channel the root waits on. It is closed by Close.
func (w *remoteWatch) Events() <-chan filewatch.Changed {
	if w == nil {
		return nil
	}
	return w.events
}

// Add implements the root's WatchDirs port: the directory of an open document is watched
// on the remote host. Adding the same directory twice is harmless, and an unwatchable
// directory is retried later rather than failing the caller (the authority reports watch
// errors asynchronously).
func (w *remoteWatch) Add(dir string) error {
	if w == nil || w.slot == nil || dir == "" {
		return nil
	}
	w.mu.Lock()
	if w.closed || w.dirs[dir] {
		w.mu.Unlock()
		return nil
	}
	w.dirs[dir] = true
	// Counted under the lock Close takes, so Close cannot start waiting between the check
	// above and this Add.
	w.wg.Add(1)
	w.mu.Unlock()
	go w.supervise(dir)
	return nil
}

// Reconnected restarts every watch after the authority reconnected: it bumps the
// generation (so events from the old carrier are dropped instead of being applied to the
// new state), cancels the current watches, and wakes the supervisors to start new ones for
// the desired directories.
func (w *remoteWatch) Reconnected() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.gen++
	cancels := make([]*remoteWatchCancel, 0, len(w.cancels))
	for _, entry := range w.cancels {
		cancels = append(cancels, entry)
	}
	w.cancels = make(map[string]*remoteWatchCancel)
	w.mu.Unlock()
	for _, entry := range cancels {
		entry.cancel()
	}
	select {
	case w.reconnect <- struct{}{}:
	default:
	}
}

// Close ends every watch, waits for the goroutines and closes the events channel. The
// root's watch loop stops on the closed channel. Close is idempotent.
func (w *remoteWatch) Close() error {
	if w == nil {
		return nil
	}
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		cancels := make([]*remoteWatchCancel, 0, len(w.cancels))
		for _, entry := range w.cancels {
			cancels = append(cancels, entry)
		}
		w.cancels = make(map[string]*remoteWatchCancel)
		w.mu.Unlock()
		for _, entry := range cancels {
			entry.cancel()
		}
		w.cancel()
		select {
		case w.reconnect <- struct{}{}:
		default:
		}
		w.wg.Wait()
		close(w.events)
	})
	return nil
}

// currentGen returns the generation a new watch must carry. Events from another
// generation are stale.
func (w *remoteWatch) currentGen() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.gen
}

// supervise runs one directory's watch for as long as the directory is desired and the
// bridge is open. A dropped carrier, a failed start and a watch that ends are all retried
// after a bounded backoff or after the next reconnect.
func (w *remoteWatch) supervise(dir string) {
	defer w.wg.Done()
	backoff := remoteWatchRetryMin
	for {
		if w.ctx.Err() != nil {
			return
		}
		if w.slot == nil || !w.slot.Connected() {
			if !w.waitConnected() {
				return
			}
			continue
		}
		gen := w.currentGen()
		ctx, cancel := context.WithCancel(w.ctx)
		w.mu.Lock()
		if w.closed || !w.dirs[dir] {
			w.mu.Unlock()
			cancel()
			return
		}
		entry := &remoteWatchCancel{cancel: cancel}
		w.cancels[dir] = entry
		w.mu.Unlock()
		watch, err := w.slot.Files().Watch(ctx, dir, hostos.WatchOptions{Debounce: remoteWatchDebounce})
		if err != nil {
			cancel()
			w.removeCancel(dir, entry)
			if !w.retryWait(backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}
		backoff = remoteWatchRetryMin
		w.drain(ctx, gen, watch)
		watch.Stop()
		cancel()
		w.removeCancel(dir, entry)
		if w.ctx.Err() != nil {
			return
		}
		if !w.slot.Connected() {
			if !w.waitConnected() {
				return
			}
			continue
		}
		if !w.retryWait(remoteWatchRetryMin) {
			return
		}
	}
}

// removeCancel drops the cancel record of a watch that ended, unless a newer watch for the
// same directory already replaced it. The pointer identity makes that check exact.
func (w *remoteWatch) removeCancel(dir string, entry *remoteWatchCancel) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancels[dir] == entry {
		delete(w.cancels, dir)
	}
}

// drain forwards one watch's events until the watch ends. Error events are not file
// changes: they mean "try again", and the supervisor does that after the watch ends.
func (w *remoteWatch) drain(ctx context.Context, gen uint64, watch hostos.Watch) {
	for {
		ev, ok := watch.Next(ctx)
		if !ok {
			return
		}
		if ev.Kind == "error" || ev.Path == "" {
			continue
		}
		if !w.forward(gen, ev) {
			return
		}
	}
}

// forward sends one raw event to the coalescer. A stale event (an older generation) is
// dropped: it belongs to a carrier that is gone.
func (w *remoteWatch) forward(gen uint64, ev hostos.WatchEvent) bool {
	if gen != w.currentGen() {
		return true
	}
	update := remoteWatchUpdate{gen: gen, dir: path.Dir(ev.Path), file: ev.Path}
	select {
	case w.updates <- update:
		return true
	case <-w.ctx.Done():
		return false
	}
}

// collect coalesces raw events into filewatch.Changed values, one per quiet window. It is
// the only sender on Events; Close closes the channel only after this goroutine returned.
func (w *remoteWatch) collect() {
	defer w.wg.Done()
	var dirs, files map[string]bool
	var gen uint64
	var timer *time.Timer
	var timerC <-chan time.Time

	reset := func() {
		dirs, files, gen = nil, nil, 0
		if timer != nil {
			timer.Stop()
			timer, timerC = nil, nil
		}
	}
	for {
		select {
		case <-w.ctx.Done():
			return
		case update := <-w.updates:
			if update.gen != w.currentGen() {
				continue
			}
			if dirs != nil && update.gen != gen {
				// The pending batch belongs to a carrier that is gone; this update starts
				// the new one.
				reset()
			}
			if dirs == nil {
				dirs, files, gen = make(map[string]bool), make(map[string]bool), update.gen
			}
			dirs[update.dir], files[update.file] = true, true
			if timer == nil {
				timer = time.NewTimer(remoteWatchDebounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(remoteWatchDebounce)
			}
			timerC = timer.C
		case <-timerC:
			timer, timerC = nil, nil
			if len(dirs) == 0 {
				reset()
				continue
			}
			if gen != w.currentGen() {
				reset()
				continue
			}
			changed := filewatch.Changed{Dirs: sortedKeys(dirs), Files: sortedKeys(files)}
			select {
			case w.events <- changed:
			case <-w.ctx.Done():
				return
			}
			reset()
		}
	}
}

// waitConnected blocks until the slot reports connected, or the adapter is closed. A
// reconnect signal wakes it early; a periodic check covers a missed signal.
func (w *remoteWatch) waitConnected() bool {
	ticker := time.NewTicker(remoteWatchCheckEvery)
	defer ticker.Stop()
	for {
		if w.slot != nil && w.slot.Connected() {
			return true
		}
		select {
		case <-w.ctx.Done():
			return false
		case <-w.reconnect:
		case <-ticker.C:
		}
	}
}

// retryWait waits for the backoff or the next reconnect; it reports false when the adapter
// closed.
func (w *remoteWatch) retryWait(d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-w.ctx.Done():
		return false
	case <-w.reconnect:
		return true
	case <-timer.C:
		return true
	}
}

// nextBackoff doubles the retry wait up to the cap.
func nextBackoff(d time.Duration) time.Duration {
	d *= 2
	if d > remoteWatchRetryMax {
		return remoteWatchRetryMax
	}
	return d
}

// sortedKeys returns the keys of m sorted, so a Changed value is deterministic.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
