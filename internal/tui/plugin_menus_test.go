package tui

import (
	"testing"

	"github.com/tbxark/gvim/pluginapi"
)

// TestPluginMenuRows checks that the plugin host keeps the rows whole-set, exposes them to
// the menu bar in registration order and drops them when the plugin is unloaded.
func TestPluginMenuRows(t *testing.T) {
	t.Parallel()
	ed := newFakeEditor("hello")
	m := newTestModel(t, ed, newFakeTree(3), 80, 24)
	rt := newFakeRuntime(t)
	m.plug = newPluginState(m, RootPorts{Plugins: rt, PluginEditor: &fakePluginEditor{active: 1, bufs: []pluginapi.BufferInfo{{ID: 1}}}})
	useHost(rt, m)
	h := m.plug.host
	h.SetMenuItems("p", []pluginapi.MenuItem{
		{Menu: "View", Label: "First", Action: "p.first"},
		{Menu: "View", Label: "Second", Action: "p.second", After: "toggle_file_explorer"},
	})
	rows := m.plugMenuRows()
	if len(rows) != 2 || rows[0].Label != "First" || rows[1].After != "toggle_file_explorer" {
		t.Fatalf("rows = %+v", rows)
	}
	h.SetMenuItems("p", nil)
	if rows := m.plugMenuRows(); len(rows) != 0 {
		t.Fatalf("rows after clear = %+v", rows)
	}
	h.SetMenuItems("p", []pluginapi.MenuItem{{Menu: "File", Label: "Open", Action: "p.open"}})
	m.plugMenusGone("p")
	if rows := m.plugMenuRows(); len(rows) != 0 {
		t.Fatalf("rows after OwnerGone = %+v", rows)
	}
	// Two plugins keep their own rows, in first-registration order.
	h.SetMenuItems("a", []pluginapi.MenuItem{{Menu: "View", Label: "A", Action: "a.cmd"}})
	h.SetMenuItems("b", []pluginapi.MenuItem{{Menu: "View", Label: "B", Action: "b.cmd"}})
	h.SetMenuItems("a", []pluginapi.MenuItem{{Menu: "View", Label: "A2", Action: "a.cmd2"}})
	rows = m.plugMenuRows()
	if len(rows) != 2 || rows[0].Label != "A2" || rows[1].Label != "B" {
		t.Fatalf("rows = %+v, want A2 then B", rows)
	}
}
