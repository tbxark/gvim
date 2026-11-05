// Package watch reports changes to directories on disk so the explorer can
// refresh. It wraps github.com/fsnotify/fsnotify and debounces bursts: changes
// that arrive within one quiet period of each other become one Changed value.
//
// The watcher is not recursive. Add each directory the tree shows; a change to
// a directory that is not added is not reported. The package has no tea types:
// the UI turns Events into a tea.Cmd.
package watch

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// DefaultDelay is the quiet period after the last change before Changed is
// sent.
const DefaultDelay = 200 * time.Millisecond

// Changed reports that entries in some watched directories changed. Dirs holds
// the directories that contained a changed entry since the previous Changed,
// sorted and without duplicates. An overflow in the operating system's queue
// reports every watched directory.
type Changed struct {
	Dirs []string
}

// Timer is a scheduled call that can be cancelled. *time.Timer satisfies it.
type Timer interface {
	Stop() bool
}

// AfterFunc schedules f to run once after d and returns a Timer for the call.
// f must run on its own goroutine, never inside the AfterFunc call.
// time.AfterFunc has this shape, so tests can inject a fake clock.
type AfterFunc func(d time.Duration, f func()) Timer

// Options configures New. The zero value is ready to use.
type Options struct {
	// Delay is the quiet period. Zero means DefaultDelay.
	Delay time.Duration
	// AfterFunc schedules the quiet period. Nil means time.AfterFunc.
	AfterFunc AfterFunc
}

// Watcher sends a Changed after each quiet period that follows a change. Its
// methods are safe for concurrent use.
type Watcher struct {
	fsw   *fsnotify.Watcher
	delay time.Duration
	after AfterFunc
	out   chan Changed
	loop  chan struct{} // closed when the event loop returns

	mu      sync.Mutex
	watched map[string]bool // directories added and not removed
	pending map[string]bool // directories changed since the last Changed
	timer   Timer           // the quiet period in progress, if any
	gen     uint64          // identifies the current quiet period
	closed  bool
}

// New starts a watcher. It fails if the operating system cannot provide one.
func New(opts Options) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch: start: %w", err)
	}
	w := &Watcher{
		fsw:     fsw,
		delay:   opts.Delay,
		after:   opts.AfterFunc,
		out:     make(chan Changed, 1),
		loop:    make(chan struct{}),
		watched: make(map[string]bool),
		pending: make(map[string]bool),
	}
	if w.delay <= 0 {
		w.delay = DefaultDelay
	}
	if w.after == nil {
		w.after = func(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }
	}
	go w.run()
	return w, nil
}

// Add watches the entries of dir. Adding a directory twice is harmless.
func (w *Watcher) Add(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("watch: add %q: %w", dir, err)
	}
	if err := w.fsw.Add(abs); err != nil {
		return fmt.Errorf("watch: add %q: %w", dir, err)
	}
	w.mu.Lock()
	w.watched[abs] = true
	w.mu.Unlock()
	return nil
}

// Remove stops watching dir. The directory is forgotten even if the operating
// system reports an error, for example because the directory was deleted.
func (w *Watcher) Remove(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("watch: remove %q: %w", dir, err)
	}
	w.mu.Lock()
	delete(w.watched, abs)
	w.mu.Unlock()
	if err := w.fsw.Remove(abs); err != nil {
		return fmt.Errorf("watch: remove %q: %w", dir, err)
	}
	return nil
}

// Events returns the channel of Changed values. It is closed by Close. When
// the reader is slower than the changes, unread values are merged, so no
// directory is lost.
func (w *Watcher) Events() <-chan Changed { return w.out }

// Close stops the watcher, cancels a pending quiet period and closes Events.
// Calling Close again does nothing.
func (w *Watcher) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	if w.timer != nil {
		w.timer.Stop()
		w.timer = nil
	}
	w.mu.Unlock()

	err := w.fsw.Close()
	<-w.loop
	close(w.out)
	if err != nil {
		return fmt.Errorf("watch: close: %w", err)
	}
	return nil
}

// run turns fsnotify events into quiet-period updates until fsnotify closes.
func (w *Watcher) run() {
	defer close(w.loop)
	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			w.event(ev)
		case _, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.overflow()
		}
	}
}

// event records the directory that holds the changed entry. If the entry is
// itself a watched directory, that directory changed too.
func (w *Watcher) event(ev fsnotify.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	dirs := []string{filepath.Dir(ev.Name)}
	if w.watched[ev.Name] {
		dirs = append(dirs, ev.Name)
	}
	w.schedule(dirs)
}

// overflow handles an error from fsnotify. Events may have been lost, so every
// watched directory counts as changed.
func (w *Watcher) overflow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.schedule(slices.Collect(maps.Keys(w.watched)))
}

// schedule marks dirs as changed and restarts the quiet period. The caller
// holds mu.
func (w *Watcher) schedule(dirs []string) {
	if w.closed || len(dirs) == 0 {
		return
	}
	for _, d := range dirs {
		w.pending[d] = true
	}
	if w.timer != nil {
		w.timer.Stop()
	}
	w.gen++
	gen := w.gen
	w.timer = w.after(w.delay, func() { w.fire(gen) })
}

// fire sends the pending directories as one Changed. A timer that was
// superseded by a later change, or that fires after Close, does nothing.
func (w *Watcher) fire(gen uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || gen != w.gen || len(w.pending) == 0 {
		return
	}
	w.timer = nil
	ev := Changed{Dirs: slices.Sorted(maps.Keys(w.pending))}
	clear(w.pending)
	select {
	case w.out <- ev:
		return
	default:
	}
	// The reader has not taken the previous Changed. Merge it into this one
	// so the reader sees every directory. Only fire sends, and it holds mu, so
	// the buffer is empty once the old value is taken.
	select {
	case old := <-w.out:
		merged := append(old.Dirs, ev.Dirs...)
		slices.Sort(merged)
		ev.Dirs = slices.Compact(merged)
	default:
	}
	w.out <- ev
}
