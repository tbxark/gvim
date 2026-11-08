package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// patched applies edits to body and returns the new file text.
func patched(t *testing.T, body string, edits []Edit) string {
	t.Helper()
	data, err := PatchBytes([]byte(body), edits)
	if err != nil {
		t.Fatalf("PatchBytes error = %v", err)
	}
	return string(data)
}

// loadPatched writes body (patched) to a temporary config file and loads it.
func loadPatched(t *testing.T, body string, edits []Edit) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(patched(t, body, edits)), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(Options{UserFile: path})
	if err != nil {
		t.Fatalf("Load after patch error = %v", err)
	}
	return cfg
}

func TestPatchBytesPreservesCommentsAndUnknownKeys(t *testing.T) {
	t.Parallel()
	body := `// my settings
{
  /* keep me */ "theme": "light",
  "unknown_key": [1, 2],
  "editor": {
    "tab_size": 2, // old
  },
}
`
	got := patched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("8")}})
	for _, want := range []string{"// my settings", "/* keep me */", `"unknown_key"`, `"tab_size": 8`, "// old"} {
		if !strings.Contains(got, want) {
			t.Errorf("patched file misses %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `"tab_size": 2`) {
		t.Errorf("the old value is still there:\n%s", got)
	}
	// The file still loads as the config it now is. unknown_key is still an
	// unknown setting, so Load reports it as it would for a hand-written file.
	cfg := loadPatched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("8")}})
	if cfg.TabSize != 8 || cfg.Theme != "light" {
		t.Fatalf("cfg = tab %d theme %q, want 8 and light", cfg.TabSize, cfg.Theme)
	}
}

func TestPatchBytesCreatesMissingParents(t *testing.T) {
	t.Parallel()
	body := `{
  "theme": "dark"
}
`
	cfg := loadPatched(t, body, []Edit{
		{Path: []string{"editor", "tab_size"}, Raw: []byte("3")},
		{Path: []string{"editor", "search", "regex"}, Raw: []byte("true")},
	})
	if cfg.TabSize != 3 || !cfg.Search.Regex {
		t.Fatalf("cfg = tab %d regex %v, want 3 and true", cfg.TabSize, cfg.Search.Regex)
	}
	if cfg.Theme != "dark" {
		t.Fatalf("theme = %q, want the untouched dark", cfg.Theme)
	}
	// The new objects keep the file's two-space style.
	got := patched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("3")}})
	if !strings.Contains(got, "\n  \"editor\"") || !strings.Contains(got, "\n    \"tab_size\": 3") {
		t.Errorf("created object layout:\n%s", got)
	}
}

func TestPatchBytesOfMissingDataCreatesAConfig(t *testing.T) {
	t.Parallel()
	data, err := PatchBytes(nil, []Edit{{Path: []string{"theme"}, Raw: []byte(`"light"`)}})
	if err != nil {
		t.Fatalf("PatchBytes error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load after patch = %v, %v", err, diags)
	}
	if cfg.Theme != "light" {
		t.Fatalf("theme = %q, want light", cfg.Theme)
	}
}

func TestPatchBytesKeepsTrailingCommaStyle(t *testing.T) {
	t.Parallel()
	body := `{
  "theme": "dark",
}
`
	got := patched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("3")}})
	// The file had a trailing comma, so the appended member keeps one at the
	// root, and the created object closes before it.
	if !strings.Contains(got, "},") {
		t.Errorf("the root trailing comma was not kept:\n%s", got)
	}
	cfg := loadPatched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("3")}})
	if cfg.TabSize != 3 {
		t.Errorf("tab size = %d, want 3", cfg.TabSize)
	}
}

func TestPatchBytesLeavesInvalidInputAlone(t *testing.T) {
	t.Parallel()
	const broken = `{
  "theme": 
`
	if _, err := PatchBytes([]byte(broken), []Edit{{Path: []string{"editor", "tab_size"}, Raw: []byte("3")}}); err == nil {
		t.Fatal("PatchBytes accepted a broken file")
	}
}

func TestPatchBytesWithNoEditsReturnsTheInput(t *testing.T) {
	t.Parallel()
	const body = `{"theme": "light"}
`
	data, err := PatchBytes([]byte(body), nil)
	if err != nil {
		t.Fatalf("PatchBytes error = %v", err)
	}
	if string(data) != body {
		t.Fatalf("PatchBytes changed the input: %q", data)
	}
}

