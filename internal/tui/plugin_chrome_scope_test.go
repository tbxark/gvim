package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
	"github.com/tbxark/gvim/theme"
)

// newSharedPluginModel builds a root model bound to the daemon-wide plugin mux: the host
// routes through PluginShared and the fake runtime is the one shared runtime.
func newSharedPluginModel(t *testing.T, shared *PluginShared, rt *fakeRuntime) (*Model, *fakePluginEditor) {
	t.Helper()
	ed := newFakeEditor("hello", "world")
	ed.quitAction = "quit"
	ed.tabs = []TabInfo{{Name: "a.txt", Path: "/w/a.txt", Active: true}, {Name: "b.txt", Path: "/w/b.txt"}}
	pe := &fakePluginEditor{ed: ed, active: 1, bufs: []pluginapi.BufferInfo{
		{ID: 1, Path: "/w/a.txt", Name: "a.txt", Active: true},
		{ID: 2, Path: "/w/b.txt", Name: "b.txt"},
	}}
	m := New(Deps{Editor: ed, Keys: testKeys(), Theme: theme.Theme{}, Clock: testClock,
		Root: RootPorts{Plugins: rt, PluginEditor: pe, PluginShared: shared}})
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.SetWorkspaceLabel("ws-" + peName(m))
	return m, pe
}

func peName(m *Model) string {
	return "x"
}

// sectionIDs lists the ids of the plugin sidebar sections a model currently shows.
func sectionIDs(m *Model) []pluginapi.PanelID {
	var out []pluginapi.PanelID
	for _, mp := range m.plug.panels.sidebarOrder(m) {
		out = append(out, mp.id)
	}
	return out
}

// queuedEvents returns and clears the model's queued plugin events.
func queuedEvents(m *Model) []pluginapi.Event {
	out := m.plug.events
	m.plug.events = nil
	return out
}

func mountSection(t *testing.T, host *pluginHost, id pluginapi.PanelID, scope pluginapi.WidgetScope, buf pluginapi.BufferID) {
	t.Helper()
	mount := pluginapi.WidgetMount{
		Panel:     pluginapi.WidgetPanel{Root: pluginapi.Widget{Kind: pluginapi.WidgetLabel, Text: "section"}},
		Title:     "section",
		Placement: pluginapi.PlacementSidebar,
		Rows:      3,
		Scope:     scope,
		Buffer:    buf,
	}
	if err := host.MountWidgetPanel("p", id, mount); err != nil {
		t.Fatalf("mount %d (%s): %v", id, scope, err)
	}
}

// TestPluginChromeScopeMatrix checks the per-registration chrome scope: window-scoped
// sections stay with their workspace, editor-scoped ones are carried into every workspace
// by PluginShared.Bind, and a buffer-scoped section is shown only while its buffer is
// active and is dropped when the buffer closes.
func TestPluginChromeScopeMatrix(t *testing.T) {
	rt := newFakeRuntime(t)
	shared := NewPluginShared()
	m1, pe1 := newSharedPluginModel(t, shared, rt)
	shared.Bind(m1)
	mountSection(t, m1.plug.host, 1, pluginapi.ScopeWindow, 0)
	mountSection(t, m1.plug.host, 2, pluginapi.ScopeEditor, 0)
	mountSection(t, m1.plug.host, 3, pluginapi.ScopeBuffer, 0)
	if got := sectionIDs(m1); len(got) != 3 {
		t.Fatalf("workspace A sections = %v, want window+editor+buffer", got)
	}

	// The buffer-scoped section follows its buffer: switching the active buffer hides it.
	pe1.active = 2
	m1.announceFocus()
	if got := sectionIDs(m1); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("after buffer switch = %v, want window+editor only", got)
	}
	pe1.active = 1
	m1.announceFocus()
	if got := sectionIDs(m1); len(got) != 3 {
		t.Fatalf("after returning to the buffer = %v, want all three", got)
	}

	// A second workspace gets the editor-scoped section only.
	m2, _ := newSharedPluginModel(t, shared, rt)
	shared.Bind(m2)
	if got := sectionIDs(m2); len(got) != 1 || got[0] != 2 {
		t.Fatalf("workspace B sections = %v, want only the editor-scoped one", got)
	}
	// The editor-scoped panel was carried as a separate instance (no shared panel pointer).
	if m1.plug.panels.get(2) == m2.plug.panels.get(2) {
		t.Fatal("the editor-scoped section was shared by pointer instead of rebuilt")
	}

	// Owner-gone on the active workspace drops the editor-scoped chrome from the snapshot;
	// the next switch does not bring it back.
	shared.Bind(m1)
	m1.plug.host.OwnerGone("p", 1)
	if got := sectionIDs(m1); len(got) != 0 {
		t.Fatalf("after owner gone = %v", got)
	}
	shared.Bind(m2)
	if got := sectionIDs(m2); len(got) != 0 {
		t.Fatalf("a removed editor section came back in workspace B: %v", got)
	}
}

