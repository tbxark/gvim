package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestWorkspaceNoticesUseTheCatalogue checks the extracted workspace close/notice strings:
// Chinese shows the English fallback (gvim-only keys), the running-terminal summary and the
// close question compose byte-identically. Not parallel: locale.
func TestWorkspaceNoticesUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	summary := workspaceRunningSummary([]WorkspaceRunningTerminal{{Name: "bash"}, {Name: "claude", Agent: "claude"}})
	if summary != "2 running terminals: bash, claude (agent claude)" {
		t.Errorf("running summary = %q", summary)
	}
	if got := uiText("gvim.workspace.extract_needs_daemon", "Extracting a tab needs the daemon workspace layer"); got != "Extracting a tab needs the daemon workspace layer" {
		t.Errorf("workspace notice = %q", got)
	}
	if got := uiTextVars("gvim.workspace.close_question", "Close workspace %{label}? This stops %{summary}", map[string]any{"label": "\"ws\"", "summary": "1 running terminal: bash"}); got != "Close workspace \"ws\"? This stops 1 running terminal: bash" {
		t.Errorf("close question = %q", got)
	}
	for _, key := range []string{"gvim.workspace.picker_close", "gvim.workspace.picker_rename", "gvim.workspace.label_prompt", "gvim.workspace.close_question", "gvim.workspace.close_button", "gvim.workspace.running_summary", "gvim.workspace.noun_terminal", "gvim.workspace.noun_terminals", "gvim.workspace.extract_needs_daemon", "gvim.workspace.extract_unsupported", "gvim.workspace.extract_no_other", "gvim.workspace.extract_into_existing_unsupported", "gvim.workspace.extract_terminal_unsupported", "gvim.workspace.extract_many_unsupported", "gvim.workspace.extract_no_file_tabs", "gvim.workspace.extract_none_selected", "gvim.workspace.extract_mixed_unsupported", "gvim.workspace.extract_title_single", "gvim.workspace.extract_title_terminal", "gvim.workspace.extract_title_tabs_new", "gvim.workspace.extract_title_tabs_existing", "gvim.workspace.extract_hint_select", "gvim.workspace.extract_hint_target", "gvim.workspace.extract_new_workspace_row", "gvim.workspace.extract_new_workspace_detail", "gvim.workspace.no_tabs_to_extract", "gvim.workspace.conflict"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