func TestPatchBytesEscapesJSONPointer(t *testing.T) {
	t.Parallel()
	got := patched(t, "{}", []Edit{{Path: []string{"a/b", "c~d"}, Raw: []byte("1")}})
	if !strings.Contains(got, `"a/b"`) || !strings.Contains(got, `"c~d"`) {
		t.Fatalf("the escaped names are missing:\n%s", got)
	}
}

func TestPatchBytesRejectsAnEmptyEditPath(t *testing.T) {
	t.Parallel()
	if _, err := PatchBytes([]byte("{}"), []Edit{{Path: nil, Raw: []byte("1")}}); err == nil {
		t.Fatal("PatchBytes accepted an empty path")
	}
}

func TestValidateEdits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		edit    Edit
		project bool
		wantErr string
	}{
		{name: "a valid bool", edit: Edit{Path: []string{"editor", "line_numbers"}, Raw: []byte("false")}},
		{name: "a valid int", edit: Edit{Path: []string{"editor", "tab_size"}, Raw: []byte("8")}},
		{name: "an int below the minimum", edit: Edit{Path: []string{"editor", "tab_size"}, Raw: []byte("0")}, wantErr: "between 1 and 16"},
		{name: "a non-integer", edit: Edit{Path: []string{"editor", "tab_size"}, Raw: []byte(`"four"`)}, wantErr: "must be an integer"},
		{name: "an unknown enum value", edit: Edit{Path: []string{"editor", "cursor_style"}, Raw: []byte(`"blink"`)}, wantErr: "must be"},
		{name: "a valid enum value", edit: Edit{Path: []string{"editor", "cursor_style"}, Raw: []byte(`"bar"`)}},
		{name: "a project editor option", edit: Edit{Path: []string{"editor", "tab_size"}, Raw: []byte("2")}, project: true},
		{name: "the keymap in the project layer", edit: Edit{Path: []string{"keymap"}, Raw: []byte(`"emacs"`)}, project: true, wantErr: "project config cannot set keymap"},
		{name: "a null value", edit: Edit{Path: []string{"theme"}, Raw: []byte("null")}, wantErr: "null is not a valid value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diags := ValidateEdits([]Edit{tt.edit}, tt.project)
			if tt.wantErr == "" {
				if len(diags) != 0 {
					t.Fatalf("ValidateEdits = %v, want none", diags)
				}
				return
			}
			if len(diags) == 0 || !strings.Contains(diags[0].String(), tt.wantErr) {
				t.Fatalf("ValidateEdits = %v, want a diagnostic containing %q", diags, tt.wantErr)
			}
		})
	}
}

func TestSettingsTableIsCoherent(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for _, s := range Settings() {
		key := s.Key()
		if seen[key] {
			t.Errorf("duplicate setting key %q", key)
		}
		seen[key] = true
		if len(s.Path) == 0 || s.Section == "" || s.Name() == "" {
			t.Errorf("%q: missing path, name or section", key)
		}
		switch s.Kind {
		case SettingEnum:
			if len(s.Choices) == 0 && s.ChoicesFrom == "" {
				t.Errorf("%q: an enum needs choices", key)
			}
		case SettingInt:
			if s.Max != 0 && s.Max < s.Min {
				t.Errorf("%q: max %d below min %d", key, s.Max, s.Min)
			}
		}
		if s.Apply != ApplyLive && s.Apply != ApplyRestart {
			t.Errorf("%q: unknown apply %q", key, s.Apply)
		}
		if s.Layers&(LayerUser|LayerProject) == 0 {
			t.Errorf("%q: no writable layer", key)
		}
	}
	if len(Settings()) < 40 {
		t.Errorf("the schema has %d settings, want the ~40 scalar settings of the slice", len(Settings()))
	}
	// Every setting's encoded current value is one the table accepts. keymap has
	// no default in the file (empty means the platform preset), so it has no
	// value to round-trip.
	for _, s := range Settings() {
		if s.Value(Defaults()) == "" {
			continue
		}
		raw, err := s.Encode(s.Value(Defaults()))
		if err != nil {
			t.Errorf("%s: current value %q does not encode: %v", s.Key(), s.Value(Defaults()), err)
			continue
		}
		if diags := ValidateEdits([]Edit{{Path: s.Path, Raw: raw}}, false); len(diags) != 0 {
			t.Errorf("%s: current value does not validate: %v", s.Key(), diags)
		}
		if s.Layers.Has(LayerProject) {
			if diags := ValidateEdits([]Edit{{Path: s.Path, Raw: raw}}, true); len(diags) != 0 {
				t.Errorf("%s: current value does not validate in the project layer: %v", s.Key(), diags)
			}
		}
	}
}
