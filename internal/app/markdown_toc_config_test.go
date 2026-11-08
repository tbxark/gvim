package app

import (
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
