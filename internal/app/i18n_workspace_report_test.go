package app

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestWorkspaceReportsUseTheCatalogue checks the extracted app workspace reports compose
// byte-identically in Chinese (gvim-only fallback), including the quoted label and the
// eviction capture suffix. Not parallel: locale.
func TestWorkspaceReportsUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	got := appTextVars("gvim.workspace.report_tabs_changed", "Extracted %{count} tabs into workspace %{label}; %{changed} changed and stayed open", map[string]any{"count": 2, "label": "\"ws\"", "changed": 1})
	if got != "Extracted 2 tabs into workspace \"ws\"; 1 changed and stayed open" {
		t.Errorf("tabs report = %q", got)
	}
	got = appTextVars("gvim.workspace.report_evicted_capture", "%{message}; its capture write failed (%{error}) but the fallback save kept the session", map[string]any{"message": "base", "error": "disk"})
	if got != "base; its capture write failed (disk) but the fallback save kept the session" {
		t.Errorf("capture report = %q", got)
	}
	for _, key := range []string{"gvim.workspace.report_tabs", "gvim.workspace.report_tabs_changed", "gvim.workspace.report_named", "gvim.workspace.report_named_changed", "gvim.workspace.report_terminal_tab", "gvim.workspace.report_terminal_tab_changed", "gvim.workspace.report_terminal_tab_revives", "gvim.workspace.report_count_noun", "gvim.workspace.report_evicted", "gvim.workspace.report_evicted_capture", "gvim.workspace.report_closed_running", "gvim.workspace.report_closed_dormant", "gvim.workspace.report_renamed"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
