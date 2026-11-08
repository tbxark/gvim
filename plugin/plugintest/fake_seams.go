package plugintest

import (
	"github.com/tbxark/gvim/pluginapi"
)

// The tier-1c-C host seams of the fake: split labels and per-plugin window state.

func (f *FakeHost) splitLabels() map[pluginapi.WinID]string {
	m, _ := f.Ext["splitlabels"].(map[pluginapi.WinID]string)
	if m == nil {
		m = map[pluginapi.WinID]string{}
		f.Ext["splitlabels"] = m
	}
	return m
}

// SetSplitLabel labels a split window.
func (f *FakeHost) SetSplitLabel(w pluginapi.WinID, label string) error {
	if f.Window(w) == nil {
		return winClosedErr(w)
	}
	f.rec("SetSplitLabel", w, label)
	f.splitLabels()[w] = label
	return nil
}

// ClearSplitLabel removes a window's label.
func (f *FakeHost) ClearSplitLabel(w pluginapi.WinID) error {
	if f.Window(w) == nil {
		return winClosedErr(w)
	}
	f.rec("ClearSplitLabel", w)
	delete(f.splitLabels(), w)
	return nil
}

// SplitByLabel returns the window with a label.
func (f *FakeHost) SplitByLabel(label string) (pluginapi.WinID, bool) {
	f.rec("SplitByLabel", label)
	for w, l := range f.splitLabels() {
		if l == label {
			return w, true
		}
	}
	return 0, false
}

// SplitLabel returns the label of a window (a test helper).
func (f *FakeHost) SplitLabel(w pluginapi.WinID) string { return f.splitLabels()[w] }

// fakeStateKey is one plugin window-state key of the fake host.
type fakeStateKey struct {
	Plugin string
	Buf    pluginapi.BufferID
	Key    string
}

func (f *FakeHost) windowState() map[fakeStateKey]any {
	m, _ := f.Ext["windowstate"].(map[fakeStateKey]any)
	if m == nil {
		m = map[fakeStateKey]any{}
		f.Ext["windowstate"] = m
	}
	return m
}

// GetWindowState reads one plugin window-state key.
func (f *FakeHost) GetWindowState(plugin string, id pluginapi.BufferID, key string) (any, bool) {
	v, ok := f.windowState()[fakeStateKey{plugin, id, key}]
	return v, ok
}

// SetWindowState writes one plugin window-state key.
func (f *FakeHost) SetWindowState(plugin string, id pluginapi.BufferID, key string, value any) error {
	if f.find(id) == nil {
		return bufClosedErr(id)
	}
	f.rec("SetWindowState", plugin, id, key)
	if value == nil {
		delete(f.windowState(), fakeStateKey{plugin, id, key})
		return nil
	}
	f.windowState()[fakeStateKey{plugin, id, key}] = value
	return nil
}

// ClearWindowState removes one key of a plugin and buffer.
func (f *FakeHost) ClearWindowState(plugin string, id pluginapi.BufferID, key string) {
	f.rec("ClearWindowState", plugin, id, key)
	delete(f.windowState(), fakeStateKey{plugin, id, key})
}

// WindowStateValue returns a window-state value (a test helper).
func (f *FakeHost) WindowStateValue(plugin string, id pluginapi.BufferID, key string) (any, bool) {
	return f.GetWindowState(plugin, id, key)
}
