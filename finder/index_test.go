package finder

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeEnum returns an enumeration that emits n paths and honours emit's stop.
func fakeEnum(n int) func(context.Context, func(string) bool) error {
	return func(ctx context.Context, emit func(string) bool) error {
		for i := range n {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !emit(fmt.Sprintf("p%06d.go", i)) {
				return nil
			}
		}
		return nil
	}
}

func TestStartWithTruncatesAtTheCap(t *testing.T) {
	x := startWith(context.Background(), fakeEnum(MaxFiles+7))
	defer x.Close()
	<-x.Done()
	files, loading := x.Snapshot()
	if loading {
		t.Fatal("still loading after Done")
	}
	if len(files) != MaxFiles {
		t.Fatalf("len = %d, want the cap %d", len(files), MaxFiles)
	}
	if !x.Truncated() {
		t.Fatal("Truncated is false past the cap")
	}
	if x.Err() != nil {
		t.Fatalf("Err = %v, want nil for a capped run", x.Err())
	}
}

func TestStartWithAtTheCapIsNotTruncated(t *testing.T) {
	x := startWith(context.Background(), fakeEnum(MaxFiles))
	defer x.Close()
	<-x.Done()
	if files, _ := x.Snapshot(); len(files) != MaxFiles || x.Truncated() {
		t.Fatalf("len = %d, Truncated = %v; exactly the cap is not truncation", len(files), x.Truncated())
	}
}

func TestCloseCancelsAPendingEnumeration(t *testing.T) {
	started := make(chan struct{})
	x := startWith(context.Background(), func(ctx context.Context, emit func(string) bool) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})
	<-started
	done := make(chan struct{})
	go func() {
		x.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not return: the enumeration ignored cancellation")
	}
	if !errors.Is(x.Err(), context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", x.Err())
	}
	if x.Loading() {
		t.Fatal("Loading after Close")
	}
}

func TestParentContextCancelStopsTheIndex(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	x := startWith(ctx, func(ctx context.Context, emit func(string) bool) error {
		<-ctx.Done()
		return ctx.Err()
	})
	defer x.Close()
	cancel()
	select {
	case <-x.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("cancelling the parent context did not stop the index")
	}
	if !errors.Is(x.Err(), context.Canceled) {
		t.Fatalf("Err = %v, want context.Canceled", x.Err())
	}
}

// TestSnapshotWhileFilling reads the index as it grows. When Snapshot reports
// that enumeration is over, the list must hold every file, and the race detector
// must see no conflict between the reader and add.
func TestSnapshotWhileFilling(t *testing.T) {
	const total = 20000
	x := startWith(context.Background(), fakeEnum(total))
	defer x.Close()
	for {
		files, loading := x.Snapshot()
		if !loading {
			if len(files) != total {
				t.Fatalf("finished with %d of %d files in the snapshot", len(files), total)
			}
			return
		}
		_ = files[:len(files):len(files)]
	}
}

// TestRestartReplacesTheList checks Restart keeps the same index pointer: the old run's
// list is discarded, waiting on Done after the restart sees the new run, and the new files
// are the only ones exposed.
func TestRestartReplacesTheList(t *testing.T) {
	calls := 0
	var mu sync.Mutex
	x := startWith(context.Background(), func(ctx context.Context, emit func(string) bool) error {
		mu.Lock()
		calls++
		run := calls
		mu.Unlock()
		if run == 1 {
			emit("old.go")
		} else {
			emit("new.go")
		}
		return nil
	})
	<-x.Done()
	if files, _ := x.Snapshot(); len(files) != 1 || files[0] != "old.go" {
		t.Fatalf("first run = %v", files)
	}
	x.Restart()
	<-x.Done()
	files, loading := x.Snapshot()
	if loading || len(files) != 1 || files[0] != "new.go" {
		t.Fatalf("after restart = %v (loading %v), want new.go", files, loading)
	}
	if err := x.Err(); err != nil {
		t.Fatalf("Err after restart = %v", err)
	}
	x.Close()
}

// TestRestartWhileLoadingDropsTheStaleRun checks a restart during an enumeration cancels
// and waits for it, reports loading from the restart on, and a stale emit cannot land.
func TestRestartWhileLoadingDropsTheStaleRun(t *testing.T) {
	firstStarted := make(chan struct{})
	release := make(chan struct{})
	secondDone := make(chan struct{})
	x := startWith(context.Background(), func(ctx context.Context, emit func(string) bool) error {
		select {
		case <-firstStarted:
			// second run
			emit("fresh.go")
			close(secondDone)
			return nil
		default:
		}
		close(firstStarted)
		if !emit("stale.go") {
			return nil
		}
		<-release
		// A stale emit after the restart must be ignored.
		emit("stale-after-restart.go")
		return nil
	})
	<-firstStarted
	// Restart runs in the background of the test; it blocks waiting for the stale run,
	// so release that run after the call starts.
	restarted := make(chan struct{})
	go func() {
		close(restarted)
		x.Restart()
	}()
	<-restarted
	// The new loading state must be visible before the stale run is released.
	deadline := time.After(5 * time.Second)
	for x.Loading() == false {
		select {
		case <-deadline:
			t.Fatal("Restart never reported loading")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	close(release)
	select {
	case <-secondDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the restarted run did not run")
	}
	<-x.Done()
	files, loading := x.Snapshot()
	if loading {
		t.Fatal("still loading after the restarted run finished")
	}
	for _, f := range files {
		if f == "stale.go" || f == "stale-after-restart.go" {
			t.Fatalf("a stale file survived the restart: %v", files)
		}
	}
	if len(files) != 1 || files[0] != "fresh.go" {
		t.Fatalf("files = %v, want fresh.go", files)
	}
	x.Close()
}

// TestRestartAfterCloseIsANoOp checks a closed index is not revived.
func TestRestartAfterCloseIsANoOp(t *testing.T) {
	calls := 0
	x := startWith(context.Background(), func(ctx context.Context, emit func(string) bool) error {
		calls++
		return nil
	})
	<-x.Done()
	x.Close()
	x.Restart()
	if calls != 1 {
		t.Fatalf("Restart after Close ran the enumeration %d times", calls)
	}
	if x.Err() != nil {
		t.Fatalf("Err = %v", x.Err())
	}
}

// TestRestartIsRaceFree polls the readers while restarts run; the race detector must see a
// coherent index (this is the palette loading-poll pattern).
func TestRestartIsRaceFree(t *testing.T) {
	x := startWith(context.Background(), func(ctx context.Context, emit func(string) bool) error {
		for i := 0; i < 50; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			emit("p.go")
		}
		return nil
	})
	defer x.Close()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			files, loading := x.Snapshot()
			_ = files
			_ = loading
			_ = x.Loading()
			_ = x.Truncated()
			_ = x.Err()
		}
	}()
	for i := 0; i < 20; i++ {
		x.Restart()
		<-x.Done()
	}
	close(stop)
	wg.Wait()
}
