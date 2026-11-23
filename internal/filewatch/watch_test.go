package filewatch

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

// TestChangedNamesTheChangedFiles checks that a Changed carries the paths of the entries
// that changed, so a reader can tell which file changed and not only its folder. A burst
// merges the names of its events, sorted and without duplicates.
func TestChangedNamesTheChangedFiles(t *testing.T) {
	w, clk := newFake(t)
	w.event(fsnotify.Event{Name: "/ws/a/one.txt", Op: fsnotify.Write})
	w.event(fsnotify.Event{Name: "/ws/b/three.txt", Op: fsnotify.Create})
	w.event(fsnotify.Event{Name: "/ws/a/one.txt", Op: fsnotify.Write})
	clk.fire(t, 2)

	got := <-w.Events()
	if want := []string{"/ws/a/one.txt", "/ws/b/three.txt"}; !slices.Equal(got.Files, want) {
		t.Fatalf("Files = %v, want %v", got.Files, want)
	}
	noValue(t, w)
}

// TestSlowReaderGetsMergedFiles checks that the names of changed files are merged, as the
// folders are, when the reader has not taken the previous Changed.
func TestSlowReaderGetsMergedFiles(t *testing.T) {
	w, clk := newFake(t)
	w.event(fsnotify.Event{Name: "/ws/a/x", Op: fsnotify.Create})
	clk.fire(t, 0)
	w.event(fsnotify.Event{Name: "/ws/b/y", Op: fsnotify.Create})
	clk.fire(t, 1)

	got := <-w.Events()
	if want := []string{"/ws/a/x", "/ws/b/y"}; !slices.Equal(got.Files, want) {
		t.Fatalf("merged Files = %v, want %v", got.Files, want)
	}
	noValue(t, w)
}

