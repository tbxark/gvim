package tui

// statusExtraSource is the structural identity of one status-row Extra item. Plugin status
// items and the native appended segments share the Extra slice, so routing must not depend
// on a plugin-chosen ID string: only entries built here with source statusExtraEnv reach the
// native environment action; a plugin item that happens to be called "env" stays a plugin
// item (its hit index is in the plugin range and its source is statusExtraPlugin).
type statusExtraSource uint8

const (
	statusExtraPlugin statusExtraSource = iota
	statusExtraAuthority
	statusExtraWorkspace
	statusExtraEnv
)

type statusExtraEntry struct {
	item   statusItem
	source statusExtraSource
}

// statusExtraEntries is the single ordered construction of the status row's Extra list:
// plugin status items first (their hit indexes must not move), then the gvim segments that
// append after them, each tagged with its structural source.
func (m *Model) statusExtraEntries() []statusExtraEntry {
	pluginItems := m.plugStatusItems()
	out := make([]statusExtraEntry, 0, len(pluginItems)+3)
	// When the official env-manager plugin is enabled it publishes its own "env" pill
	// (plugin name + item id are both structural: no other plugin's same-named id can
	// suppress the native segment). In that case the native segment is omitted so the
	// editor shows one environment pill; the plugin's pill owns its click path.
	officialEnvPill := false
	for _, item := range pluginItems {
		out = append(out, statusExtraEntry{item: item, source: statusExtraPlugin})
		if item.Plugin == "env_manager" && item.ID == "env" {
			officialEnvPill = true
		}
	}
	if item, ok := m.authoritySegment(); ok {
		out = append(out, statusExtraEntry{item: item, source: statusExtraAuthority})
	}
	if item, ok := m.workspaceSegment(); ok {
		out = append(out, statusExtraEntry{item: item, source: statusExtraWorkspace})
	}
	if !officialEnvPill {
		if item, ok := m.envStatusSegment(); ok {
			out = append(out, statusExtraEntry{item: item, source: statusExtraEnv})
		}
	}
	return out
}

// statusExtraItems is the Extra slice the frame receives.
func (m *Model) statusExtraItems() []statusItem {
	entries := m.statusExtraEntries()
	out := make([]statusItem, len(entries))
	for i := range entries {
		out[i] = entries[i].item
	}
	return out
}

// statusExtraItem is the Extra item at the layout index, if any.
func (m *Model) statusExtraItem(index int) (statusItem, bool) {
	entry, ok := m.statusExtraEntryAt(index)
	return entry.item, ok
}

// statusExtraEntryAt is the structural entry at the layout index.
func (m *Model) statusExtraEntryAt(index int) (statusExtraEntry, bool) {
	entries := m.statusExtraEntries()
	if index < 0 || index >= len(entries) {
		return statusExtraEntry{}, false
	}
	return entries[index], true
}
