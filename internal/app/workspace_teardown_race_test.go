package app

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/internal/tui/palette"
)

// startHostForTeardownTest runs a real daemon host program over modelB and returns it
// with a cleanup that stops the program. The manager's foreground model is A by
// construction; the test switches to a dormant B before starting the host, leaving A warm
// and hidden.
func startHostForTeardownTest(t *testing.T, m *workspaceManager, modelB *tui.Model) *tui.WorkspaceHost {
	t.Helper()
	host := tui.NewWorkspaceHost(modelB)
	m.setHost(host)
	inR, inW := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- tui.RunWorkspaceHost(ctx, host, inR, io.Discard, func(*tea.Program) { close(ready) })
	}()
	select {
	case <-ready:
	case <-time.After(10 * time.Second):
		t.Fatal("host program did not start")
	}
	t.Cleanup(func() {
		cancel()
		_ = inW.Close()
		_ = inR.Close()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Logf("host program ended: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Log("host program did not stop within 5s")
		}
	})
	return host
}

// setupHiddenWorkspace returns manager, hidden A's session, active B's model and the
// workspace id of A.
func setupHiddenWorkspace(t *testing.T) (*workspaceManager, *workspaceSession, *tui.Model, string) {
	t.Helper()
	m, idA := newWorkspaceManagerTest(t)
	b := createDormant(t, m)
	materializeWorkspace(t, m, b.File.StableID)
	m.mu.Lock()
	sessA := m.sessions[idA]
	modelB := m.sessions[b.File.StableID].rt.model
	m.mu.Unlock()
	if sessA == nil || modelB == nil {
		t.Fatal("missing workspace runtime")
	}
	return m, sessA, modelB, idA
}

// TestCloseHostRaceHiddenEnvelopeQuiesce checks a real host close of hidden A while A's
// queued command is in flight: quiesce marks A closed on the UI goroutine, the command's
// late result is dropped, teardown completes, and -race sees no model/editor race.
func TestCloseHostRaceHiddenEnvelopeQuiesce(t *testing.T) {
	m, sessA, modelB, idA := setupHiddenWorkspace(t)
	host := startHostForTeardownTest(t, m, modelB)

	started := make(chan struct{})
	release := make(chan struct{})
	host.Do(func() {
		host.Show(modelB)
		host.QueueFor(sessA.rt.model, func() tea.Msg {
			close(started)
			<-release
			return palette.RunCommandMsg{Action: "open_settings"}
		})
	})
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("hidden A command did not start")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- m.closeWorkspace(idA) }()
	time.Sleep(20 * time.Millisecond) // let close reach quiesce/write work
	close(release)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatalf("closeWorkspace: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("closeWorkspace did not finish")
	}
	if !sessA.rt.model.Closed() {
		t.Fatal("quiesced/closed model is not marked closed")
	}
	m.mu.Lock()
	_, still := m.sessions[idA]
	m.mu.Unlock()
	if still {
		t.Fatal("closed workspace is still materialized")
	}
}

// TestCloseHostQuiesceFailureKeepsOwnership checks the failure path with a real host:
// when quiesce cannot be confirmed, close must return before any shared-buffer ownership
// drop or runtime teardown, keeping the workspace and its model intact.
func TestCloseHostQuiesceFailureKeepsOwnership(t *testing.T) {
	m, sessA, modelB, idA := setupHiddenWorkspace(t)
	_ = startHostForTeardownTest(t, m, modelB)

	dropped := false
	m.dropEditorFn = func(*editor.Editor) { dropped = true }
	m.quiesceFn = func(*workspaceSession) error { return errors.New("host.Do timeout") }
	if err := m.closeWorkspace(idA); err == nil {
		t.Fatal("close continued after a host quiesce failure")
	}
	if dropped {
		t.Fatal("shared-buffer ownership dropped before quiesce completed")
	}
	if sessA.rt.model.Closed() {
		t.Fatal("model was closed despite the quiesce failure")
	}
	m.mu.Lock()
	retained := m.sessions[idA]
	m.mu.Unlock()
	if retained == nil {
		t.Fatal("workspace was removed despite the quiesce failure")
	}
}

// TestEvictHostQuiesceFailureKeepsOwnership is the eviction half of the host failure path.
func TestEvictHostQuiesceFailureKeepsOwnership(t *testing.T) {
	m, sessA, modelB, _ := setupHiddenWorkspace(t)
	_ = startHostForTeardownTest(t, m, modelB)

	dropped := false
	m.dropEditorFn = func(*editor.Editor) { dropped = true }
	m.quiesceFn = func(*workspaceSession) error { return errors.New("host.Do timeout") }
	kept, err := m.evictWorkspace(sessA, m.materializedCap)
	if !kept || err == nil {
		t.Fatalf("evictWorkspace = kept %v err %v, want kept with error", kept, err)
	}
	if dropped {
		t.Fatal("shared-buffer ownership dropped before quiesce completed")
	}
	if sessA.rt.model.Closed() {
		t.Fatal("model was closed despite the quiesce failure")
	}
}

// TestHostQuiesceRealTimeoutDiscardsQueuedTeardown is the real 10s host.Do timeout
// regression: with the UI blocked, quiesce times out; unblocking the UI must not run the
// queued MarkClosed/CancelTransientWork/DropEditor closure later, so the retained
// workspace keeps its model and shared-buffer ownership.
func TestHostQuiesceRealTimeoutDiscardsQueuedTeardown(t *testing.T) {
	m, sessA, modelB, _ := setupHiddenWorkspace(t)
	host := startHostForTeardownTest(t, m, modelB)

	m.doTimeout = 100 * time.Millisecond // real host timeout, shortened for the test
	uiEntered, uiRelease := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseUI := func() { releaseOnce.Do(func() { close(uiRelease) }) }
	defer releaseUI()
	blockerDone := make(chan bool, 1)
	go func() {
		blockerDone <- host.Do(func() {
			close(uiEntered)
			<-uiRelease
		})
	}()
	select {
	case <-uiEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("the UI blocker did not run")
	}

	dropped := false
	m.dropEditorFn = func(*editor.Editor) { dropped = true }
	errCh := make(chan error, 1)
	go func() { errCh <- m.quiesceModelForTeardown(sessA) }()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("quiesce reported success while the UI was blocked")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("quiesce did not time out")
	}
	releaseUI()
	<-blockerDone // the original started UI call finishes (Do waits after its timeout)
	// The queued closure is now processed as a canceled hostCallMsg: it must be a no-op.
	time.Sleep(100 * time.Millisecond)
	if sessA.rt.model.Closed() {
		t.Fatal("the timed-out queued MarkClosed ran after the workspace was retained")
	}
	if dropped {
		t.Fatal("the timed-out queued DropEditor ran after quiesce failed")
	}
}
