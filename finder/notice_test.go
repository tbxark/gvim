package finder

import (
	"context"
	"testing"
	"time"
)

// TestStartFuncNotifyNotice checks the notified enumeration seam: a run may attach one
// non-fatal notice, and a Restart clears it for the new run.
func TestStartFuncNotifyNotice(t *testing.T) {
	runs := 0
	idx := StartFuncNotify(context.Background(), func(_ context.Context, emit func(string) bool, notify func(string)) error {
		runs++
		if runs == 1 {
			notify("used a directory walk")
		}
		if !emit("a.go") {
			return nil
		}
		return nil
	})
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the index did not finish")
	}
	if got := idx.Notice(); got != "used a directory walk" {
		t.Fatalf("notice = %q", got)
	}
	idx.Restart()
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the restarted index did not finish")
	}
	if got := idx.Notice(); got != "" {
		t.Fatalf("notice after a clean restart = %q, want cleared", got)
	}
	if files, _ := idx.Snapshot(); len(files) != 1 || files[0] != "a.go" {
		t.Fatalf("files after restart = %v", files)
	}
}

// TestStartFuncWithoutNotice checks the plain flat-list constructor never invents a notice.
func TestStartFuncWithoutNotice(t *testing.T) {
	idx := StartFunc(context.Background(), func(_ context.Context, emit func(string) bool) error {
		emit("a.go")
		return nil
	})
	select {
	case <-idx.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the index did not finish")
	}
	if got := idx.Notice(); got != "" {
		t.Fatalf("plain index notice = %q", got)
	}
}
