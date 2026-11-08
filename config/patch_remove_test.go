package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// loadBytes writes data to a temporary config file and loads it.
func loadBytes(t *testing.T, data []byte) Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := Load(Options{UserFile: path})
	if err != nil || len(diags) != 0 {
		t.Fatalf("Load after patch = %v, %v", err, diags)
	}
	return cfg
}

// patchAndLoad applies edits to body, then loads the result.
func patchAndLoad(t *testing.T, body string, edits []Edit) Config {
	t.Helper()
	data, err := PatchBytes([]byte(body), edits)
	if err != nil {
		t.Fatalf("PatchBytes error = %v", err)
	}
	return loadBytes(t, data)
}

func TestPatchBytesRemove(t *testing.T) {
	t.Parallel()
	body := `// keep me
{
  "theme": "dark",
  "editor": {
    "tab_size": 2, // old
    "line_numbers": false
  }
}
`
	got := patched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Remove: true}})
	if strings.Contains(got, "tab_size") {
		t.Errorf("the removed key is still there:\n%s", got)
	}
	for _, want := range []string{"// keep me", "// old", `"theme": "dark"`, `"line_numbers": false`} {
		if !strings.Contains(got, want) {
			t.Errorf("patched file misses %q:\n%s", want, got)
		}
	}
	cfg := patchAndLoad(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Remove: true}})
	if cfg.TabSize != Defaults().TabSize {
		t.Errorf("tab size = %d, want the default %d", cfg.TabSize, Defaults().TabSize)
	}
	if cfg.LineNumbers {
		t.Error("the sibling line_numbers key did not survive")
	}
}

func TestPatchBytesRemoveCascadesEmptyParents(t *testing.T) {
	t.Parallel()
	body := `{
  "theme": "dark",
  "languages": {
    "go": {
      "tab_size": 8
    }
  }
}
`
	got := patched(t, body, []Edit{{Path: []string{"languages", "go", "tab_size"}, Remove: true}})
	if strings.Contains(got, "languages") || strings.Contains(got, "tab_size") {
		t.Errorf("the empty parents were not removed:\n%s", got)
	}
	if !strings.Contains(got, `"theme": "dark"`) {
		t.Errorf("the sibling key was removed:\n%s", got)
	}
	cfg := patchAndLoad(t, body, []Edit{{Path: []string{"languages", "go", "tab_size"}, Remove: true}})
	if len(cfg.Languages) != 0 {
		t.Errorf("languages = %v, want none", cfg.Languages)
	}
}

func TestPatchBytesRemoveKeepsParentsWithSiblings(t *testing.T) {
	t.Parallel()
	body := `{
  "languages": {
    "go": {
      "tab_size": 8,
      "use_tabs": true
    }
  }
}
`
	cfg := patchAndLoad(t, body, []Edit{{Path: []string{"languages", "go", "tab_size"}, Remove: true}})
	entry := cfg.Languages["go"]
	if entry.TabSize != nil {
		t.Errorf("tab_size = %v, want the override removed", *entry.TabSize)
	}
	if entry.UseTabs == nil || !*entry.UseTabs {
		t.Errorf("use_tabs = %v, want the sibling override kept", entry.UseTabs)
	}
}

func TestPatchBytesRemoveAbsentKeyIsANoOp(t *testing.T) {
	t.Parallel()
	const body = `{
  "theme": "dark",
  "editor": 5
}
`
	got := patched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Remove: true}})
	if got != body {
		t.Errorf("removing an absent key changed the file:\n%s", got)
	}
}

func TestPatchBytesSetAndRemoveInOnePatch(t *testing.T) {
	t.Parallel()
	body := `{
  "editor": {
    "line_numbers": true,
    "tab_size": 2
  }
}
`
	cfg := patchAndLoad(t, body, []Edit{
		{Path: []string{"theme"}, Raw: []byte(`"light"`)},
		{Path: []string{"editor", "line_numbers"}, Remove: true},
	})
	if cfg.Theme != "light" {
		t.Errorf("theme = %q, want the set value", cfg.Theme)
	}
	if !cfg.LineNumbers {
		t.Error("line_numbers = false, want the removed override to fall back to the default true")
	}
	if cfg.TabSize != 2 {
		t.Errorf("tab size = %d, want the untouched 2", cfg.TabSize)
	}
}

func TestPatchBytesRemoveKeepsTrailingCommaStyle(t *testing.T) {
	t.Parallel()
	body := `{
  "theme": "dark",
  "editor": {
    "tab_size": 2,
  },
}
`
	cfg := patchAndLoad(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Remove: true}})
	if cfg.TabSize != Defaults().TabSize {
		t.Errorf("tab size = %d, want the default", cfg.TabSize)
	}
	got := patched(t, body, []Edit{{Path: []string{"editor", "tab_size"}, Remove: true}})
	// The editor object became empty, so it is removed; the theme keeps the
	// file's trailing-comma style.
	if strings.Contains(got, `"editor"`) {
		t.Errorf("the emptied editor object was kept:\n%s", got)
	}
	if !strings.Contains(got, `"theme": "dark",`) {
		t.Errorf("the trailing-comma style was not kept:\n%s", got)
	}
}

func TestPatchBytesRemoveRejectsAValue(t *testing.T) {
	t.Parallel()
	if _, err := PatchBytes([]byte("{}"), []Edit{{Path: []string{"theme"}, Raw: []byte(`"dark"`), Remove: true}}); err == nil {
		t.Fatal("PatchBytes accepted a reset with a value")
	}
}

func TestRemovablePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path []string
		want bool
	}{
		{[]string{"theme"}, true},
		{[]string{"editor", "tab_size"}, true},
		{[]string{"formatters", "go"}, true},
		{[]string{"formatters", ".go"}, true},
		{[]string{"formatters"}, true},
		{[]string{"languages", "go"}, true},
		{[]string{"languages", "go", "tab_size"}, true},
		{[]string{"languages", "go", "formatter"}, false},
		{[]string{"languages"}, true},
		{[]string{"terminal", "shell", "command"}, true},
		{[]string{"terminal", "shell", "args"}, true},
		{[]string{"terminal", "shell", "nope"}, false},
		{[]string{"editor", "status_bar", "left"}, true},
		{[]string{}, false},
		{[]string{"unknown"}, false},
	}
	for _, tt := range tests {
		if got := RemovablePath(tt.path); got != tt.want {
			t.Errorf("RemovablePath(%v) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestValidateStructuredEdits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		edit    Edit
		project bool
		wantErr string
	}{
		{name: "a formatter command", edit: Edit{Path: []string{"formatters", "go"}, Raw: []byte(`"gofmt"`)}},
		{name: "a formatter extension", edit: Edit{Path: []string{"formatters", ".go"}, Raw: []byte(`"gofmt"`)}},
		{name: "a formatter with an empty command", edit: Edit{Path: []string{"formatters", "go"}, Raw: []byte(`""`)}, wantErr: "must not be empty"},
		{name: "a language tab size", edit: Edit{Path: []string{"languages", "go", "tab_size"}, Raw: []byte("2")}},
		{name: "a language tab size of zero clears the override", edit: Edit{Path: []string{"languages", "go", "tab_size"}, Raw: []byte("0")}},
		{name: "a language tab size above the maximum", edit: Edit{Path: []string{"languages", "go", "tab_size"}, Raw: []byte("99")}, wantErr: "between 1 and 16"},
		{name: "a language override in the project layer", edit: Edit{Path: []string{"languages", "go", "use_tabs"}, Raw: []byte("true")}, project: true},
		{name: "a status bar element", edit: Edit{Path: []string{"editor", "status_bar", "left"}, Raw: []byte(`["{filename}", "{cursor}"]`)}},
		{name: "an unknown status bar element", edit: Edit{Path: []string{"editor", "status_bar", "left"}, Raw: []byte(`["{nope}"]`)}, wantErr: "unknown status bar element"},
		{name: "a status bar separator", edit: Edit{Path: []string{"editor", "status_bar", "separator"}, Raw: []byte(`"|"`)}},
		{name: "a percent width", edit: Edit{Path: []string{"file_explorer", "width"}, Raw: []byte(`"20%"`)}},
		{name: "a column width", edit: Edit{Path: []string{"file_explorer", "width"}, Raw: []byte(`"24"`)}},
		{name: "an explorer side", edit: Edit{Path: []string{"file_explorer", "side"}, Raw: []byte(`"right"`)}},
		{name: "an unknown explorer side", edit: Edit{Path: []string{"file_explorer", "side"}, Raw: []byte(`"middle"`)}, wantErr: "must be"},
		{name: "a terminal shell", edit: Edit{Path: []string{"terminal", "shell", "command"}, Raw: []byte(`"/bin/zsh"`)}},
		{name: "a terminal shell in the project layer", edit: Edit{Path: []string{"terminal", "shell", "command"}, Raw: []byte(`"/bin/zsh"`)}, project: true, wantErr: "project config cannot set terminal.shell"},
		{name: "the terminal mouse forwarding", edit: Edit{Path: []string{"terminal", "mouse_forwarding"}, Raw: []byte(`"alt_screen"`)}},
		{name: "an unknown terminal mouse forwarding", edit: Edit{Path: []string{"terminal", "mouse_forwarding"}, Raw: []byte(`"always"`)}, wantErr: "must be"},
		{name: "a terminal scrollback size", edit: Edit{Path: []string{"terminal", "scrollback_lines"}, Raw: []byte("5000")}},
		{name: "a terminal scrollback size above the maximum", edit: Edit{Path: []string{"terminal", "scrollback_lines"}, Raw: []byte("999999999")}, wantErr: "between 0 and 1000000"},
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

func TestStructuredSettingsHelpers(t *testing.T) {
	t.Parallel()
	if !slices.Contains(KnownLanguageIDs(), "go") || !slices.Contains(KnownLanguageIDs(), "text") {
		t.Errorf("KnownLanguageIDs = %v, want the built-in languages and text", KnownLanguageIDs())
	}
	if !slices.Contains(StatusBarElementNames(), "filename") || !slices.Contains(StatusBarElementNames(), "trust") {
		t.Errorf("StatusBarElementNames = %v, want the config's element names", StatusBarElementNames())
	}
	for _, f := range LanguageOverrideFields() {
		if _, ok := languageFields[f.Key]; !ok {
			t.Errorf("override field %q is not a languages field", f.Key)
		}
		if f.Kind == SettingPage || f.Kind == SettingEnum {
			t.Errorf("override field %q has a structured or enum kind", f.Key)
		}
	}
	for key, want := range map[string]SettingKind{
		"languages":                 SettingPage,
		"formatters":                SettingPage,
		"editor.status_bar.left":    SettingPage,
		"editor.status_bar.right":   SettingPage,
		"terminal.shell.args":       SettingPage,
		"file_explorer.width":       SettingString,
		"terminal.scrollback_lines": SettingInt,
	} {
		s, ok := SettingByKey(key)
		if !ok {
			t.Errorf("setting %q is missing", key)
			continue
		}
		if s.Kind != want {
			t.Errorf("setting %q kind = %q, want %q", key, s.Kind, want)
		}
	}
	if s, _ := SettingByKey("terminal.shell.command"); s.Layers.Has(LayerProject) {
		t.Error("terminal.shell.command offers the project layer")
	}
	if s, _ := SettingByKey("file_explorer.side"); !s.Layers.Has(LayerProject) {
		t.Error("file_explorer.side does not offer the project layer")
	}
}
