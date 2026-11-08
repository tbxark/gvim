package menubar

import (
	"testing"

	"github.com/tbxark/gvim/internal/tui/style"
)

// TestInsertPluginRows checks the insertion rules of the plugin rows: by action or label,
// after and before, with an unknown neighbour appending, and only into the target menu.
func TestInsertPluginRows(t *testing.T) {
	native := []Row{
		{Label: "Toggle File Explorer", Action: "toggle_file_explorer"},
		{Label: "Theme", Action: "select_theme", Submenu: true},
		{Separator: true},
		{Label: "Settings", Action: "open_settings"},
	}
	rows := insertPluginRows(native, "View", []PluginRow{
		{Menu: "View", Label: "Plugin row", Action: "p.cmd", After: "toggle_file_explorer"},
		{Menu: "View", Label: "Before settings", Action: "p.other", Before: "Settings"},
		{Menu: "View", Label: "Appended", Action: "p.last", After: "no_such_row"},
		{Menu: "Help", Label: "Wrong menu", Action: "p.help"},
	}, func(string) bool { return false })
	if len(rows) != len(native)+3 {
		t.Fatalf("rows = %d, want %d", len(rows), len(native)+3)
	}
	want := []string{"Toggle File Explorer", "Plugin row", "Theme", "", "Before settings", "Settings", "Appended"}
	for i, label := range want {
		if rows[i].Label != label {
			t.Fatalf("row %d = %q, want %q (all: %+v)", i, rows[i].Label, label, rows)
		}
	}
}

// TestOpenTitleInsertsPluginRows checks the insertion happens when the dropdown opens, so
// the rows are read against the context state of that moment.
func TestOpenTitleInsertsPluginRows(t *testing.T) {
	m := New(nil, style.Styles{}).WithPluginRows([]PluginRow{
		{Menu: "View", Label: "Plugin row", Action: "p.cmd"},
	})
	m = m.openTitle("View")
	found := false
	for _, r := range m.rows {
		if r.Label == "Plugin row" && r.Action == "p.cmd" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the rows do not contain the plugin row: %+v", m.rows)
	}
}

// TestInsertPluginRowsMenuIDAndState checks that a menu can be named by its stable id, and
// that checkbox and when read the context keys.
func TestInsertPluginRowsMenuIDAndState(t *testing.T) {
	holds := func(name string) bool { return name == "feature_on" }
	rows := insertPluginRows(nil, "View", []PluginRow{
		{Menu: "View", Label: "On", Action: "p.on", Checkbox: "feature_on"},
		{Menu: "View", Label: "Off", Action: "p.off", Checkbox: "feature_off"},
		{Menu: "View", Label: "Gated", Action: "p.gated", When: "feature_on"},
	}, holds)
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if !rows[0].Checkbox || !rows[0].Checked {
		t.Fatalf("row 0 = %+v, want a checked checkbox", rows[0])
	}
	if !rows[1].Checkbox || rows[1].Checked {
		t.Fatalf("row 1 = %+v, want an unchecked checkbox", rows[1])
	}
	if rows[2].Disabled {
		t.Fatalf("row 2 = %+v, want enabled (the context key holds)", rows[2])
	}
	// With a ports-less Model the rows still open; holds false disables the gated row.
	if got := insertPluginRows(nil, "View", []PluginRow{{Menu: "View", Label: "G", Action: "p.g", When: "x"}}, func(string) bool { return false }); !got[0].Disabled {
		t.Fatalf("row = %+v, want disabled", got[0])
	}
}
