package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestApplyCommitEvictingNoticeDoesNotReenterDo: applyCommit runs inside a hostCall on the
// UI goroutine. Its eviction-refusal notice must update the active model directly; the old
// m.notify -> h.Notify -> h.Do path would re-enter Do from the UI and freeze it for the Do
// timeout.
func TestApplyCommitEvictingNoticeDoesNotReenterDo(t *testing.T) {
	m, _, modelB, _ := setupHiddenWorkspace(t)
	_ = modelB
	host := startHostForTeardownTest(t, m, modelB)

	const evictingID = "workspace-being-evicted"
	m.mu.Lock()
	m.seq++
	seq := m.seq
	m.pendingID = evictingID
	m.evicting[evictingID] = true
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		host.Do(func() {
			m.applyCommit(seq)
			close(done)
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("applyCommit re-entered Do and blocked the UI callback")
	}
	m.mu.Lock()
	notice := m.lastNotice
	m.mu.Unlock()
	if !strings.Contains(notice, "being evicted") {
		t.Fatalf("eviction-refusal notice = %q", notice)
	}
}

// TestEnqueueCaptureSnapshotFailureDoesNotReenterDo: enqueueCapture runs inside a runOnUI
// closure; its snapshot failure used to call m.notify -> h.Do and freeze the UI. With a
// real host and an injected snapshot error, the capture call must return promptly and set
// the notice directly.
func TestEnqueueCaptureSnapshotFailureDoesNotReenterDo(t *testing.T) {
	m, sessA, modelB, _ := setupHiddenWorkspace(t)
	host := startHostForTeardownTest(t, m, modelB)
	m.snapshotCaptureFn = func(*workspaceSession) (*captureSnapshot, error) {
		return nil, errTestSnapshotFailure
	}
	done := make(chan struct{})
	go func() {
		host.Do(func() {
			m.enqueueCapture(sessA)
			close(done)
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("enqueueCapture snapshot failure re-entered Do and froze the UI")
	}
	m.mu.Lock()
	notice := m.lastNotice
	m.mu.Unlock()
	if !strings.Contains(notice, "snapshot workspace") {
		t.Fatalf("snapshot failure notice = %q", notice)
	}
}

var errTestSnapshotFailure = errors.New("injected snapshot failure")

// TestHostNotifyInsideHostCallDoesNotReenterDo: host.Notify called from a hostCall
// callback must write the active model notice directly; re-entering Do would freeze the
// UI for the timeout.
func TestHostNotifyInsideHostCallDoesNotReenterDo(t *testing.T) {
	m, _, modelB, _ := setupHiddenWorkspace(t)
	host := startHostForTeardownTest(t, m, modelB)
	done := make(chan struct{})
	go func() {
		host.Do(func() {
			host.Notify("notice from a host call")
			close(done)
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("host.Notify re-entered Do and froze the UI callback")
	}
}

// TestHostNotifyFromOtherGoroutinesWhileHostCallRuns: Notify is safe from any goroutine;
// while a UI callback is blocked, concurrent notices are queued (or rejected) and are
// applied only by host Update. Run under -race to catch off-UI model access.
func TestHostNotifyFromOtherGoroutinesWhileHostCallRuns(t *testing.T) {
	m, _, modelB, _ := setupHiddenWorkspace(t)
	host := startHostForTeardownTest(t, m, modelB)
	entered, release := make(chan struct{}), make(chan struct{})
	blockerDone := make(chan struct{})
	go func() {
		host.Do(func() {
			close(entered)
			<-release
		})
		close(blockerDone)
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("blocker host call did not start")
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			host.Notify("notice from another goroutine")
		}()
	}
	wg.Wait()
	close(release)
	select {
	case <-blockerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked host call did not finish")
	}
	// Drain host Update once so any queued notifyMsg is applied on the UI goroutine.
	done := make(chan struct{})
	go func() {
		host.Do(func() { close(done) })
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("final host sync did not finish")
	}
}
