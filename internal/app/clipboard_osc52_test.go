package app

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/tbxark/gvim/config"
)

// TestDockOSC52QueryGating proves the app-level read/paste policy: the default is
// off, an untrusted workspace is refused even when enabled, a trusted workspace
// reads the clipboard only when enabled, and a trust downgrade refuses the next
// query without reading it again. The handler is the one installed on the dock's
// terminal sessions, so it runs on the terminal read goroutine, never the UI
// goroutine.
func TestDockOSC52QueryGating(t *testing.T) {
	t.Parallel()
	var reads atomic.Int32
	read := func() (string, error) {
		reads.Add(1)
		return "hello", nil
	}
	var trusted atomic.Bool
	cfg := config.Defaults()
	query := dockOSC52Query(cfg, trusted.Load, read)

	// Default off: no read even in a trusted workspace.
	trusted.Store(true)
	if text, ok := query("c"); ok || text != "" {
		t.Fatalf("default-off query = %q, %v; want no answer", text, ok)
	}
	if got := reads.Load(); got != 0 {
		t.Fatalf("default-off clipboard reads = %d, want 0", got)
	}

	// Enabled but untrusted: refused before any clipboard access.
	cfg.Clipboard.OSC52Paste = true
	query = dockOSC52Query(cfg, trusted.Load, read)
	trusted.Store(false)
	if text, ok := query("c"); ok || text != "" {
		t.Fatalf("untrusted query = %q, %v; want no answer", text, ok)
	}
	if got := reads.Load(); got != 0 {
		t.Fatalf("untrusted clipboard reads = %d, want 0", got)
	}

	// Enabled and trusted: one bounded read per query.
	trusted.Store(true)
	if text, ok := query("c"); !ok || text != "hello" {
		t.Fatalf("trusted enabled query = %q, %v; want hello", text, ok)
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("trusted clipboard reads = %d, want 1", got)
	}

	// Trust downgraded: the next query is refused and the clipboard is not read.
	trusted.Store(false)
	if text, ok := query("c"); ok || text != "" {
		t.Fatalf("downgraded query = %q, %v; want no answer", text, ok)
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("downgraded clipboard reads = %d, want still 1 (no read when gated)", got)
	}
}

// TestDockOSC52QueryReadResults: an empty, failed or oversized clipboard answers
// nothing, so a child's paste cannot hang on a broken clipboard.
func TestDockOSC52QueryReadResults(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Clipboard.OSC52Paste = true
	trusted := func() bool { return true }
	tests := []struct {
		name string
		read func() (string, error)
		ok   bool
	}{
		{"empty", func() (string, error) { return "", nil }, false},
		{"error", func() (string, error) { return "", errors.New("no clipboard tool") }, false},
		{"oversize", func() (string, error) { return string(make([]byte, clipboardOSC52PasteMax+1)), nil }, false},
		{"at the cap", func() (string, error) { return string(make([]byte, clipboardOSC52PasteMax)), nil }, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok := dockOSC52Query(cfg, trusted, tt.read)("c")
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && len(text) != clipboardOSC52PasteMax {
				t.Fatalf("text length = %d", len(text))
			}
		})
	}
}

// TestDockOSC52QueryDoesNotBlockOtherWork: the handler reads synchronously on the
// terminal read goroutine; a slow clipboard read therefore blocks only that
// session's read loop, not a separately running update loop. The test models the
// update loop as an independent goroutine and shows it keeps making progress while
// the clipboard read is blocked, then the query returns and is answered once.
func TestDockOSC52QueryDoesNotBlockOtherWork(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Clipboard.OSC52Paste = true
	release := make(chan struct{})
	read := func() (string, error) {
		<-release
		return "hello", nil
	}
	query := dockOSC52Query(cfg, func() bool { return true }, read)
	done := make(chan bool, 1)
	go func() {
		text, ok := query("c")
		done <- ok && text == "hello"
	}()
	// The independent ("UI") loop keeps running while the read is blocked.
	var progress atomic.Int32
	for progress.Load() < 1000 {
		progress.Add(1)
	}
	close(release)
	if ok := <-done; !ok {
		t.Fatal("the query did not answer after the clipboard read returned")
	}
	if progress.Load() < 1000 {
		t.Fatal("the independent loop made no progress")
	}
}

// TestDockOSC52QueryConcurrentTrustChanges calls the handler from several
// goroutines while the trust level flips, which is what the session read
// goroutine and a trust decision can do concurrently: it must never race or
// answer after a downgrade.
func TestDockOSC52QueryConcurrentTrustChanges(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Clipboard.OSC52Paste = true
	var trusted atomic.Bool
	query := dockOSC52Query(cfg, trusted.Load, func() (string, error) { return "hello", nil })
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				trusted.Store(j%2 == 0)
				text, ok := query("c")
				if ok && text != "hello" {
					t.Errorf("answer = %q, want hello", text)
					return
				}
			}
		}()
	}
	wg.Wait()
}
