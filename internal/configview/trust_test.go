package configview

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trustWorkspace makes a workspace root with one project config file and returns it.
func trustWorkspace(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ws")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// projectConfig writes body as the workspace's .gvim/config.json.
func projectConfig(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".gvim")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// viewSettings decodes the view JSON into a map.
func viewSettings(t *testing.T, view ConfigView) map[string]any {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(view.JSON, &settings); err != nil {
		t.Fatalf("view JSON: %v", err)
	}
	return settings
}

// TestLoadGatesTheProjectLayerByTrust checks the trust gate: the caller resolves the
// workspace trust read-only (Options.Trusted), and an untrusted or unknown workspace
// never merges the project layer while the view says which file was skipped.
func TestLoadGatesTheProjectLayerByTrust(t *testing.T) {
	root := trustWorkspace(t)
	projectConfig(t, root, `{"tab_size": 8, "show_menu_bar": false}`)
	user := userConfigPath(t, `{"tab_size": 2}`)

	tests := []struct {
		name     string
		trusted  *bool
		wantTab  float64
		wantSkip bool
	}{
		{"no trust information", nil, 2, true},
		{"untrusted", boolPtr(false), 2, true},
		{"trusted", boolPtr(true), 8, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			view, err := Load(root, Options{Config: user, Trusted: tt.trusted})
			if err != nil {
				t.Fatal(err)
			}
			settings := viewSettings(t, view)
			if got := settings["tab_size"]; got != tt.wantTab {
				t.Fatalf("tab_size = %v, want %v", got, tt.wantTab)
			}
			if tt.trusted != nil && *tt.trusted {
				if got := settings["show_menu_bar"]; got != false {
					t.Fatalf("trusted show_menu_bar = %v, want the project layer's false", got)
				}
			}
			skip := "project config layer skipped: " + filepath.Join(root, ".gvim", "config.json")
			found := false
			for _, w := range view.Warnings {
				if strings.Contains(w, skip) {
					found = true
				}
			}
			if found != tt.wantSkip {
				t.Fatalf("warnings = %q, want skip note %v", view.Warnings, tt.wantSkip)
			}
		})
	}
}

// TestLoadWithoutAProjectFileHasNoSkipNote checks the note is only for a project file
// the gate actually skipped.
func TestLoadWithoutAProjectFileHasNoSkipNote(t *testing.T) {
	root := trustWorkspace(t)
	user := userConfigPath(t, `{"tab_size": 3}`)
	view, err := Load(root, Options{Config: user})
	if err != nil {
		t.Fatal(err)
	}
	if got := viewSettings(t, view)["tab_size"]; got != float64(3) {
		t.Fatalf("tab_size = %v, want 3", got)
	}
	for _, w := range view.Warnings {
		if strings.Contains(w, "project config layer skipped") {
			t.Fatalf("a workspace without a project file reports a skipped layer: %q", w)
		}
	}
}

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }
