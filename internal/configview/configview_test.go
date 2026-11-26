package configview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// userConfigPath writes body as a user config file in a temp directory and
// returns its path.
func userConfigPath(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gvim", "config.json")
	if body != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// TestLoadReportsKeybindingWarnings checks that the view reports the user's
// keybindings that the keymap cannot apply, in file order, and warns about
// nothing when the list is clean.
func TestLoadReportsKeybindingWarnings(t *testing.T) {
	path := userConfigPath(t, `{"keybindings": [
  {"key": "Weird", "modifiers": ["ctrl"], "action": "save"},
  {"key": "u", "modifiers": ["alt"], "action": "no_such_action"},
  {"key": "esc", "modifiers": ["control"], "action": "save"}
]}`)
	view, err := Load(t.TempDir(), Options{Config: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Warnings) != 2 ||
		!strings.Contains(view.Warnings[0], `keybindings[0]: unknown key name "Weird"`) ||
		!strings.Contains(view.Warnings[1], `keybindings[1]: unknown action "no_such_action"`) {
		t.Fatalf("warnings = %q, want the two bad entries and not the valid Fresh-form one", view.Warnings)
	}
	clean := userConfigPath(t, `{"keybindings": [{"key": "u", "modifiers": ["alt"], "action": "save"}]}`)
	view, err = Load(t.TempDir(), Options{Config: clean})
	if err != nil || len(view.Warnings) != 0 {
		t.Fatalf("a clean config warns: %q, %v", view.Warnings, err)
	}
}

func TestLoadOutputReloadsWithoutDiagnostics(t *testing.T) {
	path := userConfigPath(t, `{
  "theme": "gopher",
  "editor": {"tab_size": 2, "use_tabs": true},
  "file_explorer": {"show_hidden": false, "hide_gitignore": true}
}`)
	view, err := Load(t.TempDir(), Options{Config: path})
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", view.Warnings)
	}
	roundTrip := userConfigPath(t, string(view.JSON))
	view2, err := Load(t.TempDir(), Options{Config: roundTrip})
	if err != nil {
		t.Fatal(err)
	}
	if len(view2.Warnings) != 0 {
		t.Fatalf("round-trip config show output produced warnings: %v\nJSON:\n%s", view2.Warnings, view.JSON)
	}
}

func TestLoadShowsNestedSchemaWithoutDottedKeys(t *testing.T) {
	path := userConfigPath(t, `{"editor": {"tab_size": 2}, "file_explorer": {"hide_gitignore": true}, "lsp_enabled": false}`)
	view, err := Load(t.TempDir(), Options{Config: path})
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(view.JSON, &top); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, view.JSON)
	}
	for key := range top {
		if strings.Contains(key, ".") {
			t.Errorf("top-level key %q is dotted", key)
		}
	}
	var sections struct {
		Editor struct {
			TabSize int `json:"tab_size"`
		} `json:"editor"`
		FileExplorer struct {
			HideGitignore bool `json:"hide_gitignore"`
		} `json:"file_explorer"`
		LSPEnabled bool            `json:"lsp_enabled"`
		Terminal   json.RawMessage `json:"terminal"`
		Clipboard  json.RawMessage `json:"clipboard"`
	}
	if err := json.Unmarshal(view.JSON, &sections); err != nil {
		t.Fatal(err)
	}
	if sections.Editor.TabSize != 2 || !sections.FileExplorer.HideGitignore || sections.LSPEnabled {
		t.Errorf("effective values lost: %+v", sections)
	}
	if sections.Terminal == nil || sections.Clipboard == nil {
		t.Errorf("terminal and clipboard sections missing from the output")
	}
}

func TestKeymapNamesReturnsClone(t *testing.T) {
	names := KeymapNames()
	if len(names) == 0 {
		t.Fatal("no keymap names")
	}
	first := names[0]
	names[0] = "mutated"
	if KeymapNames()[0] != first {
		t.Errorf("KeymapNames shares its backing slice: got %q after mutation", KeymapNames()[0])
	}
	err := ValidateFlags("nope", "")
	if err == nil {
		t.Fatal("unknown keymap accepted")
	}
	for _, name := range KeymapNames() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not list keymap %q", err, name)
		}
	}
}
