package app

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/pluginapi"
)

func writePluginFile(dir, name string) error {
	return os.WriteFile(filepath.Join(dir, name), []byte(time.Now().String()), 0o644)
}

// eventLog collects the events of a watcher on a goroutine.
type eventLog struct {
	mu     sync.Mutex
	events []plugin.WatchEvent
}

func collectEvents(w plugin.Watch) *eventLog {
	l := &eventLog{}
	go func() {
		for {
			e, ok := w.Next(context.Background())
			if !ok {
				return
			}
			l.mu.Lock()
			l.events = append(l.events, e)
			l.mu.Unlock()
		}
	}()
	return l
}

func (l *eventLog) snapshot() []plugin.WatchEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]plugin.WatchEvent(nil), l.events...)
}

func (l *eventLog) count(path, kind string) int {
	n := 0
	for _, e := range l.snapshot() {
		if e.Path == path && (kind == "" || e.Kind == kind) {
			n++
		}
	}
	return n
}

// TestWatchDebounceCoalesces: a burst of writes inside one window is a single event, and a
// file that was created and then written is reported as created.
func TestWatchDebounceCoalesces(t *testing.T) {
	o, dir := newTestOS(t)
	w, err := o.Watch(context.Background(), dir, plugin.WatchOptions{Debounce: 150 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	log := collectEvents(w)
	path := filepath.Join(dir, "burst.txt")
	for range 6 {
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	eventually(t, "the coalesced event", func() bool { return log.count(path, "") >= 1 })
	time.Sleep(400 * time.Millisecond) // nothing more may follow from the same burst
	evs := log.snapshot()
	if n := log.count(path, ""); n != 1 {
		t.Fatalf("events = %+v, want exactly one for the burst", evs)
	}
	if log.count(path, "create") != 1 {
		t.Fatalf("events = %+v, want kind create", evs)
	}
	// a later change after the window is a new event
	if err := os.WriteFile(path, []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the second event", func() bool { return log.count(path, "write") >= 1 })
}

func TestWatchKinds(t *testing.T) {
	o, dir := newTestOS(t)
	w, err := o.Watch(context.Background(), dir, plugin.WatchOptions{Debounce: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	log := collectEvents(w)
	path := filepath.Join(dir, "k.txt")
	if err := os.WriteFile(path, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	eventually(t, "create", func() bool { return log.count(path, "create") == 1 })
	time.Sleep(60 * time.Millisecond)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	eventually(t, "remove", func() bool { return log.count(path, "remove")+log.count(path, "rename") >= 1 })

	w.Stop()
	w.Stop() // idempotent
	eventually(t, "Next to end after Stop", func() bool {
		_, ok := w.Next(context.Background())
		return !ok
	})
}

// TestWatchAFileSurvivesReplacement: editors save by renaming a new file over the old one;
// a watch on the file must keep reporting.
func TestWatchAFileSurvivesReplacement(t *testing.T) {
	o, dir := newTestOS(t)
	path := filepath.Join(dir, "doc.txt")
	if err := os.WriteFile(path, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := o.Watch(context.Background(), path, plugin.WatchOptions{Debounce: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	log := collectEvents(w)
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("noise"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if err := o.WriteFile(context.Background(), path, []byte("v"+string(rune('2'+i))), plugin.WriteOptions{Atomic: true}); err != nil {
			t.Fatal(err)
		}
		n := log.count(path, "")
		eventually(t, "an event for the replaced file", func() bool { return log.count(path, "") > n })
	}
	if n := log.count(filepath.Join(dir, "other.txt"), ""); n != 0 {
		t.Fatalf("events of a sibling were delivered: %+v", log.snapshot())
	}
}

func TestWatchErrors(t *testing.T) {
	o, dir := newTestOS(t)
	if _, err := o.Watch(context.Background(), filepath.Join(dir, "missing"), plugin.WatchOptions{}); codeOf(err) != pluginapi.CodeNotFound {
		t.Fatalf("err = %v", err)
	}
	t.Run("Shutdown stops watchers", func(t *testing.T) {
		w, err := o.Watch(context.Background(), dir, plugin.WatchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		o.Shutdown()
		if _, ok := w.Next(context.Background()); ok {
			t.Fatal("Next returned an event after Shutdown")
		}
		if _, err := o.Watch(context.Background(), dir, plugin.WatchOptions{}); codeOf(err) != pluginapi.CodeClosed {
			t.Fatalf("Watch after Shutdown: %v", err)
		}
	})
	t.Run("Next honours its context", func(t *testing.T) {
		o2 := newPluginOS(nil)
		w, err := o2.Watch(context.Background(), dir, plugin.WatchOptions{})
		if err != nil {
			t.Fatal(err)
		}
		defer w.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()
		if _, ok := w.Next(ctx); ok {
			t.Fatal("event from nowhere")
		}
	})
}

func TestMergeKind(t *testing.T) {
	rows := []struct{ old, new, want string }{
		{"create", "write", "create"},
		{"remove", "create", "write"},
		{"write", "write", "write"},
		{"write", "remove", "remove"},
		{"create", "remove", "remove"},
		{"write", "rename", "rename"},
	}
	for _, r := range rows {
		if got := mergeKind(r.old, r.new); got != r.want {
			t.Errorf("mergeKind(%s, %s) = %s, want %s", r.old, r.new, got, r.want)
		}
	}
}
