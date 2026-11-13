package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestSharedWaitStaysWithItsOwnerWorkspace: a wait opened in A is still A's after a switch
// to B, is not visible to B, and its completion resumes in A's model.
func TestSharedWaitStaysWithItsOwnerWorkspace(t *testing.T) {
	a := statusViewModel(t, nil)
	b := statusViewModel(t, nil)
	shared := NewPluginShared()
	rt := newFakeRuntime(t)
	a.plug.rt, a.plug.shared, a.plug.host = rt, shared, shared.host
	b.plug.rt, b.plug.shared, b.plug.host = rt, shared, shared.host
	shared.Bind(a)
	if err := a.plugWait(7, waitInput); err != nil {
		t.Fatal(err)
	}
	if shared.waitOwner(7) != a {
		t.Fatal("A does not own its wait")
	}

	// Switch to B: the wait, its overlay and its table stay in A.
	shared.Bind(b)
	if shared.waitOwner(7) != a {
		t.Fatal("the switch moved the wait to another workspace")
	}
	if _, ok := b.plug.waits[7]; ok {
		t.Fatal("B sees a wait it never opened")
	}
	if kind, ok := a.plug.waits[7]; !ok || kind != waitInput {
		t.Fatalf("A lost its wait: %+v", a.plug.waits)
	}
	// The runtime token cannot be claimed twice across workspaces.
	if err := b.plugWait(7, waitInput); err == nil || !strings.Contains(err.Error(), "another workspace") {
		t.Fatalf("cross-workspace duplicate token error = %v", err)
	}

	// A's completion (when A is shown again) resumes the plugin in A.
	shared.Bind(a)
	cmd := a.plugFinish(pluginapi.Completion{Token: 7, Values: []any{"yes"}})
	if cmd != nil {
		cmd()
	}
	if len(rt.resolved) != 1 || rt.resolved[0].Token != 7 {
		t.Fatalf("runtime resolved = %+v", rt.resolved)
	}
	if shared.waitOwner(7) != nil {
		t.Fatal("the registry kept a finished wait")
	}
}

// TestSharedWorkspaceCloseCancelsItsWaits: closing the workspace that owns a plugin prompt
// resolves it as cancelled (with a notice) instead of leaking the plugin task.
func TestSharedWorkspaceCloseCancelsItsWaits(t *testing.T) {
	a := statusViewModel(t, nil)
	b := statusViewModel(t, nil)
	shared := NewPluginShared()
	rt := newFakeRuntime(t)
	a.plug.rt, a.plug.shared, a.plug.host = rt, shared, shared.host
	b.plug.rt, b.plug.shared, b.plug.host = rt, shared, shared.host
	shared.Bind(b) // B is active while A closes
	if err := a.plugWait(9, waitConfirm); err != nil {
		t.Fatal(err)
	}
	if err := b.plugWait(10, waitInput); err != nil {
		t.Fatal(err)
	}
	a.cancelSharedPluginWaits()
	if len(rt.resolved) != 1 || rt.resolved[0].Token != 9 || !strings.Contains(rt.resolved[0].Err, string(pluginapi.CodeCancelled)) {
		t.Fatalf("runtime resolved = %+v, want A's token 9 cancelled", rt.resolved)
	}
	if shared.waitOwner(9) != nil {
		t.Fatal("the closed workspace's wait stayed registered")
	}
	if shared.waitOwner(10) != b {
		t.Fatal("cancelling A's waits touched B's wait")
	}
	if len(rt.resolved) != 1 || rt.resolved[0].Token == 10 {
		t.Fatal("B's wait was resolved by A's close")
	}
}

// waitOwner returns the model that owns t, or nil.
func (s *PluginShared) waitOwner(t pluginapi.Token) *Model {
	waits := sharedWaits(s)
	waitRegistry.mu.Lock()
	defer waitRegistry.mu.Unlock()
	return waits[t]
}
