package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestTask129ChromeFallsBackToEnglish checks extracted plugin/theme/agent/project-search
// strings render their English text in Chinese (gvim-only keys, never the bare key), and
// guards that every referenced key is in the English catalogue.
func TestTask129ChromeFallsBackToEnglish(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := uiText("gvim.plugin_manager.cannot_reload", "No plugin can be reloaded"); got != "No plugin can be reloaded" {
		t.Errorf("plugin notice = %q", got)
	}
	if got := uiTextVars("gvim.theme_editor.saved", "Theme saved to %{ref}", map[string]any{"ref": "x.json"}); got != "Theme saved to x.json" {
		t.Errorf("theme notice = %q", got)
	}
	if got := uiText("gvim.agent_discover.scanning", "Scanning for local agent sessions…"); got != "Scanning for local agent sessions…" {
		t.Errorf("agent chrome = %q", got)
	}
	if got := uiText("gvim.project_search.cancelled", "Replacement cancelled"); got != "Replacement cancelled" {
		t.Errorf("project notice = %q", got)
	}
	for _, key := range []string{
		"gvim.project_search.confirm", "gvim.project_search.confirm_remote", "gvim.project_search.partial_remote",
		"gvim.plugin_manager.plugin_error", "gvim.plugin_manager.log_empty_plugin",
		"gvim.theme_editor.error", "gvim.theme_editor.bundled_save", "gvim.agent_discover.no_queued_launch",
	} {
		if got := i18n.T("en", key, map[string]any{"matches": 1, "match_word": "match", "files": 1, "file_word": "file", "target": "h", "error": "e", "name": "n", "ref": "r"}); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
