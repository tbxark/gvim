package config

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"
)

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
		t.Errorf("layers = %v, want user-only", setting.Layers)
	}
	if setting.Apply != ApplyLive {
		t.Errorf("apply = %v, want live", setting.Apply)
	}
	cfg := Defaults()
	if got := setting.Value(cfg); got != "off" {
		t.Errorf("default value = %q, want off", got)
	}
	on := true
	cfg.Contents.FoldBuffer = &on
	if got := setting.Value(cfg); got != "on" {
		t.Errorf("value = %q, want on", got)
	}
}

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
		t.Errorf("ValidateEdits(project) = %v, want accepted-ignored project layer", diags)
	}
}

func TestContentsNativeSettingsDefaultsAndParsing(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{}`)
	if len(diags) != 0 || cfg.ContentsFollow() != ContentsFollowCursor || !cfg.ContentsAutoOpen() || cfg.ContentsRows() != DefaultContentsRows || cfg.ContentsMaxDepth() != DefaultContentsMaxDepth {
		t.Fatalf("defaults: diags %v follow %q auto %v rows %d depth %d", diags, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows(), cfg.ContentsMaxDepth())
	}
	cfg, diags = loadBody(t, `{"contents": {"follow": "scroll", "auto_open": false, "rows": 25, "max_depth": 3}}`)
	if len(diags) != 0 || cfg.ContentsFollow() != ContentsFollowScroll || cfg.ContentsAutoOpen() || cfg.ContentsRows() != 25 || cfg.ContentsMaxDepth() != 3 {
		t.Fatalf("native values: diags %v follow %q auto %v rows %d depth %d", diags, cfg.ContentsFollow(), cfg.ContentsAutoOpen(), cfg.ContentsRows(), cfg.ContentsMaxDepth())
	}
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
		{`{"contents": {"max_depth": 0}}`, "contents.max_depth"},
		{`{"contents": {"max_depth": 7}}`, "contents.max_depth"},
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
		if diags := ValidateEdits([]Edit{tc.edit}, true); len(diags) != 0 {
			t.Errorf("ValidateEdits(%s, project) = %v, want accepted-and-ignored", tc.key, diags)
		}
	}
	if diags := ValidateEdits([]Edit{{Path: []string{"contents", "follow"}, Raw: json.RawMessage(`"sideways"`)}}, false); len(diags) != 1 {
		t.Errorf("follow sideways diagnostics = %v", diags)
	}
	if diags := ValidateEdits([]Edit{{Path: []string{"contents", "rows"}, Raw: json.RawMessage("0")}}, false); len(diags) != 1 {
		t.Errorf("rows 0 diagnostics = %v", diags)
	}
}

func TestContentsEnabledIsUserLayerOnly(t *testing.T) {
	t.Parallel()
	cfg, diags := loadBody(t, `{}`)
	if len(diags) != 0 || !cfg.ContentsEnabled() {
		t.Fatalf("default enabled: diags %v enabled %v", diags, cfg.ContentsEnabled())
	}
	cfg, diags = loadBody(t, `{"contents": {"enabled": false}}`)
	if len(diags) != 0 || cfg.ContentsEnabled() {
		t.Fatalf("user enabled=false: diags %v enabled %v", diags, cfg.ContentsEnabled())
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"contents": {"enabled": false}}`)
	cfg, diags, err := Load(Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 0 || !cfg.ContentsEnabled() {
		t.Fatalf("project enabled=false: diags %v enabled %v", diags, cfg.ContentsEnabled())
	}
}
