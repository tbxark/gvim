package pluginhost

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/tbxark/gvim/plugin/hostos"
	"github.com/tbxark/gvim/pluginapi"
)

// Watchers of the plugin OS. A watcher is fsnotify, non-recursive: a
// directory reports changes of its entries, a file reports changes of itself. Events are
// coalesced per path over a debounce window before they reach the plugin.

const (
	defaultWatchDebounce = 50 * time.Millisecond
	watchQueueMax        = 4096
)

// osWatch is a running watcher.
type osWatch struct {
	os       *pluginOS
	fsw      *fsnotify.Watcher
	path     string // the watched path
	file     string // non-empty: only events of this path (the parent directory is watched)
	debounce time.Duration
	// entries estimates the entries of a watched directory where kqueue holds a descriptor
	// for each (kqueueDirLimit > 0; -1 elsewhere and for a file). Only run touches it.
	entries int

	mu       sync.Mutex
	pending  map[string]*pendingEvent
	queue    []hostos.WatchEvent
	overflow bool
	stopped  bool
	wake     chan struct{} // 1-buffered: something is in the queue or the watcher stopped
}

type pendingEvent struct {
	kind  string
	timer *time.Timer
}

var _ hostos.Watch = (*osWatch)(nil)

// Watch implements hostos.OS.
func (o *pluginOS) Watch(ctx context.Context, path string, wo hostos.WatchOptions) (hostos.Watch, error) {
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	p := o.abs(path)
	st, err := os.Stat(p)
	if err != nil {
		return nil, mapErr(err, p)
	}
	entries := -1
	if st.IsDir() && kqueueDirLimit > 0 {
		ents, err := os.ReadDir(p)
		if err != nil {
			return nil, mapErr(err, p)
		}
		if len(ents) > kqueueDirLimit {
			return nil, pluginapi.Errf(pluginapi.CodeLimit, "directory too large to watch; watch a file or a subdirectory")
		}
		entries = len(ents)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, mapErr(err, "")
	}
	w := &osWatch{os: o, fsw: fsw, path: p, entries: entries, debounce: wo.Debounce, pending: map[string]*pendingEvent{}, wake: make(chan struct{}, 1)}
	if w.debounce <= 0 {
		w.debounce = defaultWatchDebounce
	}
	target := p
	if !st.IsDir() && kqueueDirLimit == 0 {
		// A file is watched through its directory, so replacing it (the way editors save)
		// does not end the watch with the old inode.
		target, w.file = filepath.Dir(p), p
	}
	if err := fsw.Add(target); err != nil {
		_ = fsw.Close()
		return nil, mapErr(err, p)
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		_ = fsw.Close()
		return nil, errShutdown()
	}
	o.watches[w] = struct{}{}
	o.mu.Unlock()
	go w.run()
	return w, nil
}

// run reads fsnotify until the watcher is closed.
func (w *osWatch) run() {
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if w.overKqueueLimit(ev) {
				return
			}
			w.event(ev)
		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.push(hostos.WatchEvent{Path: w.path, Kind: "error", Err: mapErr(err, w.path).Error()})
		}
	}
}

func opKind(op fsnotify.Op) string {
	switch {
	case op.Has(fsnotify.Create):
		return "create"
	case op.Has(fsnotify.Write):
		return "write"
	case op.Has(fsnotify.Remove):
		return "remove"
	case op.Has(fsnotify.Rename):
		return "rename"
	}
	return "" // chmod
}

// mergeKind combines the pending kind of a path with a newer one: a file created and then
// written is still created, a file removed and created again was rewritten.
func mergeKind(old, n string) string {
	switch {
	case old == "create" && n == "write":
		return "create"
	case old == "remove" && n == "create":
		return "write"
	}
	return n
}

// overKqueueLimit keeps count of the entries of a watched directory on platforms where each
// costs a descriptor. kqueue opens a descriptor for an entry after it has delivered the
// entry's create event, and fsnotify delivers events one at a time, so stopping here keeps the
// descriptors close to the limit. The count follows creates and removals; it is checked
// against the directory itself before the watch is given up. When the limit is passed it
// queues a `limit` error, stops the watcher and reports true.
func (w *osWatch) overKqueueLimit(ev fsnotify.Event) bool {
	if w.entries < 0 {
		return false
	}
	switch {
	case ev.Op.Has(fsnotify.Create):
		w.entries++
	case ev.Op.Has(fsnotify.Remove), ev.Op.Has(fsnotify.Rename):
		w.entries = max(w.entries-1, 0)
	}
	if w.entries <= kqueueDirLimit {
		return false
	}
	ents, err := os.ReadDir(w.path)
	if err != nil {
		return false
	}
	if w.entries = len(ents); w.entries <= kqueueDirLimit {
		return false
	}
	w.mu.Lock()
	if !w.stopped {
		w.queue = append(w.queue, hostos.WatchEvent{Path: w.path, Kind: "error", Err: fmt.Sprintf("limit: the directory has more than %d entries; the watch was stopped", kqueueDirLimit)})
		w.signal()
	}
	w.mu.Unlock()
	w.Stop()
	return true
}

// event coalesces ev into the pending event of its path.
func (w *osWatch) event(ev fsnotify.Event) {
	kind := opKind(ev.Op)
	path := filepath.Clean(ev.Name)
	if kind == "" || (w.file != "" && path != w.file) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if pe := w.pending[path]; pe != nil {
		pe.kind = mergeKind(pe.kind, kind)
		return
	}
	pe := &pendingEvent{kind: kind}
	w.pending[path] = pe
	pe.timer = time.AfterFunc(w.debounce, func() {
		w.mu.Lock()
		delete(w.pending, path)
		w.mu.Unlock()
		w.push(hostos.WatchEvent{Path: path, Kind: pe.kind})
	})
}

// push queues an event for Next.
func (w *osWatch) push(e hostos.WatchEvent) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopped {
		return
	}
	if len(w.queue) >= watchQueueMax {
		w.overflow = true
		return
	}
	w.queue = append(w.queue, e)
	w.signal()
}

func (w *osWatch) signal() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Next implements hostos.Watch.
func (w *osWatch) Next(ctx context.Context) (hostos.WatchEvent, bool) {
	for {
		w.mu.Lock()
		switch {
		case w.overflow:
			w.overflow = false
			w.mu.Unlock()
			return hostos.WatchEvent{Path: w.path, Kind: "error", Err: "limit: file events were dropped"}, true
		case len(w.queue) > 0:
			e := w.queue[0]
			w.queue = w.queue[1:]
			if len(w.queue) > 0 {
				w.signal()
			}
			w.mu.Unlock()
			return e, true
		case w.stopped:
			w.mu.Unlock()
			return hostos.WatchEvent{}, false
		}
		w.mu.Unlock()
		select {
		case <-w.wake:
		case <-ctx.Done():
			return hostos.WatchEvent{}, false
		}
	}
}

// Stop implements hostos.Watch.
func (w *osWatch) Stop() {
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	w.stopped = true
	for _, pe := range w.pending {
		pe.timer.Stop()
	}
	w.pending = map[string]*pendingEvent{}
	w.signal()
	w.mu.Unlock()
	_ = w.fsw.Close()
	w.os.mu.Lock()
	delete(w.os.watches, w)
	w.os.mu.Unlock()
}
