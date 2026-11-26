package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tbxark/gvim/config"
)

// TestMarkdownTOCConfigReadsNativeSettings checks the Contents section's native
// contents.* settings wiring.
func TestMarkdownTOCConfigReadsNativeSettings(t *testing.T) {
	t.Parallel()
	if got := markdownTOCConfig(config.Defaults()).Settings.FoldBuffer; got {
		t.Error("the default fold_buffer is on")
	}
	on := true
	rows := 20
	depth := 2
	cfg := config.Defaults()
	cfg.Contents.FoldBuffer = &on
	cfg.Contents.Rows = &rows
	cfg.Contents.MaxDepth = &depth
	got := markdownTOCConfig(cfg).Settings
	if !got.FoldBuffer || got.Rows != 20 || got.MaxDepth != 2 {
		t.Errorf("settings = %+v, want fold_buffer on, rows 20, max_depth 2", got)
	}
}

// TestMarkdownTOCConfigIsUserLayerOnly checks the native section's enabled/settings
// wiring: only the user file may disable it or set contents.* keys; a project
// file's contents object is ignored.
func TestMarkdownTOCConfigIsUserLayerOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".gvim"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gvim", "config.json"), []byte(`{"contents": {"enabled": false, "follow": "scroll"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err := config.Load(config.Options{Root: root, UserFile: filepath.Join(t.TempDir(), "absent.json")})
	if err != nil || len(diags) != 0 {
		t.Fatalf("project load = %v, %v", err, diags)
	}
	got := markdownTOCConfig(cfg)
	if !got.Enabled {
		t.Fatal("a project contents.enabled=false disabled the native Contents section")
	}
	if got.Settings.Follow != 0 {
		t.Fatalf("project contents settings leaked into the native section: %+v", got.Settings)
	}

	user := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(user, []byte(`{"contents": {"enabled": false, "follow": "scroll", "auto_open": false, "rows": 25}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, diags, err = config.Load(config.Options{UserFile: user})
	if err != nil || len(diags) != 0 {
		t.Fatalf("user load = %v, %v", err, diags)
	}
	got = markdownTOCConfig(cfg)
	if got.Enabled {
		t.Fatal("user contents.enabled=false did not disable the native section")
	}
	if got.Settings.Follow.String() != "scroll" || got.Settings.AutoOpen || got.Settings.Rows != 25 {
		t.Fatalf("native settings = %+v, want scroll/off/25", got.Settings)
	}
}
