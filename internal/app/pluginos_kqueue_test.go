//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package app

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/pluginapi"
)

// TestKqueueDirLimit: on kqueue platforms a directory with more than 512 entries is not
// watched (kqueue opens a descriptor per entry); a file or a smaller directory is.
func TestKqueueDirLimit(t *testing.T) {
	o, dir := newTestOS(t)
	big := filepath.Join(dir, "big")
	if err := os.Mkdir(big, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range kqueueDirLimit + 1 {
		if err := os.WriteFile(filepath.Join(big, "f"+strconv.Itoa(i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := o.Watch(context.Background(), big, plugin.WatchOptions{})
	if codeOf(err) != pluginapi.CodeLimit {
		t.Fatalf("err = %v, want a limit error", err)
	}
	if err == nil || err.Error() != "limit: directory too large to watch; watch a file or a subdirectory" {
		t.Fatalf("message = %v", err)
	}
	// at the limit it still works, and so does a file inside the big directory
	if err := os.Remove(filepath.Join(big, "f0")); err != nil {
		t.Fatal(err)
	}
	w, err := o.Watch(context.Background(), big, plugin.WatchOptions{})
	if err != nil {
		t.Fatalf("a directory at the limit: %v", err)
	}
	w.Stop()
	w, err = o.Watch(context.Background(), filepath.Join(big, "f1"), plugin.WatchOptions{})
	if err != nil {
		t.Fatalf("a file in a big directory: %v", err)
	}
	w.Stop()
}

// TestKqueueDirGrowingPastTheLimit: the limit also holds for a directory that grows after
// the watch began. The watcher reports `limit` once, stops, and releases the descriptors
// kqueue opened for the entries (one each).
func TestKqueueDirGrowingPastTheLimit(t *testing.T) {
	o, dir := newTestOS(t)
	grow := filepath.Join(dir, "grow")
	if err := os.Mkdir(grow, 0o755); err != nil {
		t.Fatal(err)
	}
	openFDs := func() int {
		ents, err := os.ReadDir("/dev/fd")
		if err != nil {
			t.Fatal(err)
		}
		return len(ents)
	}
	base := openFDs()
	w, err := o.Watch(context.Background(), grow, plugin.WatchOptions{Debounce: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Stop()
	log := collectEvents(w)
	for i := range kqueueDirLimit + 300 {
		if err := os.WriteFile(filepath.Join(grow, "f"+strconv.Itoa(i)), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "the limit error event", func() bool {
		for _, e := range log.snapshot() {
			if e.Kind == "error" && strings.HasPrefix(e.Err, "limit:") {
				return true
			}
		}
		return false
	})
	eventually(t, "the descriptors of the entries to be released", func() bool { return openFDs() <= base+8 })
}
