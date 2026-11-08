package app

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/internal/keybindings"
	"github.com/tbxark/gvim/internal/tui"
	"github.com/tbxark/gvim/keymap"
)

// keybindingsSettingsService builds the page service with an effective keymap built
// from the default preset and the config's keybindings.
func keybindingsSettingsService(t *testing.T, cfg config.Config) *settingsService {
	t.Helper()
	base, err := keybindings.Keymap("default")
	if err != nil {
		t.Fatal(err)
	}
	eff := keybindings.ApplyOverrides(base, cfg)
	return newSettingsService("", Options{}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil, settingsRuntime{
		keymapOf:   func(config.Config) tui.Keymap { return keybindings.NewKeymap(eff) },
		presetName: "default",
	})
}

// TestSettingsKeybindingsPage checks the read-only effective list: the preset rows
// grouped in the consulted-context order, the user's own entry marked, every row
// locked, and the action row that opens the validated Keybinding Editor.
func TestSettingsKeybindingsPage(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Keybindings = json.RawMessage(`[{"key":"s","modifiers":["ctrl"],"action":"save","when":"normal"}]`)
	s := keybindingsSettingsService(t, cfg)
	st, err := s.Structure("keybindings", "user")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != "bindings" || !st.Filter || st.Note == "" {
		t.Fatalf("structure = %+v, want the filtered bindings page", st)
	}
	if len(st.Rows) == 0 {
		t.Fatal("the page has no binding rows")
	}
	// The rows are grouped in keymap.ConsultedContexts order.
	order := map[string]int{}
	for i, ctx := range keymap.ConsultedContexts {
		order[string(ctx)] = i
	}
	last := -1
	for _, row := range st.Rows {
		if row.Action != "" {
			continue
		}
		if row.Group == "" {
			t.Errorf("row %+v has no context group", row)
			continue
		}
		if i, ok := order[row.Group]; !ok || i < last {
			t.Errorf("row %q group %q is out of the consulted order (index %d, want >= %d)", row.Name, row.Group, i, last)
		} else {
			last = i
		}
		if !row.Locked {
			t.Errorf("binding row %+v is not read-only", row)
		}
	}
	// The user's Ctrl+S override is marked as such in normal; the other rows are
	// the preset's.
	var sawUser, sawPreset bool
	for _, row := range st.Rows {
		if row.Group != "normal" {
			continue
		}
		if row.Name == "Ctrl+S" && row.Action == "" && row.Note == "user" {
			sawUser = true
		}
		if row.Note == "preset" {
			sawPreset = true
		}
	}
	if !sawUser || !sawPreset {
		t.Errorf("normal rows: user %v, preset %v; want both", sawUser, sawPreset)
	}
	// The action row opens the existing editor and is last.
	action := st.Rows[len(st.Rows)-1]
	if action.Action != "ui:open_keybinding_editor" || action.Name != "Open Keybinding Editor…" {
		t.Errorf("last row = %+v, want the editor action", action)
	}
	// The app does not list plugin bindings; the TUI appends those read-only.
	for _, row := range st.Rows {
		if slices.Contains([]string{"plugin", "plugin: git"}, row.Note) {
			t.Errorf("the app listed a plugin binding: %+v", row)
		}
	}
}

// TestSettingsKeybindingsSummary checks the page row's summary in the snapshot: the
// binding count and the preset.
func TestSettingsKeybindingsSummary(t *testing.T) {
	t.Parallel()
	cfg := config.Defaults()
	cfg.Keybindings = json.RawMessage(`[{"key":"s","modifiers":["ctrl"],"action":"save"}]`)
	s := keybindingsSettingsService(t, cfg)
	snap := s.Snapshot()
	var found bool
	for _, sec := range snap.Sections {
		if sec.Name != "Keybindings" {
			continue
		}
		found = true
		item := sec.Items[0]
		if item.Kind != "page" || item.Key != "keybindings" || item.Value == "" {
			t.Errorf("keybindings item = %+v, want the page row with a summary", item)
		}
		if len(item.Layers) != 1 || item.Layers[0] != tui.SettingsLayerUser {
			t.Errorf("layers = %v, want user only", item.Layers)
		}
	}
	if !found {
		t.Fatal("the snapshot has no Keybindings section")
	}
}
