package config

import (
	"path/filepath"
	"testing"
)

// TestLoadUntrustedSkipsTheProjectLayer checks the workspace-trust gate of the
// config: a workspace that is not trusted is configured by the user file only.
// The project file is not even read, so its diagnostics do not appear either.
func TestLoadUntrustedSkipsTheProjectLayer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	root := filepath.Join(dir, "proj")
	writeFile(t, userFile, `{"theme": "light", "tab_size": 2}`)
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{
		"theme": "dark",
		"line_numbers": false,
		"keybindings": {"a": "x"},
		"plugins": {"demo": {"enabled": true}}
	}`)

	cfg, diags, err := Load(Options{Root: root, UserFile: userFile, Untrusted: true})
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if cfg.Theme != "light" {
		t.Errorf("theme = %q, want the user layer's light", cfg.Theme)
	}
	if cfg.TabSize != 2 {
		t.Errorf("tab_size = %d, want the user layer's 2", cfg.TabSize)
	}
	// Defaults stand where only the project file would have changed them.
	if !cfg.LineNumbers {
		t.Error("line_numbers = false, want the default true: the project layer was read")
	}
	if len(cfg.Plugins) != 0 {
		t.Errorf("plugins = %v, want none: the project layer was read", cfg.Plugins)
	}
	if len(diags) != 0 {
		t.Errorf("diags = %v, want none: the project layer was read", diags)
	}
	if src := cfg.Source("theme"); src != userFile {
		t.Errorf("Source(theme) = %q, want %q", src, userFile)
	}
}

// TestLoadTrustedReadsTheProjectLayer pins the zero value: a caller without a
// trust decision reads the project layer as before.
func TestLoadTrustedReadsTheProjectLayer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	userFile := filepath.Join(dir, "config.json")
	root := filepath.Join(dir, "proj")
	writeFile(t, userFile, `{"theme": "light", "tab_size": 2}`)
	writeFile(t, filepath.Join(root, ".gvim", "config.json"), `{"theme": "dark", "line_numbers": false}`)

	cfg, diags, err := Load(Options{Root: root, UserFile: userFile})
	if err != nil {
		t.Fatalf("Load error = %v", err)
	}
	if len(diags) != 0 {
		t.Fatalf("diags = %v, want none", diags)
	}
	if cfg.Theme != "dark" {
		t.Errorf("theme = %q, want the project layer's dark", cfg.Theme)
	}
	if cfg.LineNumbers {
		t.Error("line_numbers = true, want false from the project layer")
	}
	if cfg.TabSize != 2 {
		t.Errorf("tab_size = %d, want the user layer's 2", cfg.TabSize)
	}
}
