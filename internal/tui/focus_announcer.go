package tui

// Focus announcer: the one place that turns a change of what the user is looking at
// (workspace window, split/window, buffer) into the plugin events active_window_changed,
// buffer_activated / buffer_deactivated and focus_gained. Fresh's plan calls for a single
// announcer so every focus path fires the same hooks; here the paths that change focus call
// announceFocus after updating their state, and a seeded announcement carries the focus
// across a workspace switch (PluginShared.Bind).

import (
	"github.com/tbxark/gvim/pluginapi"
)

// announceState is one announced focus: the focused split's window id, the active buffer,
// the workspace label it belongs to, and whether the state was seeded from another
// workspace (a switch forces the first announcement even when the numeric ids coincide).
type announceState struct {
	valid     bool
	window    uint64
	buffer    pluginapi.BufferID
	workspace string
	fromOther bool
}

// focusWindowID is the window id of the focused split (0 without a focused pane).
func (m *Model) focusWindowID() uint64 {
	if m.splits.Focus == nil {
		return 0
	}
	return m.splits.IDOf(m.splits.Focus)
}

// announceFocus emits the focus hooks whose (window, buffer) changed since the last
// announcement. It runs on the update loop and only queues events: no IO, no runtime call.
func (m *Model) announceFocus() {
	if m.plug.rt == nil {
		return
	}
	ps := &m.plug.panels
	cur := announceState{
		valid:     true,
		window:    uint64(m.pluginGlobalWindow(m.focusWindowID())),
		buffer:    m.pluginGlobalBuffer(m.pluginActiveBuffer()),
		workspace: m.workspaceLabel,
	}
	prev := ps.announced
	ps.announced = cur
	if !prev.valid {
		// First announcement of a direct model: the editor's initial buffer events come from
		// the buffer diff, so the announcer only records the state here.
		m.plugPanelsScopeSync()
		return
	}
	if prev.window != cur.window || prev.fromOther {
		m.emitFocusWindow(prev.window, cur.window, cur.workspace)
	}
	if prev.buffer != cur.buffer || prev.fromOther {
		m.emitFocusBuffer(prev.buffer, cur.buffer, cur.workspace)
	}
	m.plugPanelsScopeSync()
}

// syncFocusAnnouncement records the current focus without emitting events. The buffer diff
// calls it after emitting the buffer pair it already knows about, so a later window change
// does not repeat a buffer transition.
func (m *Model) syncFocusAnnouncement() {
	if m.plug.rt == nil {
		return
	}
	m.plug.panels.announced = announceState{
		valid:     true,
		window:    uint64(m.pluginGlobalWindow(m.focusWindowID())),
		buffer:    m.pluginGlobalBuffer(m.pluginActiveBuffer()),
		workspace: m.workspaceLabel,
	}
}

// seedFocusAnnouncement carries the leaving model's announced focus into m, so the first
// announcement after a switch reports the transition (and a switch forces it even when two
// workspaces happen to have the same numeric window/buffer ids; the ids are per-workspace,
// the workspace label in the payload disambiguates).
func (m *Model) seedFocusAnnouncement(prev *Model) {
	if prev == nil {
		return
	}
	m.plug.panels.announced = announceState{
		valid:     true,
		window:    uint64(prev.pluginGlobalWindow(prev.focusWindowID())),
		buffer:    prev.pluginGlobalBuffer(prev.pluginActiveBuffer()),
		workspace: prev.workspaceLabel,
		fromOther: true,
	}
}

// emitFocusWindow queues active_window_changed (and focus_gained for nil -> window).
func (m *Model) emitFocusWindow(previous, current uint64, workspace string) {
	if m.plug.rt == nil {
		return
	}
	data := map[string]any{"previous": previous, "current": current}
	if workspace != "" {
		data["workspace"] = workspace
	}
	m.plugEmit("active_window_changed", data)
	if previous == 0 && current != 0 {
		m.plugEmit("focus_gained", map[string]any{})
	}
}

// emitFocusBuffer queues buffer_deactivated and buffer_activated in the shape the event
// layer always used, extended with the workspace label and the current window id.
func (m *Model) emitFocusBuffer(previous, current pluginapi.BufferID, workspace string) {
	// This path only runs for a forced workspace switch, where both ids are already global
	// and the previous one belongs to the leaving workspace: emit the pair unconditionally.
	prevExists := previous != 0
	if prevExists {
		data := map[string]any{"buf": previous}
		if current != 0 {
			data["next"] = current
		}
		if workspace != "" {
			data["workspace"] = workspace
		}
		m.plugEmit("buffer_deactivated", data)
	}
	if current != 0 {
		data := map[string]any{"buf": current}
		if prevExists {
			data["previous"] = previous
		}
		if workspace != "" {
			data["workspace"] = workspace
		}
		m.plugEmit("buffer_activated", data)
	}
}
