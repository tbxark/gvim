package app

import (
	"sync"
	"testing"
	"time"
)

// TestOpenTargetScansTabsOnTheUIGoroutine checks the open-file request, which arrives on
// the daemon's socket goroutine, reads the runtimes' tabs on the program's UI goroutine:
// with the UI goroutine busy it waits instead of reading editor state concurrently.
func TestOpenTargetScansTabsOnTheUIGoroutine(t *testing.T) {
	m, _, modelB, _ := setupHiddenWorkspace(t)
	m.mu.Lock()
	active := m.sessions[m.active]
	m.mu.Unlock()
	path := extractionFile(t, active.ws.File.Root, "owned.txt", "x\n")
	openInModel(t, modelB, path)
	host := startHostForTeardownTest(t, m, modelB)

	entered := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	go host.Do(func() {
		close(entered)
		<-release
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the UI goroutine did not run the blocking call")
	}

	done := make(chan struct{})
	go func() {
		m.openTarget(workspaceOpenTarget{path: path})
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("openTarget read the tabs while the UI goroutine was busy")
	case <-time.After(300 * time.Millisecond):
	}
	unblock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("openTarget did not finish once the UI goroutine was free")
	}
}

// TestSharedBufferLabelTakesTheManagerLock checks the label callback the shared-buffer
// registry uses reads the session map under the manager's lock: an editor may call it while
// a switch adds or drops a session.
func TestSharedBufferLabelTakesTheManagerLock(t *testing.T) {
	m, id := newWorkspaceManagerTest(t)
	if m.shared == nil || m.shared.Label == nil {
		t.Fatal("a local daemon manager has no shared-buffer label")
	}
	m.mu.Lock()
	ed := m.sessions[id].rt.ed
	label := m.sessions[id].ws.File.Label
	got := make(chan string, 1)
	go func() { got <- m.shared.Label(ed) }()
	select {
	case <-got:
		m.mu.Unlock()
		t.Fatal("the label was read without the manager lock")
	case <-time.After(100 * time.Millisecond):
	}
	m.mu.Unlock()
	select {
	case name := <-got:
		if name != label {
			t.Fatalf("label = %q, want %q", name, label)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the label read did not finish")
	}
}
