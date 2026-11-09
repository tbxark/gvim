package app

import (
	"errors"
	"testing"

	"github.com/tbxark/gvim/editor"
)

// TestCloseQuiesceFailureKeepsOwnership: quiesce runs BEFORE the irreversible
// shared-buffer DropEditor. A host.Do timeout (simulated) must return an error, keep the
// workspace materialized, and leave the ownership drop uncalled.
func TestCloseQuiesceFailureKeepsOwnership(t *testing.T) {
	m, _ := newWorkspaceManagerTest(t)
	target := createDormant(t, m)
	materializeWorkspace(t, m, target.File.StableID)

	dropped := false
	m.dropEditorFn = func(*editor.Editor) { dropped = true }
	m.quiesceFn = func(*workspaceSession) error { return errors.New("host.Do timeout") }

	if err := m.closeWorkspace(target.File.StableID); err == nil {
		t.Fatal("close continued after a quiesce failure")
	}
	if dropped {
		t.Fatal("close dropped shared-buffer ownership before quiesce completed")
	}
	m.mu.Lock()
	sess := m.sessions[target.File.StableID]
	m.mu.Unlock()
	if sess == nil {
		t.Fatal("close removed the workspace after a quiesce failure")
	}
	if sess.rt == nil || sess.rt.model.Closed() {
		t.Fatal("the retained workspace runtime was left half-closed")
	}
}

// TestEvictQuiesceFailureKeepsOwnership: same ordering for eviction: quiesce before
// DropEditor/rt.exit; failure keeps the warm workspace and its ownership.
func TestEvictQuiesceFailureKeepsOwnership(t *testing.T) {
	m, activeID := newWorkspaceManagerTest(t)
	target := createDormant(t, m)
	materializeWorkspace(t, m, target.File.StableID)
	m.mu.Lock()
	sess := m.sessions[target.File.StableID]
	m.mu.Unlock()
	// Switch back so the target is a non-active eviction candidate.
	materializeWorkspace(t, m, activeID)

	dropped := false
	m.dropEditorFn = func(*editor.Editor) { dropped = true }
	m.quiesceFn = func(*workspaceSession) error { return errors.New("host.Do timeout") }
	kept, err := m.evictWorkspace(sess, m.materializedCap)
	if !kept || err == nil {
		t.Fatalf("evictWorkspace = kept %v err %v, want kept with an error", kept, err)
	}
	if dropped {
		t.Fatal("eviction dropped shared-buffer ownership before quiesce completed")
	}
	m.mu.Lock()
	retained := m.sessions[target.File.StableID]
	m.mu.Unlock()
	if retained == nil || retained.rt == nil || retained.rt.model.Closed() {
		t.Fatal("eviction removed or half-closed the workspace after a quiesce failure")
	}
}
