package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// TestContentsFoldBufferParses checks the native contents.fold_buffer key: user
// layer only, true/false, rejected unknown keys and non-booleans, and the project
// layer accepted and ignored like the retired plugin's settings.
func TestContentsFoldBufferParses(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{"contents": {"fold_buffer": true}}`)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !cfg.ContentsFoldBuffer() {
		t.Fatal("contents.fold_buffer = true did not take effect")
	}
	cfg, diags = loadBody(t, `{"contents": {"fold_buffer": false}}`)
	if len(diags) != 0 || cfg.ContentsFoldBuffer() {
		t.Fatalf("contents.fold_buffer = false: diags %v effective %v", diags, cfg.ContentsFoldBuffer())
	}
	// A project file's contents object is accepted and ignored (the section is the
	// retired plugin's, and plugin settings are the user's).
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"contents": {"fold_buffer": true, "who_knows": 1}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 || cfg.ContentsFoldBuffer() {
		t.Fatalf("project contents: diags %v effective %v", diags, cfg.ContentsFoldBuffer())
	}
}

// TestContentsFoldBufferRejects checks the bad values: an unknown key, a null and a
// non-boolean are config diagnostics, and the default stays.
func TestContentsFoldBufferRejects(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ body, key string }{
		{`{"contents": {"fold": true}}`, "contents.fold"},
		{`{"contents": {"fold_buffer": null}}`, "contents.fold_buffer"},
		{`{"contents": {"fold_buffer": "yes"}}`, "contents.fold_buffer"},
		{`{"contents": "on"}`, "contents"},
	} {
		cfg, diags := loadBody(t, tt.body)
		if len(diags) != 1 || diags[0].Key != tt.key {
			t.Errorf("%s: diags = %+v, want one for %s", tt.body, diags, tt.key)
		}
		if cfg.ContentsFoldBuffer() {
			t.Errorf("%s: a rejected value changed the setting", tt.body)
		}
	}
}

// TestContentsFoldBufferFallsBackToThePluginKey checks the compatibility path: the
// retired plugin's plugins.markdown_toc.settings.fold_buffer still works, and the
// native key wins when both are set.
func TestContentsFoldBufferFallsBackToThePluginKey(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{"plugins": {"markdown_toc": {"settings": {"fold_buffer": true}}}}`)
	if len(diags) != 0 || !cfg.ContentsFoldBuffer() {
		t.Fatalf("plugin path: diags %v effective %v", diags, cfg.ContentsFoldBuffer())
	}
	cfg, diags = loadBody(t, `{"plugins": {"markdown_toc": {"settings": {"fold_buffer": true}}}, "contents": {"fold_buffer": false}}`)
	if len(diags) != 0 || cfg.ContentsFoldBuffer() {
		t.Fatalf("native override: diags %v effective %v", diags, cfg.ContentsFoldBuffer())
	}
}

// TestContentsFoldBufferSetting checks the Settings-page row: it exists, is
// user-only, applies on restart, and shows the effective value.
func TestContentsFoldBufferSetting(t *testing.T) {
	t.Parallel()
	setting, ok := SettingByKey("contents.fold_buffer")
	if !ok {
		t.Fatal("contents.fold_buffer is not in the settings table")
	}
	if setting.Section != SectionContents || setting.Kind != SettingBool {
		t.Errorf("setting = %+v, want a boolean row in the Contents section", setting)
	}
	if setting.Layers != LayerUser {
		t.Errorf("layers = %v, want user-only (the retired plugin's settings are user-only)", setting.Layers)
	}
	if setting.Apply != ApplyRestart {
		t.Errorf("apply = %v, want restart (the running section reads it at startup)", setting.Apply)
	}
	cfg := Defaults()
	if got := setting.Value(cfg); got != "off" {
		t.Errorf("default value = %q, want off", got)
	}
	plugin := Defaults()
	plugin.Plugins = map[string]PluginConfig{"markdown_toc": {Settings: map[string]any{"fold_buffer": true}}}
	if got := setting.Value(plugin); got != "on" {
		t.Errorf("plugin fallback value = %q, want on", got)
	}
}

// TestContentsFoldBufferSettingEdit checks the write path the Settings page uses:
// config.ValidateEdits accepts true/false for the user layer, rejects a non-boolean,
// and the project layer is accepted and ignored like a plugin setting.
func TestContentsFoldBufferSettingEdit(t *testing.T) {
	t.Parallel()
	edit := func(raw string) []Edit {
		return []Edit{{Path: []string{"contents", "fold_buffer"}, Raw: json.RawMessage(raw)}}
	}
	for _, raw := range []string{"true", "false"} {
		if diags := ValidateEdits(edit(raw), false); len(diags) != 0 {
			t.Errorf("ValidateEdits(%s) = %v, want none", raw, diags)
		}
	}
	if diags := ValidateEdits(edit(`"yes"`), false); len(diags) != 1 || diags[0].Key != "contents.fold_buffer" {
		t.Errorf(`ValidateEdits("yes") = %v, want one diagnostic for contents.fold_buffer`, diags)
	}
	if diags := ValidateEdits(edit("true"), true); len(diags) != 0 {
		t.Errorf("ValidateEdits(project) = %v, want the accepted-ignored project layer", diags)
	}
}
