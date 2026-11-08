package plugintest

import "github.com/tbxark/gvim/pluginapi"

// File explorer decorations and slots of the fake host (task-61).

func explorerSets(f *FakeHost, key string) map[string]any {
	m, _ := f.Ext[key].(map[string]any)
	if m == nil {
		m = map[string]any{}
		f.Ext[key] = m
	}
	return m
}

// SetExplorerDecorations records a namespace's decorated rows.
func (f *FakeHost) SetExplorerDecorations(plugin, ns string, decorations []pluginapi.FileExplorerDecoration) error {
	f.rec("SetExplorerDecorations", plugin, ns, len(decorations))
	explorerSets(f, "explorertdeco")[plugin+"\x00"+ns] = append([]pluginapi.FileExplorerDecoration(nil), decorations...)
	return nil
}

// ClearExplorerDecorations forgets a namespace's decorations.
func (f *FakeHost) ClearExplorerDecorations(plugin, ns string) {
	f.rec("ClearExplorerDecorations", plugin, ns)
	delete(explorerSets(f, "explorertdeco"), plugin+"\x00"+ns)
}

// SetExplorerSlots records a namespace's slot overrides.
func (f *FakeHost) SetExplorerSlots(plugin, ns string, slots []pluginapi.FileExplorerSlot) error {
	f.rec("SetExplorerSlots", plugin, ns, len(slots))
	explorerSets(f, "explorerslots")[plugin+"\x00"+ns] = append([]pluginapi.FileExplorerSlot(nil), slots...)
	return nil
}

// ClearExplorerSlots forgets a namespace's slot overrides.
func (f *FakeHost) ClearExplorerSlots(plugin, ns string) {
	f.rec("ClearExplorerSlots", plugin, ns)
	delete(explorerSets(f, "explorerslots"), plugin+"\x00"+ns)
}

// ExplorerDecorations returns the decorations recorded for (plugin, namespace).
func (f *FakeHost) ExplorerDecorations(plugin, ns string) []pluginapi.FileExplorerDecoration {
	v, _ := explorerSets(f, "explorertdeco")[plugin+"\x00"+ns].([]pluginapi.FileExplorerDecoration)
	return append([]pluginapi.FileExplorerDecoration(nil), v...)
}

// ExplorerSlots returns the slot overrides recorded for (plugin, namespace).
func (f *FakeHost) ExplorerSlots(plugin, ns string) []pluginapi.FileExplorerSlot {
	v, _ := explorerSets(f, "explorerslots")[plugin+"\x00"+ns].([]pluginapi.FileExplorerSlot)
	return append([]pluginapi.FileExplorerSlot(nil), v...)
}
