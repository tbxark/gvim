package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

// TestMarkdownTOCConfigReadsTheNativeFoldBuffer checks the Contents section's
// fold_buffer wiring: the native contents.fold_buffer key wins, Fresh's retired
// plugin settings path still supplies the value otherwise, and the plugin's other
// settings keep parsing.
func TestMarkdownTOCConfigReadsTheNativeFoldBuffer(t *testing.T) {
	t.Parallel()
	if got := markdownTOCConfig(config.Defaults()).Settings.FoldBuffer; got {
		t.Error("the default fold_buffer is on")
	}
	plugin := config.Defaults()
	plugin.Plugins = map[string]config.PluginConfig{
		"markdown_toc": {Settings: map[string]any{"fold_buffer": true, "rows": float64(20)}},
	}
	got := markdownTOCConfig(plugin).Settings
	if !got.FoldBuffer || got.Rows != 20 {
		t.Errorf("plugin settings = %+v, want fold_buffer on and rows 20", got)
	}
	// The native key overrides the plugin path.
	off := false
	native := plugin
	native.Contents.FoldBuffer = &off
	if got := markdownTOCConfig(native).Settings.FoldBuffer; got {
		t.Error("the native contents.fold_buffer = false did not override the plugin setting")
	}
	on := true
	native.Contents.FoldBuffer = &on
	plain := config.Defaults()
	plain.Contents.FoldBuffer = &on
	if got := markdownTOCConfig(plain).Settings.FoldBuffer; !got {
		t.Error("the native contents.fold_buffer = true did not turn the setting on")
	}
}

// TestMarkdownTOCConfigIsUserLayerOnly checks the native section's enabled/settings
// wiring: the user file may disable it and set the native contents.* keys, but a
// project file's plugins.markdown_toc.enabled=false or settings must not.
func TestMarkdownTOCConfigIsUserLayerOnly(t *testing.T) {
	t.Parallel()
	// A project file alone cannot disable the native section.
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gvim", "config.json"), []byte(`{"plugins": {"markdown_toc": {"enabled": false, "settings": {"follow": "scroll"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := config.Load(config.Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil || len(diags) != 0 {
		t.Fatalf("project load = %v, %v", err, diags)
	}
	got := markdownTOCConfig(cfg)
	if !got.Enabled {
		t.Fatal("a project plugin enabled=false disabled the native Contents section")
	}
	if got.Settings.Follow != 0 { // FollowCursor is the default
		t.Fatalf("project plugin settings leaked into the native section: %+v", got.Settings)
	}
	// The user file may disable it and set the native keys, which win over plugin ones.
	user := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(user, []byte(`{"plugins": {"markdown_toc": {"enabled": false, "settings": {"follow": "scroll", "autoOpen": false, "rows": 40}}}, "contents": {"follow": "cursor", "auto_open": true, "rows": 25}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err = config.Load(config.Options{UserFile: user})
	if err != nil || len(diags) != 0 {
		t.Fatalf("user load = %v, %v", err, diags)
	}
	got = markdownTOCConfig(cfg)
	if got.Enabled {
		t.Fatal("a user plugin enabled=false did not disable the native section")
	}
	if got.Settings.Follow != 0 || !got.Settings.AutoOpen || got.Settings.Rows != 25 {
		t.Fatalf("native settings = %+v, want cursor/on/25", got.Settings)
	}
}
