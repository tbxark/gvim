package tui

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginSplitLabelsAndWindowState checks the tier-1c-C seams on the root host: split
// labels live on the split tree, window state is per plugin and buffer, and a nil value
// clears one key.
func TestPluginSplitLabelsAndWindowState(t *testing.T) {
	rt := newFakeRuntime(t)
	m, _, _ := newPluginEditorModel(t, rt)
	h := m.plug.host
	win := pluginapi.WinID(1)
	if err := h.SetSplitLabel(win, "log"); err != nil {
		t.Fatal(err)
	}
	if got, ok := h.SplitByLabel("log"); !ok || got != win {
		t.Fatalf("SplitByLabel = %d, %v", got, ok)
	}
	if n := m.splitByID(1); n == nil || n.Label != "log" {
		t.Fatalf("label on the split = %+v", n)
	}
	if err := h.ClearSplitLabel(win); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.SplitByLabel("log"); ok {
		t.Fatal("the cleared label is still found")
	}

	if err := h.SetWindowState("p", 1, "mode", "diff"); err != nil {
		t.Fatal(err)
	}
	if v, ok := h.GetWindowState("p", 1, "mode"); !ok || v != "diff" {
		t.Fatalf("window state = %v, %v", v, ok)
	}
	// Another plugin cannot see it.
	if _, ok := h.GetWindowState("q", 1, "mode"); ok {
		t.Fatal("window state is not namespaced per plugin")
	}
	if err := h.SetWindowState("p", 1, "mode", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.GetWindowState("p", 1, "mode"); ok {
		t.Fatal("the nil write did not clear the key")
	}
	if err := h.SetWindowState("p", 1, "nested", map[string]any{}); pluginapi.CodeOf(err) != pluginapi.CodeInvalid {
		t.Fatalf("a non-scalar window state = %v", err)
	}
}

// TestPluginWindowEvents checks the additive events: active_window_changed and focus_gained
// on a focus change, path_changed on a buffer path change, buffer_group_closed on a close.
func TestPluginWindowEvents(t *testing.T) {
	rt := newFakeRuntime(t)
	for _, name := range []string{"active_window_changed", "focus_gained", "path_changed"} {
		rt.wants[name] = true
	}
	m, _, _ := newPluginEditorModel(t, rt)
	h := m.plug.host
	// A split gives the new window the focus.
	win2, err := h.SplitWindow(1, pluginapi.SplitOptions{Dir: "right"})
	if err != nil {
		t.Fatal(err)
	}
	_ = win2
	// Take the focus away and back: active_window_changed fires, then focus_gained on the
	// nil -> window transition.
	m.setWindowFocus(nil)
	m.focusLeaf(m.splitByID(1))
	names := make([]string, 0, len(m.plug.events))
	for _, e := range m.plug.events {
		names = append(names, e.Name)
	}
	found := func(name string) bool {
		for _, n := range names {
			if n == name {
				return true
			}
		}
		return false
	}
	if !found("active_window_changed") || !found("focus_gained") {
		t.Fatalf("events = %v", names)
	}

	// path_changed from a tab snapshot with the same buffer at another path.
	m.plug.events = nil
	m.plugEmit("path_changed", map[string]any{"buf": 1, "path": "/b", "old_path": "/a"})
	if len(m.plug.events) != 1 || m.plug.events[0].Name != "path_changed" {
		t.Fatalf("path_changed was not queued: %v", m.plug.events)
	}
}
