package tui

import (
	"math"
	"strings"

	"github.com/tbxark/gvim/pluginapi"
)

// The tier-1c-C seams: split labels, per-plugin window state and the focus/window events.
// They live on the root plugin state (not the editor) because they belong to plugins:
// window state survives a plugin reload in-session and is cleared when the buffer closes.

const (
	maxSplitLabel      = 64
	maxViewStateKey    = 128
	maxViewStateKeys   = 256 // per plugin and buffer
	maxViewStateString = 4096
)

// SetSplitLabel names a split window so a plugin can find it again.
func (h *pluginHost) SetSplitLabel(w pluginapi.WinID, label string) error {
	n := h.m.splitByID(uint64(w))
	if n == nil {
		return closedWindow(w)
	}
	label = strings.TrimSpace(label)
	if len(label) > maxSplitLabel {
		return pluginapi.Errf(pluginapi.CodeLimit, "a split label is at most %d bytes", maxSplitLabel)
	}
	n.Label = label
	return nil
}

// ClearSplitLabel removes a split window's label.
func (h *pluginHost) ClearSplitLabel(w pluginapi.WinID) error {
	n := h.m.splitByID(uint64(w))
	if n == nil {
		return closedWindow(w)
	}
	n.Label = ""
	return nil
}

// SplitByLabel returns the window with a label.
func (h *pluginHost) SplitByLabel(label string) (pluginapi.WinID, bool) {
	for _, n := range h.m.windowLeaves() {
		if n.Label == label {
			return pluginapi.WinID(n.ID), true
		}
	}
	return 0, false
}

// windowState returns the key map of one plugin's state on a buffer, creating it when
// create is set.
func (m *Model) windowState(plugin string, id pluginapi.BufferID, create bool) map[string]any {
	if m.plug.viewState == nil {
		if !create {
			return nil
		}
		m.plug.viewState = map[pluginapi.BufferID]map[string]map[string]any{}
	}
	byPlugin := m.plug.viewState[id]
	if byPlugin == nil {
		if !create {
			return nil
		}
		byPlugin = map[string]map[string]any{}
		m.plug.viewState[id] = byPlugin
	}
	keys := byPlugin[plugin]
	if keys == nil && create {
		keys = map[string]any{}
		byPlugin[plugin] = keys
	}
	return keys
}

// GetWindowState reads one plugin window-state key.
func (h *pluginHost) GetWindowState(plugin string, id pluginapi.BufferID, key string) (any, bool) {
	if _, ok := h.ed().Buffer(id); !ok {
		return nil, false
	}
	keys := h.m.windowState(plugin, id, false)
	if keys == nil {
		return nil, false
	}
	v, ok := keys[key]
	return v, ok
}

// SetWindowState writes one plugin window-state key; a nil value removes it.
func (h *pluginHost) SetWindowState(plugin string, id pluginapi.BufferID, key string, value any) error {
	if _, ok := h.ed().Buffer(id); !ok {
		return pluginapi.Errf(pluginapi.CodeClosed, "buffer %d is closed", id)
	}
	if key == "" || len(key) > maxViewStateKey {
		return pluginapi.Errf(pluginapi.CodeInvalid, "a window state key is 1 to %d bytes", maxViewStateKey)
	}
	if value == nil {
		h.ClearWindowState(plugin, id, key)
		return nil
	}
	switch v := value.(type) {
	case string:
		if len(v) > maxViewStateString {
			return pluginapi.Errf(pluginapi.CodeLimit, "a window state string is at most %d bytes", maxViewStateString)
		}
	case bool:
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return pluginapi.Errf(pluginapi.CodeInvalid, "a window state number must be finite")
		}
	case int:
	case int64:
	default:
		return pluginapi.Errf(pluginapi.CodeInvalid, "a window state value is a string, number or boolean")
	}
	keys := h.m.windowState(plugin, id, true)
	if _, exists := keys[key]; !exists && len(keys) >= maxViewStateKeys {
		return pluginapi.Errf(pluginapi.CodeLimit, "a plugin keeps at most %d window state keys per buffer", maxViewStateKeys)
	}
	keys[key] = value
	return nil
}

// ClearWindowState removes one key, or every key of the plugin when key is empty.
func (h *pluginHost) ClearWindowState(plugin string, id pluginapi.BufferID, key string) {
	keys := h.m.windowState(plugin, id, false)
	if keys == nil {
		return
	}
	if key == "" {
		delete(h.m.plug.viewState[id], plugin)
		return
	}
	delete(keys, key)
}

// plugViewStateDrop forgets a closed buffer's window state.
func (m *Model) plugViewStateDrop(id pluginapi.BufferID) {
	delete(m.plug.viewState, id)
}

// plugViewStateDropOwner forgets a plugin's window state on unload.
func (m *Model) plugViewStateDropOwner(plugin string) {
	for id, byPlugin := range m.plug.viewState {
		delete(byPlugin, plugin)
		if len(byPlugin) == 0 {
			delete(m.plug.viewState, id)
		}
	}
}

// setWindowFocus gives the focused leaf to n (nil: no focused pane) and raises the
// tier-1c-C window events: active_window_changed when the window changed, focus_gained
// when the editor gets a focused body pane back.
func (m *Model) setWindowFocus(n *splitNode) {
	prev := m.splits.Focus
	if prev == n {
		return
	}
	// Read the old window's id without allocating one: the previous focus may be the
	// internal split node a new split just made (its id is gone), and IDOf would give it
	// a fresh id and burn the one the new pane should get.
	prevID := uint64(0)
	if prev != nil {
		prevID = prev.ID
	}
	curID := uint64(0)
	if n != nil {
		curID = m.splits.IDOf(n)
	}
	m.splits.Focus = n
	if prevID != curID {
		m.plugEmit("active_window_changed", map[string]any{"previous": prevID, "current": curID})
	}
	if prev == nil && n != nil {
		m.plugEmit("focus_gained", map[string]any{})
	}
}