// TestOverflowNamesNoFiles checks that an overflow, which may have lost any entry, reports
// the watched folders and names no file, so no file is taken to have changed.
func TestOverflowNamesNoFiles(t *testing.T) {
	w, clk := newFake(t)
	dir := t.TempDir()
	if err := w.Add(dir); err != nil {
		t.Fatalf("Add(%s) = %v", dir, err)
	}
	w.overflow()
	clk.fire(t, 0)

	got := <-w.Events()
	if len(got.Files) != 0 {
		t.Fatalf("overflow Files = %v, want none", got.Files)
	}
	if want := []string{dir}; !slices.Equal(got.Dirs, want) {
		t.Fatalf("overflow Dirs = %v, want %v", got.Dirs, want)
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

// TestCreateFileNamesTheFile checks on the real file system that a created file is named by
// the path it has in the watched folder. The root matches a document's file by that path.
func TestCreateFileNamesTheFile(t *testing.T) {
	dir := t.TempDir()
	w, err := New(Options{})
	if err != nil {
		t.Skipf("fsnotify cannot start: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if err := w.Add(dir); err != nil {
		t.Fatalf("Add(%s) = %v", dir, err)
	}
	path := filepath.Join(dir, "new.txt")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case got, ok := <-w.Events():
		if !ok || !slices.Contains(got.Files, path) {
			t.Fatalf("Changed = %+v (open %v), want Files to contain %s", got, ok, path)
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

func TestAddAfterCloseFailsWithoutRecording(t *testing.T) {
	w, _ := newFake(t)
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	dir := t.TempDir()
	if err := w.Add(dir); err == nil {
		t.Fatal("Add after Close succeeded, want error")
	}
	w.mu.Lock()
	_, recorded := w.watched[dir]
	w.mu.Unlock()
	if recorded {
		t.Fatalf("closed watcher recorded %q in watched", dir)
	}
}

func TestAddFailedDoesNotRecordWatched(t *testing.T) {
	w, _ := newFake(t)
	missing := filepath.Join(t.TempDir(), "missing")
	if err := w.Add(missing); err == nil {
		t.Fatal("Add(missing) succeeded, want error")
	}
	w.mu.Lock()
	_, recorded := w.watched[missing]
	w.mu.Unlock()
	if recorded {
		t.Fatalf("failed Add recorded %q in watched", missing)
	}
}

func TestChmodEventsIgnored(t *testing.T) {
	w, clk := newFake(t)
	w.event(fsnotify.Event{Name: "/ws/a/one.txt", Op: fsnotify.Chmod})
	if clk.count() != 0 {
		t.Fatalf("pure Chmod scheduled %d timers, want 0", clk.count())
	}
	noValue(t, w)

	w.event(fsnotify.Event{Name: "/ws/a/one.txt", Op: fsnotify.Write | fsnotify.Chmod})
	if clk.count() != 1 {
		t.Fatalf("Write|Chmod scheduled %d timers, want 1", clk.count())
	}
	clk.fire(t, 0)
	got := <-w.Events()
	if !slices.Equal(got.Files, []string{"/ws/a/one.txt"}) {
		t.Fatalf("Files = %v, want [/ws/a/one.txt]", got.Files)
	}
}

func TestMaxWaitCeilingPreventsStarvationFakeClock(t *testing.T) {
	w, clk := newFake(t)
	now := time.Unix(1_700_000_000, 0)
	w.now = func() time.Time { return now }

	// Send events every 100ms (half of DefaultDelay=200ms).
	// maxDelay = 5 * 200ms = 1000ms.
	for step := range 12 {
		w.event(fsnotify.Event{Name: "/ws/a/one.txt", Op: fsnotify.Write})
		now = now.Add(100 * time.Millisecond)
		if step == 9 && clk.count() == 0 {
			t.Fatal("no timers scheduled")
		}
	}
	// At t=800ms, now+200ms == deadline (1000ms), so timer is capped once and
	// subsequent events at t=900ms, 1000ms, 1100ms must not postpone or schedule more.
	lastIdx := clk.count() - 1
	if clk.stopped(lastIdx) {
		t.Fatal("the capped timer should still be active")
	}
	if got := clk.timers[lastIdx].d; got != 200*time.Millisecond {
		t.Fatalf("capped timer delay = %v, want 200ms", got)
	}
	if clk.count() != 9 {
		t.Fatalf("timers scheduled = %d, want 9 (capped at maxDelay)", clk.count())
	}

	clk.fire(t, lastIdx)
	got := <-w.Events()
	if !slices.Equal(got.Files, []string{"/ws/a/one.txt"}) {
		t.Fatalf("Files = %v, want [/ws/a/one.txt]", got.Files)
	}

	// Next batch resets pendingStart and works normally.
	w.event(fsnotify.Event{Name: "/ws/b/two.txt", Op: fsnotify.Create})
	if clk.count() != 10 || clk.timers[9].d != DefaultDelay {
		t.Fatalf("after flush: timers=%d delay=%v", clk.count(), clk.timers[9].d)
	}
	clk.fire(t, 9)
	got = <-w.Events()
	if !slices.Equal(got.Files, []string{"/ws/b/two.txt"}) {
		t.Fatalf("second batch Files = %v, want [/ws/b/two.txt]", got.Files)
	}
}

func TestMaxWaitCeilingRealTimerAndReuse(t *testing.T) {
	w, err := New(Options{Delay: 20 * time.Millisecond})
	if err != nil {
		t.Skipf("fsnotify cannot start: %v", err)
	}
	defer func() { _ = w.Close() }()

	// Continuous events every 5ms would starve a 20ms debounce without maxDelay=100ms.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(5 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				w.event(fsnotify.Event{Name: "/ws/a/stream.txt", Op: fsnotify.Write})
			}
		}
	}()

	select {
	case got := <-w.Events():
		if !slices.Equal(got.Files, []string{"/ws/a/stream.txt"}) {
			close(stop)
			<-done
			t.Fatalf("Files = %v", got.Files)
		}
	case <-time.After(2 * time.Second):
		close(stop)
		<-done
		t.Fatal("continuous writes starved Changed past maxDelay ceiling")
	}
	close(stop)
	<-done

	// Drain any trailing event and verify the same prodTimer instance is reused across batches.
	time.Sleep(40 * time.Millisecond)
	select {
	case <-w.Events():
	default:
	}

	w.mu.Lock()
	firstTimer := w.prodTimer
	w.mu.Unlock()
	if firstTimer == nil {
		t.Fatal("prodTimer was nil after production burst")
	}

	w.event(fsnotify.Event{Name: "/ws/a/next.txt", Op: fsnotify.Create})
	select {
	case got := <-w.Events():
		if !slices.Equal(got.Files, []string{"/ws/a/next.txt"}) {
			t.Fatalf("next batch Files = %v", got.Files)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second batch did not fire")
	}
	w.mu.Lock()
	secondTimer := w.prodTimer
	w.mu.Unlock()
	if secondTimer != firstTimer {
		t.Fatal("prodTimer was reallocated instead of reused across batches")
	}
}

func BenchmarkWatcherEvent(b *testing.B) {
	w, err := New(Options{Delay: 50 * time.Millisecond})
	if err != nil {
		b.Skipf("fsnotify cannot start: %v", err)
	}
	defer func() { _ = w.Close() }()
	// Drain Events in background so channel never stalls.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range w.Events() {
		}
	}()
	ev := fsnotify.Event{Name: "/ws/a/file.txt", Op: fsnotify.Write}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		w.event(ev)
	}
	b.StopTimer()
	_ = w.Close()
	<-done
}
