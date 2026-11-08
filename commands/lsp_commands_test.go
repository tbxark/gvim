package commands_test

import (
	"testing"

	"github.com/tbxark/gvim/commands"
)

// TestDiagnosticsCommandsAreInThePalette checks the hand-written diagnostics rows:
// both are native palette entries with their actions, in the every-context set.
func TestDiagnosticsCommandsAreInThePalette(t *testing.T) {
	entries := commands.Commands(nil)
	want := []struct{ name, action string }{
		{"Toggle Diagnostics Sidebar", "toggle_diagnostics_panel"},
		{"Diagnostics List", "open_diagnostics_list"},
	}
	for _, w := range want {
		found := false
		for _, e := range entries {
			if e.Name != w.name {
				continue
			}
			found = true
			if e.Action != w.action {
				t.Errorf("%s: action = %q, want %q", w.name, e.Action, w.action)
			}
			if e.Source != commands.SourceBuiltin {
				t.Errorf("%s: source = %q, want builtin", w.name, e.Source)
			}
		}
		if !found {
			t.Errorf("%s is not in the palette", w.name)
		}
	}
}
