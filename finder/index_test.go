package finder

import (
	"context"
	"errors"
	"fmt"
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
