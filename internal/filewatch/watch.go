// Package filewatch reports changes to directories on disk so the explorer can
// refresh. It wraps github.com/fsnotify/fsnotify and debounces bursts: changes
// that arrive within one quiet period of each other become one Changed value.
//
// The watcher is not recursive. Add each directory the tree shows; a change to
// a directory that is not added is not reported. The package has no tea types:
// the UI turns Events into a tea.Cmd.
package filewatch

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

// maxDelayFactor bounds how long continuous changes can delay Changed via
// debounce resets before flushing.
const maxDelayFactor = 5

// Changed reports that entries in some watched directories changed. Dirs holds
// the directories that contained a changed entry since the previous Changed,
// sorted and without duplicates. Files holds the paths of the changed entries
// themselves, in the same way, so a reader can tell which file changed. An
// overflow in the operating system's queue reports every watched directory and
// no file.
type Changed struct {
	Dirs  []string
	Files []string
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

	mu           sync.Mutex
	watched      map[string]bool // directories added and not removed
	pending      map[string]bool // directories changed since the last Changed
	pendingFiles map[string]bool // entries changed since the last Changed
	pendingStart time.Time       // when the current pending batch started
	now          func() time.Time
	timer        Timer       // the quiet period in progress when after != nil
	prodTimer    *time.Timer // reusable timer when after == nil
	fireAt       time.Time   // monotonic deadline of the armed prodTimer
	capped       bool        // timer is already capped at the max-wait ceiling
	gen          uint64      // identifies the current quiet period
	closed       bool
}

// New starts a watcher. It fails if the operating system cannot provide one.
func New(opts Options) (*Watcher, error) {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch: start: %w", err)
	}
	w := &Watcher{
		fsw:          fsw,
		delay:        opts.Delay,
		after:        opts.AfterFunc,
		out:          make(chan Changed, 1),
		loop:         make(chan struct{}),
		watched:      make(map[string]bool),
		pending:      make(map[string]bool),
		pendingFiles: make(map[string]bool),
		now:          time.Now,
	}
	if w.delay <= 0 {
		w.delay = DefaultDelay
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
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return fmt.Errorf("watch: add %q: closed", dir)
	}
	wasWatched := w.watched[abs]
	w.watched[abs] = true
	w.mu.Unlock()
	if err := w.fsw.Add(abs); err != nil {
		if !wasWatched {
			w.mu.Lock()
			delete(w.watched, abs)
			w.mu.Unlock()
		}
		return fmt.Errorf("watch: add %q: %w", dir, err)
	}
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
	if w.prodTimer != nil {
		w.prodTimer.Stop()
		w.prodTimer = nil
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

// event records the directory that holds the changed entry, and the entry itself. If
// the entry is itself a watched directory, that directory changed too. Pure Chmod events
// are ignored.
func (w *Watcher) event(ev fsnotify.Event) {
	if ev.Op == fsnotify.Chmod {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.pending[filepath.Dir(ev.Name)] = true
	if w.watched[ev.Name] {
		w.pending[ev.Name] = true
	}
	w.pendingFiles[ev.Name] = true
	w.scheduleLocked()
}

// overflow handles an error from fsnotify. Events may have been lost, so every
// watched directory counts as changed, and no file is named.
func (w *Watcher) overflow() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || len(w.watched) == 0 {
		return
	}
	for d := range w.watched {
		w.pending[d] = true
	}
	w.scheduleLocked()
}

func (w *Watcher) nowTime() time.Time {
	if w.now != nil {
		return w.now()
	}
	return time.Now()
}

// scheduleLocked arms or restarts the quiet-period timer up to the max-wait ceiling.
// The caller holds mu and has populated pending/pendingFiles.
func (w *Watcher) scheduleLocked() {
	if w.closed || (len(w.pending) == 0 && len(w.pendingFiles) == 0) {
		return
	}
	now := w.nowTime()
	if w.pendingStart.IsZero() {
		w.pendingStart = now
		w.capped = false
	}
	deadline := w.pendingStart.Add(maxDelayFactor * w.delay)
	d := w.delay
	capped := false
	if !now.Add(d).Before(deadline) {
		d = deadline.Sub(now)
		if d <= 0 {
			if w.timer != nil || !w.fireAt.IsZero() {
				return
			}
			d = 0
		}
		capped = true
	}
	if w.after != nil {
		if w.capped && w.timer != nil {
			return
		}
		if w.timer != nil {
			w.timer.Stop()
		}
		w.gen++
		gen := w.gen
		w.capped = capped
		w.timer = w.after(d, func() { w.fire(gen) })
		return
	}
	if w.capped && !w.fireAt.IsZero() {
		return
	}
	w.capped = capped
	w.fireAt = now.Add(d)
	if w.prodTimer == nil {
		w.prodTimer = time.AfterFunc(d, w.fireProd)
		return
	}
	w.prodTimer.Stop()
	w.prodTimer.Reset(d)
}

// fireProd is the reusable production timer callback.
func (w *Watcher) fireProd() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || (len(w.pending) == 0 && len(w.pendingFiles) == 0) {
		return
	}
	if w.nowTime().Before(w.fireAt) {
		return
	}
	w.flushLocked()
}

// fire sends the pending directories and files as one Changed. A timer that was
// superseded by a later change, or that fires after Close, does nothing.
func (w *Watcher) fire(gen uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || gen != w.gen || (len(w.pending) == 0 && len(w.pendingFiles) == 0) {
		return
	}
	w.flushLocked()
}

// flushLocked delivers the pending changes to out and resets batch state. Caller holds mu.
func (w *Watcher) flushLocked() {
	w.timer = nil
	w.pendingStart = time.Time{}
	w.fireAt = time.Time{}
	w.capped = false
	ev := Changed{
		Dirs:  slices.Sorted(maps.Keys(w.pending)),
		Files: slices.Sorted(maps.Keys(w.pendingFiles)),
	}
	clear(w.pending)
	clear(w.pendingFiles)
	select {
	case w.out <- ev:
		return
	default:
	}
	// The reader has not taken the previous Changed. Merge it into this one
	// so the reader sees every directory and file. Only fire sends, and it holds
	// mu, so the buffer is empty once the old value is taken.
	select {
	case old := <-w.out:
		ev.Dirs = union(old.Dirs, ev.Dirs)
		ev.Files = union(old.Files, ev.Files)
	default:
	}
	w.out <- ev
}

// union returns the sorted paths of a and b, without duplicates. It does not change
// either slice.
func union(a, b []string) []string {
	merged := append(slices.Clone(a), b...)
	slices.Sort(merged)
	return slices.Compact(merged)
}