// TestPluginBufferScopedChromeDropsWithBuffer checks a buffer-scoped section is removed (not
// just hidden) when its buffer closes.
func TestPluginBufferScopedChromeDropsWithBuffer(t *testing.T) {
	rt := newFakeRuntime(t)
	shared := NewPluginShared()
	m, pe := newSharedPluginModel(t, shared, rt)
	shared.Bind(m)
	mountSection(t, m.plug.host, 5, pluginapi.ScopeBuffer, 0)
	if got := sectionIDs(m); len(got) != 1 {
		t.Fatalf("sections = %v", got)
	}
	m.plugPanelsBufferClosed(pe.bufs[0].ID)
	if got := sectionIDs(m); len(got) != 0 {
		t.Fatalf("a closed buffer's section survived: %v", got)
	}
}

// TestPluginFocusAnnouncerSplit checks split focus changes produce exactly one
// active_window_changed with the old and new window ids (the announcer replaced the
// scattered emitters).
func TestPluginFocusAnnouncerSplit(t *testing.T) {
	rt := newFakeRuntime(t)
	rt.wants["active_window_changed"] = true
	rt.wants["buffer_activated"] = true
	rt.wants["focus_gained"] = true
	m, _ := newSharedPluginModel(t, NewPluginShared(), rt)
	m.announceFocus() // first announcement of the model
	_ = queuedEvents(m)
	runAction(t, m, "split_vertical")
	events := queuedEvents(m)
	if len(events) == 0 {
		t.Fatalf("a split focus change emitted no events")
	}
	windows := 0
	var data map[string]any
	for _, e := range events {
		if e.Name == "active_window_changed" {
			windows++
			data = e.Data
		}
	}
	if windows != 1 {
		t.Fatalf("active_window_changed fired %d times on one split: %v", windows, events)
	}
	prev, _ := data["previous"].(uint64)
	cur, _ := data["current"].(uint64)
	if cur == 0 || cur == prev {
		t.Fatalf("window ids = %v -> %v, want distinct nonzero", prev, cur)
	}
}

// TestPluginFocusAnnouncerWorkspaceSwitch checks a workspace switch forces one announcement
// of the entering workspace's window/buffer with its label, even when the numeric ids are
// equal, and does not duplicate on the next update.
func TestPluginFocusAnnouncerWorkspaceSwitch(t *testing.T) {
	rt := newFakeRuntime(t)
	for _, name := range []string{"active_window_changed", "buffer_activated", "focus_gained"} {
		rt.wants[name] = true
	}
	shared := NewPluginShared()
	a, _ := newSharedPluginModel(t, shared, rt)
	shared.Bind(a)
	a.announceFocus()
	b, _ := newSharedPluginModel(t, shared, rt)
	b.workspaceLabel = "ws-b"
	shared.Bind(b)
	_ = queuedEvents(a)
	b.announceFocus()
	var windows, buffers int
	var windowData map[string]any
	events := queuedEvents(b)
	for _, e := range events {
		switch e.Name {
		case "active_window_changed":
			windows++
			windowData = e.Data
		case "buffer_activated":
			buffers++
			if e.Data["workspace"] != "ws-b" {
				t.Fatalf("buffer_activated workspace = %v, want ws-b", e.Data["workspace"])
			}
		}
	}
	if windows != 1 || buffers != 1 {
		t.Fatalf("switch events windows=%d buffers=%d: %v", windows, buffers, events)
	}
	if windowData["workspace"] != "ws-b" {
		t.Fatalf("active_window_changed = %v, want the workspace label", windowData)
	}
	// A second announcement with nothing changed emits nothing.
	_ = queuedEvents(b)
	b.announceFocus()
	if got := queuedEvents(b); len(got) != 0 {
		t.Fatalf("a stable focus emitted %d events", len(got))
	}
}
