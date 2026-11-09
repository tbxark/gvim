package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tbxark/gvim/finder"
	"github.com/tbxark/gvim/internal/tui/searchpanel"
	"github.com/tbxark/gvim/plugin/hostos"
)

// waitFinderDone waits for one real finder enumeration to finish.
func waitFinderDone(t *testing.T, idx *finder.Index) {
	t.Helper()
	select {
	case <-idx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("finder enumeration did not finish")
	}
}

// finderStateRequest builds the AllFiles request from the real finder capability state.
func finderStateRequest(root, query string, files []string, st searchpanel.FileIndexState) searchpanel.SearchRequest {
	return searchpanel.SearchRequest{
		Root: root, Query: query, Scope: searchpanel.AllFiles,
		Files: files, FilesKnown: st.Known, FilesReady: st.Ready,
		FilesUnknown: st.Unknown, FilesIdle: st.Idle, FilesTruncated: st.Truncated,
		FilesClosed: st.Closed, FilesError: st.Error, FilesNotice: st.Notice,
	}
}

// TestRemoteSearchRealFinderStates checks the optional finder capability end to end: a
// ready empty index is authoritative zero results, while Idle, failed and loading indexes
// permit the bounded fallback and carry an explicit incomplete notice.
func TestRemoteSearchRealFinderStates(t *testing.T) {
	ctx := context.Background()
	// Ready, genuinely empty index.
	ready := finder.StartFunc(ctx, func(context.Context, func(string) bool) error { return nil })
	t.Cleanup(ready.Close)
	waitFinderDone(t, ready)
	files, st := searchpanel.SnapshotWithState(ready)
	if !st.Known || !st.Ready || st.Idle || st.Error != "" || len(files) != 0 {
		t.Fatalf("ready empty state = %+v files %v", st, files)
	}
	noTool := &fakeOneShot{script: func(context.Context, []string, hostos.ProcOptions) (hostos.ProcResult, error) {
		return hostos.ProcResult{}, errors.New("ready-empty search must not run a tool")
	}}
	s := newRemoteProjectSearch(noTool, "/remote/w")
	batches := collectRemoteBatches(t, s.ProjectSearchBatches(finderStateRequest("/remote/w", "todo", files, st), nil))
	if names := remoteSearchFileNames(t, batches); len(names) != 0 {
		t.Fatalf("ready-empty files = %v, want none", names)
	}
	if calls := noTool.recorded(); len(calls) != 0 {
		t.Fatalf("ready-empty search ran remote commands: %v", calls)
	}

	// Idle placeholder: not authoritative; bounded fallback results + notice.
	idle := finder.Idle()
	t.Cleanup(idle.Close)
	files, st = searchpanel.SnapshotWithState(idle)
	if !st.Known || st.Ready || !st.Idle {
		t.Fatalf("idle state = %+v", st)
	}
	one := &fakeOneShot{script: toolScript("rg", "src/a.go:1:1:todo a\n")}
	batches = collectRemoteBatches(t, newRemoteProjectSearch(one, "/remote/w").ProjectSearchBatches(finderStateRequest("/remote/w", "todo", files, st), nil))
	if names := remoteSearchFileNames(t, batches); len(names) != 1 || names[0] != "src/a.go" {
		t.Fatalf("idle fallback files = %v, want the fallback result", names)
	}
	if done := doneBatch(t, batches); done == nil || !strings.Contains(done.Notice, "remote file index is not ready") {
		t.Fatalf("idle Done = %+v, want the idle fallback notice", done)
	}

	// A failed enumeration: fallback + the failure reason, never a complete-looking empty list.
	failed := finder.StartFunc(ctx, func(_ context.Context, emit func(string) bool) error {
		emit("only.go")
		return errors.New("enumeration failed")
	})
	t.Cleanup(failed.Close)
	waitFinderDone(t, failed)
	files, st = searchpanel.SnapshotWithState(failed)
	if !st.Known || st.Ready || st.Error == "" || !strings.Contains(st.Error, "enumeration failed") {
		t.Fatalf("failed state = %+v", st)
	}
	one = &fakeOneShot{script: toolScript("rg", "src/a.go:1:1:todo a\n")}
	batches = collectRemoteBatches(t, newRemoteProjectSearch(one, "/remote/w").ProjectSearchBatches(finderStateRequest("/remote/w", "todo", files, st), nil))
	if names := remoteSearchFileNames(t, batches); len(names) != 1 || names[0] != "src/a.go" {
		t.Fatalf("failed fallback files = %v, want the fallback result", names)
	}
	if done := doneBatch(t, batches); done == nil || !strings.Contains(done.Notice, "file index failed") || !strings.Contains(done.Notice, "enumeration failed") {
		t.Fatalf("failed Done = %+v, want the failure fallback notice", done)
	}

	// Loading index: not authoritative; bounded fallback + loading notice.
	loading := finder.StartFunc(ctx, func(ctx context.Context, _ func(string) bool) error {
		<-ctx.Done()
		return ctx.Err()
	})
	t.Cleanup(loading.Close)
	files, st = searchpanel.SnapshotWithState(loading)
	if !st.Known || st.Ready || st.Idle {
		t.Fatalf("loading state = %+v", st)
	}
	one = &fakeOneShot{script: toolScript("rg", "src/a.go:1:1:todo a\n")}
	batches = collectRemoteBatches(t, newRemoteProjectSearch(one, "/remote/w").ProjectSearchBatches(finderStateRequest("/remote/w", "todo", files, st), nil))
	if done := doneBatch(t, batches); done == nil || !strings.Contains(done.Notice, "while the file index is loading") {
		t.Fatalf("loading Done = %+v, want the loading fallback notice", done)
	}
}

