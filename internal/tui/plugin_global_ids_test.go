package tui

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginGlobalIDsAcrossWorkspaces checks the editor-global plugin id namespace: two
// workspaces whose local window and buffer ids collide get distinct global ids, and a
// cached id from the non-active workspace is refused instead of resolving to the colliding
// local id.
func TestPluginGlobalIDsAcrossWorkspaces(t *testing.T) {
	rt := newFakeRuntime(t)
	rt.wants["active_window_changed"] = true
	rt.wants["buffer_activated"] = true
	rt.wants["buffer_deactivated"] = true
	shared := NewPluginShared()
	a, pea := newSharedPluginModel(t, shared, rt)
	a.SetWorkspaceLabel("ws-a")
	shared.Bind(a)
	h := shared.host
	ga := h.ActiveBuffer()
	wa := h.CurrentWindow()
	if ga == 0 || wa == 0 {
		t.Fatalf("workspace A ids = buffer %d window %d", ga, wa)
	}
	if ga == pluginapi.BufferID(pea.active) {
		t.Fatalf("workspace A buffer id %d is still a bare local id", ga)
	}
	winA := h.Windows()
	if len(winA) != 1 || winA[0].ID != wa || winA[0].Buffer != ga {
		t.Fatalf("workspace A windows = %+v", winA)
	}
	_ = queuedEvents(a)

	b, peb := newSharedPluginModel(t, shared, rt)
	b.SetWorkspaceLabel("ws-b")
	shared.Bind(b)
	gb := h.ActiveBuffer()
	wb := h.CurrentWindow()
	if gb == 0 || wb == 0 {
		t.Fatalf("workspace B ids = buffer %d window %d", gb, wb)
	}
	if ga == gb || wa == wb {
		// Both workspaces have local buffer 1 and split window 1.
		t.Fatalf("colliding local ids were not qualified: buffers %d/%d windows %d/%d", ga, gb, wa, wb)
	}
	_ = peb

	// The switch announcement carries the global ids.
	events := queuedEvents(b)
	var windowCurrent, bufferCurrent uint64
	for _, e := range events {
		switch e.Name {
		case "active_window_changed":
			windowCurrent, _ = e.Data["current"].(uint64)
		case "buffer_activated":
			switch v := e.Data["buf"].(type) {
			case pluginapi.BufferID:
				bufferCurrent = uint64(v)
			case uint64:
				bufferCurrent = v
			}
		}
	}
	if windowCurrent != uint64(wb) || bufferCurrent != uint64(gb) {
		t.Fatalf("switch events carry window=%d buffer=%d, want %d/%d: %v", windowCurrent, bufferCurrent, wb, gb, events)
	}

	// A cached id from the leaving workspace is refused with CodeClosed.
	if _, ok := h.Buffer(ga); ok {
		t.Fatalf("a foreign buffer id %d resolved in the active workspace", ga)
	}
	if _, err := h.Lines(ga, 0, 1); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Lines(foreign) = %v, want CodeClosed", err)
	}
	if err := h.FocusWindow(wa); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("FocusWindow(foreign) = %v, want CodeClosed", err)
	}
	if err := h.SetSplitLabel(wa, "x"); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("SetSplitLabel(foreign) = %v, want CodeClosed", err)
	}
	if _, ok := h.GetWindowState("p", ga, "k"); ok {
		t.Fatal("window state of a foreign buffer id resolved")
	}
	if err := h.SetWindowState("p", ga, "k", 1); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("SetWindowState(foreign) = %v, want CodeClosed", err)
	}
	// The active workspace's own ids still work.
	if info, ok := h.Buffer(gb); !ok || info.ID != gb {
		t.Fatalf("Buffer(active) = %+v, %v", info, ok)
	}
	if err := h.FocusWindow(wb); err != nil {
		t.Fatalf("FocusWindow(active) = %v", err)
	}
}

// TestPluginGlobalIDRejectedAfterClose checks a cached global buffer id stops resolving
// once the buffer closes, rather than being handed to a reused local id.
func TestPluginGlobalIDRejectedAfterClose(t *testing.T) {
	rt := newFakeRuntime(t)
	shared := NewPluginShared()
	a, _ := newSharedPluginModel(t, shared, rt)
	shared.Bind(a)
	h := shared.host
	id := h.ActiveBuffer()
	if err := h.Close(id, true); err != nil {
		t.Fatal(err)
	}
	if err := h.Activate(id); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Activate(closed global id) = %v, want CodeClosed", err)
	}
	if _, ok := h.Buffer(id); ok {
		t.Fatalf("a closed global buffer id resolved: %d", id)
	}
}

// TestPluginGlobalIDDirectLaunchUnchanged pins the direct-launch identity: without a shared
// mux the host keeps the workspace-local ids.
func TestPluginGlobalIDDirectLaunchUnchanged(t *testing.T) {
	rt := newFakeRuntime(t)
	m, _, pe := newPluginEditorModel(t, rt)
	if got := m.plug.host.ActiveBuffer(); got != pe.active {
		t.Fatalf("direct ActiveBuffer = %d, want local %d", got, pe.active)
	}
	win := m.plug.host.CurrentWindow()
	if uint64(win) != m.splits.IDOf(m.splits.Focus) {
		t.Fatalf("direct CurrentWindow = %d, want local %d", win, m.splits.IDOf(m.splits.Focus))
	}
}

// TestPluginReleaseWorkspace checks an evicted/closed model's sequence is forgotten and a
// rematerialized model gets a fresh one, so old ids never resolve against the new model.
func TestPluginReleaseWorkspace(t *testing.T) {
	rt := newFakeRuntime(t)
	shared := NewPluginShared()
	a, _ := newSharedPluginModel(t, shared, rt)
	shared.Bind(a)
	old := shared.host.ActiveBuffer()
	if old == 0 {
		t.Fatal("no active global buffer id")
	}
	shared.ReleaseWorkspace(a)
	// Re-binding the same model (the eviction/rematerialize shape is a new model, but this
	// also covers an in-place rebind) hands out a fresh sequence.
	shared.Bind(a)
	fresh := shared.host.ActiveBuffer()
	if fresh == old {
		t.Fatalf("released workspace got the same global buffer id %d", old)
	}
	if err := shared.host.Activate(old); pluginapi.CodeOf(err) != pluginapi.CodeClosed {
		t.Fatalf("Activate(released id) = %v, want CodeClosed", err)
	}
	if got, ok := shared.host.Buffer(fresh); !ok || got.ID != fresh {
		t.Fatalf("Buffer(fresh) = %+v, %v", got, ok)
	}
}
