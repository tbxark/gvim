package watch

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
)

// fakeTimer is a Timer whose callback runs only when the test fires it.
type fakeTimer struct {
	mu      sync.Mutex
	d       time.Duration
	f       func()
	stopped bool
}

func (t *fakeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	was := !t.stopped
	t.stopped = true
	return was
}

// fakeClock records every timer it schedules, in order.
type fakeClock struct {
	mu     sync.Mutex
	timers []*fakeTimer
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{d: d, f: f}
	c.timers = append(c.timers, t)
	return t
}

// fire runs timer i the way the runtime would: not at all if it was stopped.
func (c *fakeClock) fire(t *testing.T, i int) {
	t.Helper()
	c.mu.Lock()
	timer := c.timers[i]
	c.mu.Unlock()
	timer.mu.Lock()
	stopped := timer.stopped
	timer.mu.Unlock()
	if !stopped {
		timer.f()
	}
}

func (c *fakeClock) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.timers)
}

func (c *fakeClock) stopped(i int) bool {
	c.mu.Lock()
	timer := c.timers[i]
	c.mu.Unlock()
	timer.mu.Lock()
	defer timer.mu.Unlock()
	return timer.stopped
}

// newFake returns a watcher on a fake clock. The events are fed to it with
// event(), so no file system is touched.
func newFake(t *testing.T) (*Watcher, *fakeClock) {
	t.Helper()
	clk := &fakeClock{}
	w, err := New(Options{AfterFunc: clk.AfterFunc})
	if err != nil {
		t.Skipf("fsnotify cannot start: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, clk
}

// noValue fails the test if a Changed is waiting on Events.
func noValue(t *testing.T, w *Watcher) {
	t.Helper()
	select {
	case got := <-w.Events():
		t.Fatalf("unexpected Changed %+v", got)
	default:
	}
}

func TestDelayDefaultsTo200ms(t *testing.T) {
	w, clk := newFake(t)
	if DefaultDelay != 200*time.Millisecond {
		t.Fatalf("DefaultDelay = %v, want 200ms", DefaultDelay)
	}
	w.event(fsnotify.Event{Name: "/ws/a/one.txt", Op: fsnotify.Create})
	if clk.count() != 1 || clk.timers[0].d != 200*time.Millisecond {
		t.Fatalf("timers = %d, delay = %v; want one timer of 200ms", clk.count(), clk.timers[0].d)
	}
}

func TestBurstBecomesOneChangedAfterQuietPeriod(t *testing.T) {
	w, clk := newFake(t)
	w.event(fsnotify.Event{Name: "/ws/a/one.txt", Op: fsnotify.Create})
	w.event(fsnotify.Event{Name: "/ws/b/three.txt", Op: fsnotify.Write})
	w.event(fsnotify.Event{Name: "/ws/a/two.txt", Op: fsnotify.Create})

	if clk.count() != 3 {
		t.Fatalf("timers scheduled = %d, want 3", clk.count())
	}
	if !clk.stopped(0) || !clk.stopped(1) {
		t.Fatal("superseded quiet periods were not stopped")
	}
	if clk.stopped(2) {
		t.Fatal("the latest quiet period was stopped")
	}

	// Callbacks of the superseded periods run anyway, as a timer that fired
	// just before Stop would. They must not send.
	clk.timers[0].f()
	clk.timers[1].f()
	noValue(t, w)

	clk.fire(t, 2)
	got := <-w.Events()
	if want := []string{"/ws/a", "/ws/b"}; !slices.Equal(got.Dirs, want) {
		t.Fatalf("Dirs = %v, want %v", got.Dirs, want)
	}
	noValue(t, w)
}

func TestSentOnceNotResent(t *testing.T) {
	w, clk := newFake(t)
	w.event(fsnotify.Event{Name: "/ws/a/x", Op: fsnotify.Create})
	clk.fire(t, 0)
	<-w.Events()
	clk.timers[0].f() // nothing is pending, so a second callback sends nothing
	noValue(t, w)
}

func TestSlowReaderGetsMergedDirs(t *testing.T) {
	w, clk := newFake(t)
	w.event(fsnotify.Event{Name: "/ws/a/x", Op: fsnotify.Create})
	clk.fire(t, 0)
	w.event(fsnotify.Event{Name: "/ws/b/y", Op: fsnotify.Create})
	clk.fire(t, 1)

	got := <-w.Events()
	if want := []string{"/ws/a", "/ws/b"}; !slices.Equal(got.Dirs, want) {
		t.Fatalf("merged Dirs = %v, want %v", got.Dirs, want)
	}
	noValue(t, w)
}

func TestCloseCancelsAndClosesEvents(t *testing.T) {
	w, clk := newFake(t)
	w.event(fsnotify.Event{Name: "/ws/a/x", Op: fsnotify.Create})
	if err := w.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
	if !clk.stopped(0) {
		t.Fatal("Close did not stop the pending quiet period")
	}
	clk.timers[0].f()
	w.event(fsnotify.Event{Name: "/ws/a/y", Op: fsnotify.Create})
	if _, ok := <-w.Events(); ok {
		t.Fatal("Events delivered a value after Close")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close() = %v, want nil", err)
	}
}

func TestOverflowReportsEveryWatchedDir(t *testing.T) {
	w, clk := newFake(t)
	a, b := t.TempDir(), t.TempDir()
	for _, d := range []string{a, b} {
		if err := w.Add(d); err != nil {
			t.Fatalf("Add(%s) = %v", d, err)
		}
	}
	w.overflow()
	clk.fire(t, 0)
	got := <-w.Events()
	want := []string{a, b}
	slices.Sort(want)
	if !slices.Equal(got.Dirs, want) {
		t.Fatalf("overflow Dirs = %v, want %v", got.Dirs, want)
	}
}

func TestCreateFileReportsChange(t *testing.T) {
	dir := t.TempDir()
	w, err := New(Options{})
	if err != nil {
		t.Skipf("fsnotify cannot start: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Add(dir); err != nil {
		t.Fatalf("Add(%s) = %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got, ok := <-w.Events():
		if !ok || !slices.Contains(got.Dirs, dir) {
			t.Fatalf("Changed = %+v (open %v), want Dirs to contain %s", got, ok, dir)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no Changed within 5s of creating a file")
	}
}

func TestRemoveStopsReportingDir(t *testing.T) {
	dir := t.TempDir()
	w, err := New(Options{})
	if err != nil {
		t.Skipf("fsnotify cannot start: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Add(dir); err != nil {
		t.Fatalf("Add(%s) = %v", dir, err)
	}
	if err := w.Remove(dir); err != nil {
		t.Fatalf("Remove(%s) = %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "after.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-w.Events():
		t.Fatalf("Changed %+v after Remove", got)
	case <-time.After(500 * time.Millisecond):
	}
}
