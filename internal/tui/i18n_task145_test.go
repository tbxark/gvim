package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestTask145StringsUseTheCatalogue checks the settings action notices, the remote
// large-file status and the remaining picker footer: Chinese shows the explicit English
// fallback (gvim-only keys), never the bare key. Not parallel: locale.
func TestTask145StringsUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := uiTextVars("gvim.settings.notice.read_only", "Settings: %{name} is read-only — %{why}", map[string]any{"name": "theme", "why": "builtin"}); got != "Settings: theme is read-only — builtin" {
		t.Errorf("settings notice = %q", got)
	}
	if got := uiTextVars("gvim.remote.large_file_error", "Remote large file: %{error}", map[string]any{"error": "boom"}); got != "Remote large file: boom" {
		t.Errorf("remote large-file notice = %q", got)
	}
	if got := uiText("gvim.picker.run_agent_footer", "Terminal: Run Agent — Enter starts the agent · Esc closes"); got != "Terminal: Run Agent — Enter starts the agent · Esc closes" {
		t.Errorf("picker footer = %q", got)
	}
	if got := uiText("gvim.settings.value.configured", "configured"); got != "configured" {
		t.Errorf("settings value = %q", got)
	}
}
