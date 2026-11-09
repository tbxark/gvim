package finder

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// snapshotWithin fails unless a short IndexSnapshot call completes promptly.
func snapshotWithin(t *testing.T, idx *Index) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _, _, _, _, _, _ = idx.IndexSnapshot()
	}()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("IndexSnapshot blocked behind a Restart/Close waiting for oldDone")
	}
}

// TestIndexSnapshotDoesNotWaitForBlockedRestart: Restart cancels and waits for the old
// enumeration while holding lifecycle, but IndexSnapshot uses only mu; it must return
// immediately with a non-ready new generation.
func TestIndexSnapshotDoesNotWaitForBlockedRestart(t *testing.T) {
	ctx := context.Background()
	var started, cancelled sync.Once
	startedCh, cancelledCh := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	idx := StartFunc(ctx, func(ctx context.Context, _ func(string) bool) error {
		started.Do(func() { close(startedCh) })
		<-ctx.Done()
		cancelled.Do(func() { close(cancelledCh) })
		<-release
		return ctx.Err()
	})
	t.Cleanup(idx.Close)
	<-startedCh
	restarted := make(chan struct{})
	go func() {
		idx.Restart()
		close(restarted)
	}()
	<-cancelledCh // Restart is now waiting for oldDone.
	_, loading, ready, _, _, closed, _, _ := idx.IndexSnapshot()
	if !loading || ready || closed {
		t.Fatalf("snapshot during restart = loading %v ready %v closed %v, want loading/non-ready/open", loading, ready, closed)
	}
	snapshotWithin(t, idx)
	close(release)
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Restart did not finish after the old enumeration stopped")
	}
}

// TestIndexSnapshotCallbackReentryWhileRestartWaits: the enumeration goroutine itself can
// call IndexSnapshot while another goroutine's Restart is waiting for it; mu-only
// snapshotting must not self-deadlock.
func TestIndexSnapshotCallbackReentryWhileRestartWaits(t *testing.T) {
	ctx := context.Background()
	var idx *Index
	assigned := make(chan struct{})
	started, cancelled, snapped := make(chan struct{}), make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	idx = StartFunc(ctx, func(ctx context.Context, _ func(string) bool) error {
		<-assigned
		if calls.Add(1) != 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		close(started)
		<-ctx.Done()
		close(cancelled)
		_, _, _, _, _, _, _, _ = idx.IndexSnapshot() // callback reentry from the enumeration goroutine
		close(snapped)
		<-release
		return ctx.Err()
	})
	close(assigned)
	t.Cleanup(idx.Close)
	<-started
	restarted := make(chan struct{})
	go func() {
		idx.Restart()
		close(restarted)
	}()
	<-cancelled
	select {
	case <-snapped:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("callback reentry IndexSnapshot deadlocked behind Restart")
	}
	close(release)
	select {
	case <-restarted:
	case <-time.After(2 * time.Second):
		t.Fatal("Restart did not finish after callback reentry")
	}
}

// TestIndexSnapshotDoesNotWaitForBlockedClose: Close marks the generation closed under mu
// and then waits for the enumeration; IndexSnapshot must return closed/non-ready at once.
func TestIndexSnapshotDoesNotWaitForBlockedClose(t *testing.T) {
	ctx := context.Background()
	var started sync.Once
	startedCh, cancelledCh := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	closeRelease := func() { releaseOnce.Do(func() { close(release) }) }
	idx := StartFunc(ctx, func(ctx context.Context, _ func(string) bool) error {
		started.Do(func() { close(startedCh) })
		<-ctx.Done()
		close(cancelledCh)
		<-release
		return ctx.Err()
	})
	t.Cleanup(closeRelease)
	<-startedCh
	closed := make(chan struct{})
	go func() {
		idx.Close()
		close(closed)
	}()
	<-cancelledCh
	_, loading, ready, _, _, isClosed, _, _ := idx.IndexSnapshot()
	if !isClosed || ready {
		t.Fatalf("snapshot during close = loading %v ready %v closed %v, want closed/non-ready", loading, ready, isClosed)
	}
	snapshotWithin(t, idx)
	closeRelease()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not finish after the enumeration stopped")
	}
}