// TestRemoteSearchFinderRestartAndCloseAreNeverReady: a Restart's new generation is
// loading (not an authoritative empty snapshot), and a closed index is never ready even
// if its last snapshot was complete; both permit only the bounded fallback.
func TestRemoteSearchFinderRestartAndCloseAreNeverReady(t *testing.T) {
	ctx := context.Background()
	var calls atomic.Int32
	release := make(chan struct{})
	idx := finder.StartFunc(ctx, func(ctx context.Context, emit func(string) bool) error {
		if calls.Add(1) == 1 {
			emit("a.go")
			return nil
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	t.Cleanup(idx.Close)
	waitFinderDone(t, idx)
	_, st := searchpanel.SnapshotWithState(idx)
	if !st.Ready {
		t.Fatalf("initial state = %+v, want ready", st)
	}
	idx.Restart()
	_, st = searchpanel.SnapshotWithState(idx)
	if st.Ready || st.Closed {
		t.Fatalf("restart state = %+v, want a non-authoritative loading generation", st)
	}
	close(release)
	waitFinderDone(t, idx)
	_, st = searchpanel.SnapshotWithState(idx)
	if !st.Ready {
		t.Fatalf("post-restart state = %+v, want ready after the new generation completes", st)
	}
	idx.Close()
	var files []string
	files, st = searchpanel.SnapshotWithState(idx)
	if !st.Closed || st.Ready {
		t.Fatalf("closed state = %+v, want closed and non-ready", st)
	}
	one := &fakeOneShot{script: toolScript("rg", "src/a.go:1:1:todo a\n")}
	batches := collectRemoteBatches(t, newRemoteProjectSearch(one, "/remote/w").ProjectSearchBatches(finderStateRequest("/remote/w", "todo", files, st), nil))
	if done := doneBatch(t, batches); done == nil || !strings.Contains(done.Notice, "file index was closed") {
		t.Fatalf("closed Done = %+v, want the closed fallback notice", done)
	}
}

// TestRemoteSearchFinderDynamicNoticeIsSanitizedAndBounded: the finder's error/notice
// text is untrusted display data; the fallback notice strips control/bidi bytes and
// bounds the detail before it reaches the panel.
func TestRemoteSearchFinderDynamicNoticeIsSanitizedAndBounded(t *testing.T) {
	hostile := "boom\n\x1b[31m" + strings.Repeat("x", 500) + "\u202e"
	ctx := context.Background()
	idx := finder.StartFuncNotify(ctx, func(_ context.Context, emit func(string) bool, notify func(string)) error {
		emit("only.go")
		notify(hostile)
		return errors.New(hostile)
	})
	t.Cleanup(idx.Close)
	waitFinderDone(t, idx)
	_, st := searchpanel.SnapshotWithState(idx)
	if !st.Known || st.Ready || st.Error == "" || st.Notice == "" {
		t.Fatalf("hostile state = %+v, want a failed non-ready state with notice", st)
	}
	one := &fakeOneShot{script: toolScript("rg", "src/a.go:1:1:todo a\n")}
	batches := collectRemoteBatches(t, newRemoteProjectSearch(one, "/remote/w").ProjectSearchBatches(finderStateRequest("/remote/w", "todo", nil, st), nil))
	done := doneBatch(t, batches)
	if done == nil || done.Notice == "" {
		t.Fatalf("hostile Done = %+v, want a fallback notice", done)
	}
	if strings.ContainsAny(done.Notice, "\n\r\x1b\u202e") || strings.Contains(done.Notice, strings.Repeat("x", 200)) {
		t.Fatalf("notice = %q, want sanitized/bounded dynamic text", done.Notice)
	}
	if len([]rune(done.Notice)) > 500 {
		t.Fatalf("notice runes = %d, want a bounded notice", len([]rune(done.Notice)))
	}
}
