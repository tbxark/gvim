package config

import (
	"encoding/json"
	"path/filepath"
	"strconv"
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
// user-only, applies live, and shows the effective value.
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
	if setting.Apply != ApplyLive {
		t.Errorf("apply = %v, want live (the Contents section republishes when it is saved)", setting.Apply)
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

// TestContentsFoldBufferSettingIsLive checks the Settings schema row: fold_buffer
// applies to the running session (the Contents section republishes on save and
// reload), not only at the next start.
func TestContentsFoldBufferSettingIsLive(t *testing.T) {
	t.Parallel()
	setting, ok := SettingByKey("contents.fold_buffer")
	if !ok {
		t.Fatal("contents.fold_buffer is not in the settings schema")
	}
	if setting.Apply != ApplyLive {
		t.Errorf("contents.fold_buffer applies %v, want live", setting.Apply)
	}
}

// TestContentsNativeSettingsDefaultsAndParsing checks the native contents.follow,
// contents.auto_open and contents.rows keys: Fresh's defaults, valid values, and
// that the native keys win over the retired plugin's settings path.
func TestContentsNativeSettingsDefaultsAndParsing(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{}`)
	if len(diags) != 0 || cfg.ContentsFollow() != ContentsFollowCursor || !cfg.ContentsAutoOpen() || cfg.ContentsRows() != DefaultContentsRows {
		t.Fatalf("defaults: diags %v follow %q auto %v rows %d", diags, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows())
	}
	cfg, diags = loadBody(t, `{"contents": {"follow": "scroll", "auto_open": false, "rows": 25}}`)
	if len(diags) != 0 || cfg.ContentsFollow() != ContentsFollowScroll || cfg.ContentsAutoOpen() || cfg.ContentsRows() != 25 {
		t.Fatalf("native values: diags %v follow %q auto %v rows %d", diags, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows())
	}
	// A project file's contents object is accepted and ignored, native keys included.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"contents": {"follow": "scroll", "auto_open": false, "rows": 30, "who_knows": 1}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 || cfg.ContentsFollow() != ContentsFollowCursor || !cfg.ContentsAutoOpen() || cfg.ContentsRows() != DefaultContentsRows {
		t.Fatalf("project contents: diags %v follow %q auto %v rows %d", diags, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows())
	}
}

// TestContentsNativeSettingsRejects checks the diagnostics for bad values and keeps
// the defaults.
func TestContentsNativeSettingsRejects(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ body, key string }{
		{`{"contents": {"follow": "sideways"}}`, "contents.follow"},
		{`{"contents": {"follow": 1}}`, "contents.follow"},
		{`{"contents": {"auto_open": "yes"}}`, "contents.auto_open"},
		{`{"contents": {"rows": 0}}`, "contents.rows"},
		{`{"contents": {"rows": 201}}`, "contents.rows"},
		{`{"contents": {"rows": 2.5}}`, "contents.rows"},
		{`{"contents": {"rows": "10"}}`, "contents.rows"},
	} {
		cfg, diags := loadBody(t, tt.body)
		if len(diags) != 1 || diags[0].Key != tt.key {
			t.Errorf("%s: diags = %+v, want one for %s", tt.body, diags, tt.key)
		}
		if cfg.ContentsFollow() != ContentsFollowCursor || !cfg.ContentsAutoOpen() || cfg.ContentsRows() != DefaultContentsRows {
			t.Errorf("%s: a rejected value changed a setting (follow %q auto %v rows %d)", tt.body, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows())
		}
	}
}

// TestContentsNativeSettingsFallBackToThePluginPath checks Fresh-compatible plugin
// settings (camelCase names Fresh's plugin declares and the snake_case names the old
// docs used) as the fallback, with the native keys winning.
func TestContentsNativeSettingsFallBackToThePluginPath(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"plugins": {"markdown_toc": {"settings": {"follow": "scroll", "autoOpen": false, "rows": 40, "foldBuffer": true}}}}`,
		`{"plugins": {"markdown_toc": {"settings": {"follow": "scroll", "auto_open": false, "rows": 40, "fold_buffer": true}}}}`,
	} {
		cfg, diags := loadBody(t, body)
		if len(diags) != 0 || cfg.ContentsFollow() != ContentsFollowScroll || cfg.ContentsAutoOpen() || cfg.ContentsRows() != 40 || !cfg.ContentsFoldBuffer() {
			t.Fatalf("plugin fallback %s: diags %v follow %q auto %v rows %d fold %v", body, diags, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows(), cfg.ContentsFoldBuffer())
		}
	}
	cfg, diags := loadBody(t, `{"plugins": {"markdown_toc": {"settings": {"follow": "scroll", "autoOpen": false, "rows": 40}}}, "contents": {"follow": "cursor", "auto_open": true, "rows": 5}}`)
	if len(diags) != 0 || cfg.ContentsFollow() != ContentsFollowCursor || !cfg.ContentsAutoOpen() || cfg.ContentsRows() != 5 {
		t.Fatalf("native override: diags %v follow %q auto %v rows %d", diags, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows())
	}
}

// TestContentsSettingsRows checks the three native rows: user-only, live, the right
// editor kinds/bounds, and the effective values incl. the plugin fallback.
func TestContentsSettingsRows(t *testing.T) {
	t.Parallel()
	follow, ok := SettingByKey("contents.follow")
	if !ok || follow.Section != SectionContents || follow.Kind != SettingEnum || follow.Layers != LayerUser || follow.Apply != ApplyLive {
		t.Fatalf("contents.follow = %+v, ok %v", follow, ok)
	}
	if len(follow.Choices) != 2 || follow.Choices[0] != ContentsFollowCursor || follow.Choices[1] != ContentsFollowScroll {
		t.Fatalf("contents.follow choices = %v", follow.Choices)
	}
	auto, ok := SettingByKey("contents.auto_open")
	if !ok || auto.Kind != SettingBool || auto.Layers != LayerUser || auto.Apply != ApplyLive {
		t.Fatalf("contents.auto_open = %+v, ok %v", auto, ok)
	}
	rows, ok := SettingByKey("contents.rows")
	if !ok || rows.Kind != SettingInt || rows.Min != MinContentsRows || rows.Max != MaxContentsRows || rows.Layers != LayerUser || rows.Apply != ApplyLive {
		t.Fatalf("contents.rows = %+v, ok %v", rows, ok)
	}
	cfg := Defaults()
	if follow.Value(cfg) != ContentsFollowCursor || auto.Value(cfg) != "on" || rows.Value(cfg) != strconv.Itoa(DefaultContentsRows) {
		t.Fatalf("default row values = %q %q %q", follow.Value(cfg), auto.Value(cfg), rows.Value(cfg))
	}
	plugin := Defaults()
	plugin.Plugins = map[string]PluginConfig{"markdown_toc": {Settings: map[string]any{
		"follow": "scroll", "autoOpen": false, "rows": float64(25),
	}}}
	if follow.Value(plugin) != ContentsFollowScroll || auto.Value(plugin) != "off" || rows.Value(plugin) != "25" {
		t.Fatalf("plugin row values = %q %q %q", follow.Value(plugin), auto.Value(plugin), rows.Value(plugin))
	}
	// Edits validate through the schema; the project layer is refused for these rows.
	for _, tc := range []struct {
		key  string
		edit Edit
	}{
		{"contents.follow", Edit{Path: []string{"contents", "follow"}, Raw: json.RawMessage(`"scroll"`)}},
		{"contents.auto_open", Edit{Path: []string{"contents", "auto_open"}, Raw: json.RawMessage("false")}},
		{"contents.rows", Edit{Path: []string{"contents", "rows"}, Raw: json.RawMessage("20")}},
	} {
		if diags := ValidateEdits([]Edit{tc.edit}, false); len(diags) != 0 {
			t.Errorf("ValidateEdits(%s, user) = %v", tc.key, diags)
		}
		// The project layer of contents.* is accepted and ignored (the section is a
		// retired plugin's, user-layer only); the row's Layers still prevents the
		// Settings page from writing it there.
		if diags := ValidateEdits([]Edit{tc.edit}, true); len(diags) != 0 {
			t.Errorf("ValidateEdits(%s, project) = %v, want accepted-and-ignored", tc.key, diags)
		}
	}
	// Bound checks come from the row: follow rejects a non-choice and rows rejects
	// out-of-range values before the file is written.
	if diags := ValidateEdits([]Edit{{Path: []string{"contents", "follow"}, Raw: json.RawMessage(`"sideways"`)}}, false); len(diags) != 1 {
		t.Errorf("follow sideways diagnostics = %v", diags)
	}
	if diags := ValidateEdits([]Edit{{Path: []string{"contents", "rows"}, Raw: json.RawMessage("0")}}, false); len(diags) != 1 {
		t.Errorf("rows 0 diagnostics = %v", diags)
	}
}

// TestContentsEnabledIsUserLayerOnly checks the retired plugin's enabled flag: a user
// file may disable the native section, but a project file's enabled=false must not
// (the project layer of a plugin can only disable real plugins; the native Contents
// section is user-layer only).
func TestContentsEnabledIsUserLayerOnly(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{}`)
	if len(diags) != 0 || !cfg.ContentsEnabled() {
		t.Fatalf("default enabled: diags %v enabled %v", diags, cfg.ContentsEnabled())
	}
	cfg, diags = loadBody(t, `{"plugins": {"markdown_toc": {"enabled": false}}}`)
	if len(diags) != 0 || cfg.ContentsEnabled() {
		t.Fatalf("user enabled=false: diags %v enabled %v", diags, cfg.ContentsEnabled())
	}
	// Project enabled=false only: accepted (it can disable a real plugin) but ignored
	// for the native section.
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"plugins": {"markdown_toc": {"enabled": false}}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 || !cfg.ContentsEnabled() {
		t.Fatalf("project enabled=false (no user file): diags %v enabled %v", diags, cfg.ContentsEnabled())
	}
	// User true + project false: the user's choice wins.
	user := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, user, `{"plugins": {"markdown_toc": {"enabled": true}}}`)
	cfg, diags, err = Load(Options{Root: root, UserFile: user})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 || !cfg.ContentsEnabled() {
		t.Fatalf("user true + project false: diags %v enabled %v", diags, cfg.ContentsEnabled())
	}
}
