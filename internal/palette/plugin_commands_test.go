package palette

import (
	"slices"
	"strings"
	"testing"
)

// TestPluginCommandRows checks the hand-written rows of the plugin manager: each is a
// listed built-in row with its action, after the generated rows, and each answers to a
// query for its name.
func TestPluginCommandRows(t *testing.T) {
	t.Parallel()
	entries := Commands(nil)
	tests := []struct {
		name, action, query string
	}{
		{"Plugin: Manager", "plugin_manager", ">plugin man"},
		{"Plugin: Show Log", "plugin_show_log", ">plugin log"},
		{"Plugin: Reload...", "plugin_reload", ">plugin reload..."},
		{"Plugin: Reload All", "plugin_reload_all", ">plugin reload all"},
	}
	for i, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			idx := slices.IndexFunc(entries, func(e Entry) bool { return e.Action == tt.action })
			if idx < 0 {
				t.Fatalf("no row for %s", tt.action)
			}
			e := entries[idx]
			if e.Name != tt.name || e.Source != SourceBuiltin || e.Description == "" {
				t.Fatalf("row = %+v", e)
			}
			if want := len(entries) - len(tests) + i; idx != want {
				t.Fatalf("%s is row %d, want %d (after the generated rows)", tt.action, idx, want)
			}
			got := Search(entries, tt.query)
			if len(got) == 0 || got[0].Action != tt.action || got[0].Disabled {
				t.Fatalf("query %q: first = %+v", tt.query, got)
			}
		})
	}
}

// TestGvimRenames checks that no row says init.ts or Fresh's defineMacro any more,
// and that the init rows say init.lua.
func TestGvimRenames(t *testing.T) {
	t.Parallel()
	entries := Commands(nil)
	tests := []struct {
		action, name string
		desc         string // a word the description must contain
	}{
		{"init_check", "init: Check init.lua", "init.lua"},
		{"init_edit", "init: Edit init.lua", "init.lua"},
		{"init_reload", "init: Reload init.lua", "init.lua"},
		{"prompt_promote_macro", "Macro: Promote to command", "init.lua"},
		{"prompt_save_macro_to_init", "Macro: Save to init.lua", "init.lua"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			t.Parallel()
			idx := slices.IndexFunc(entries, func(e Entry) bool { return e.Action == tt.action })
			if idx < 0 {
				t.Fatalf("no row for %s", tt.action)
			}
			e := entries[idx]
			if e.Name != tt.name || !strings.Contains(e.Description, tt.desc) {
				t.Fatalf("row = %q / %q, want %q mentioning %q", e.Name, e.Description, tt.name, tt.desc)
			}
		})
	}
	for _, e := range entries {
		if strings.Contains(e.Name, "init.ts") {
			t.Errorf("%s still names Fresh's init.ts", e.Name)
		}
		if strings.Contains(e.Description, "init.ts") || strings.Contains(e.Description, "defineMacro") {
			t.Errorf("%s still describes Fresh's init.ts: %q", e.Name, e.Description)
		}
	}
}
