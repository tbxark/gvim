package config

import (
	"strings"
	"testing"
)

// TestExplorerWidthForms checks file_explorer.width's two forms and the legacy
// fraction, as Fresh's ExplorerWidth deserializer accepts them.
func TestExplorerWidthForms(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ExplorerWidth
	}{
		{"percent string", "{\"file_explorer\": {\"width\": \"30%\"}}", ExplorerWidth{Percent: true, Value: 30}},
		{"percent with a space", "{\"file_explorer\": {\"width\": \"30 %\"}}", ExplorerWidth{Percent: true, Value: 30}},
		{"column string", "{\"file_explorer\": {\"width\": \"24\"}}", ExplorerWidth{Value: 24}},
		{"integer is a percent", "{\"file_explorer\": {\"width\": 25}}", ExplorerWidth{Percent: true, Value: 25}},
		{"legacy fraction", "{\"file_explorer\": {\"width\": 0.3}}", ExplorerWidth{Percent: true, Value: 30}},
		{"float outside the fraction range is a percent", "{\"file_explorer\": {\"width\": 30.0}}", ExplorerWidth{Percent: true, Value: 30}},
		{"zero columns", "{\"file_explorer\": {\"width\": \"0\"}}", ExplorerWidth{Value: 0}},
		{"full percent", "{\"file_explorer\": {\"width\": \"100%\"}}", ExplorerWidth{Percent: true, Value: 100}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, diags := loadBody(t, tt.body)
			if len(diags) != 0 {
				t.Fatalf("diagnostics: %v", diags)
			}
			if cfg.FileExplorer.Width != tt.want {
				t.Errorf("width = %+v, want %+v", cfg.FileExplorer.Width, tt.want)
			}
		})
	}
}

// TestExplorerWidthRejects checks the bad widths: they are a config diagnostic and
// leave the default in place.
func TestExplorerWidthRejects(t *testing.T) {
	for _, body := range []string{
		"{\"file_explorer\": {\"width\": \"wide\"}}",
		"{\"file_explorer\": {\"width\": \"120%\"}}",
		"{\"file_explorer\": {\"width\": 200}}",
		"{\"file_explorer\": {\"width\": \"\"}}",
		"{\"file_explorer\": {\"width\": null}}",
		"{\"file_explorer\": {\"width\": true}}",
		"{\"file_explorer\": {\"width\": \"-4\"}}",
	} {
		cfg, diags := loadBody(t, body)
		if len(diags) != 1 || diags[0].Key != "file_explorer.width" {
			t.Errorf("%s: diags = %+v, want one for file_explorer.width", body, diags)
		}
		if want := DefaultFileExplorer().Width; cfg.FileExplorer.Width != want {
			t.Errorf("%s: a rejected width changed the config to %+v", body, cfg.FileExplorer.Width)
		}
	}
}

// TestFileExplorerDefaultsAreFreshs checks the section defaults: 30%, left, follow
// off, auto-open on, compact on.
func TestFileExplorerDefaultsAreFreshs(t *testing.T) {
	cfg := Defaults()
	want := FileExplorerConfig{
		Width:                     ExplorerWidth{Percent: true, Value: 30},
		Side:                      "left",
		AutoOpenOnLastBufferClose: true,
		CompactDirectories:        true,
	}
	if cfg.FileExplorer != want {
		t.Fatalf("file_explorer defaults = %+v, want %+v", cfg.FileExplorer, want)
	}
}

// TestFileExplorerBehaviourKeys checks the boolean and side keys, and that the
// deferred Fresh keys still load without a diagnostic.
func TestFileExplorerBehaviourKeys(t *testing.T) {
	body := "{\"file_explorer\": {" +
		"\"side\": \"right\", \"follow_active_buffer\": true," +
		"\"auto_open_on_last_buffer_close\": false, \"compact_directories\": false," +
		"\"respect_gitignore\": false, \"preview_tabs\": false," +
		"\"custom_ignore_patterns\": [\"build/\"]," +
		"\"tree_indicator_collapsed\": \">\", \"tree_indicator_expanded\": \"v\"}}"
	cfg, diags := loadBody(t, body)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	got := cfg.FileExplorer
	if got.Side != "right" || !got.FollowActiveBuffer || got.AutoOpenOnLastBufferClose || got.CompactDirectories {
		t.Errorf("file_explorer = %+v", got)
	}
}

// TestFileExplorerSideRejects checks a bad side name.
func TestFileExplorerSideRejects(t *testing.T) {
	_, diags := loadBody(t, "{\"file_explorer\": {\"side\": \"middle\"}}")
	if len(diags) != 1 || diags[0].Key != "file_explorer.side" || !strings.Contains(diags[0].Message, "left") {
		t.Fatalf("diags = %+v, want one for file_explorer.side", diags)
	}
}
