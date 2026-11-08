package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/tbxark/gvim/pluginapi"
)

// TestStructureBindingsKind checks the bindings page layout: context headers, the
// read-only rows, the filter, and the action row that asks the root to open the
// keybinding editor.
func TestStructureBindingsKind(t *testing.T) {
	t.Parallel()
	port := &fakeSettingsPort{
		snap: plumbingSnapshot(),
		structures: map[string]SettingsStructure{
			"keybindings": {Key: "keybindings", Title: "Keybindings", Kind: "bindings", Filter: true,
				Rows: []SettingsStructureRow{
					{Name: "Ctrl+S", Value: "save", Group: "normal", Note: "preset", Locked: true},
					{Name: "Ctrl+X Ctrl+S", Value: "save_all", Group: "global", Note: "user", Locked: true},
					{Name: "Open Keybinding Editor…", Action: "ui:open_keybinding_editor"},
				}},
		},
	}
	m, p := openSettingsPage(t, port)
	sub := requestStructurePage(t, m, p, "keybindings")
	if len(sub.rows) != 5 {
		t.Fatalf("rows = %+v, want two headers and three rows", sub.rows)
	}
	if !sub.rows[0].header || sub.rows[0].title != "normal" {
		t.Errorf("first row = %+v, want the normal header", sub.rows[0])
	}
	if !sub.rows[2].header || sub.rows[2].title != "global" {
		t.Errorf("third row = %+v, want the global header", sub.rows[2])
	}
	// Filtering narrows to the matching binding (headers are dropped while a
	// filter is on).
	for _, r := range "save_all" {
		press(m, typed(string(r)))
	}
	if len(sub.rows) != 1 || !strings.Contains(sub.rows[0].item.Value, "save_all") {
		t.Fatalf("filtered rows = %+v, want save_all only", sub.rows)
	}
	press(m, special(tea.KeyEscape)) // clear the filter
	if len(sub.rows) != 5 {
		t.Fatalf("rows after clearing the filter = %d, want 5", len(sub.rows))
	}
	// Enter on the last row asks the root to open the editor.
	sub.sel = 4
	cmd := press(m, special(tea.KeyEnter))
	msg := firstOf[settingsUIActionMsg](t, collect(cmd))
	if msg.action != "open_keybinding_editor" {
		t.Errorf("ui action = %q, want open_keybinding_editor", msg.action)
	}
	// Runs on the update goroutine, closing the settings overlays first.
	m.Update(msg)
	for _, ov := range m.overlays {
		if _, ok := ov.(*settingsPage); ok {
			t.Error("a Settings page is still open after the editor action")
		}
		if _, ok := ov.(*settingsStructurePage); ok {
			t.Error("a Settings structure page is still open after the editor action")
		}
	}
}

// TestSettingsKeybindingPluginRows checks the TUI-side read-only plugin rows: they
// are appended before the app's action row, with their context and plugin name.
func TestSettingsKeybindingPluginRows(t *testing.T) {
	t.Parallel()
	m, _, _ := newMgrFixture(t)
	m.plug.keys.bindings = map[string][]pluginapi.Binding{
		"hello": {{Chord: "Alt+G", Action: "hello:status", Plugin: "hello", When: []string{"normal"}}},
	}
	m.plug.keys.border = []string{"hello"}
	rows := []SettingsStructureRow{
		{Name: "Ctrl+S", Value: "save", Group: "normal", Note: "preset", Locked: true},
		{Name: "Open Keybinding Editor…", Action: "ui:open_keybinding_editor"},
	}
	got := m.settingsKeybindingRows(rows)
	if len(got) != 3 {
		t.Fatalf("rows = %+v, want the preset row, the plugin row and the action", got)
	}
	if pluginRow := got[1]; pluginRow.Name != "Alt+G" || pluginRow.Value != "hello:status" ||
		pluginRow.Group != "normal" || pluginRow.Note != "plugin: hello" || !pluginRow.Locked {
		t.Errorf("plugin row = %+v, want the read-only hello binding", pluginRow)
	}
	if got[2].Action != "ui:open_keybinding_editor" {
		t.Errorf("last row = %+v, want the action row last", got[2])
	}
}
