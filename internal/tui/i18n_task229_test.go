package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestTask229MoveAndResolveStrings checks the extracted task-220 move confirmation/notices
// and task-216 resolve/accept refusal notices: Chinese shows the English fallback for these
// gvim-only keys (never the bare key) and the placeholders compose byte-identically.
// Not parallel: locale.
func TestTask229MoveAndResolveStrings(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := uiTextVars("gvim.prompt.move_remote", "Move %{what} on %{target} to %{dir}?", map[string]any{"what": "a.go", "target": "host", "dir": "src"}); got != "Move a.go on host to src?" {
		t.Errorf("move confirm = %q", got)
	}
	if got := uiTextVars("gvim.prompt.move_entries", "%{count} entries", map[string]any{"count": 3}); got != "3 entries" {
		t.Errorf("move count = %q", got)
	}
	if got := uiText("gvim.prompt.move", "Move"); got != "Move" {
		t.Errorf("move button = %q", got)
	}
	if got := uiText("gvim.notice.move_cancelled", "Move cancelled"); got != "Move cancelled" {
		t.Errorf("move cancel notice = %q", got)
	}
	if got := uiText("gvim.notice.completion.resolve_unsupported", "completion: multi-cursor resolved acceptance is not available in this editor"); got != "completion: multi-cursor resolved acceptance is not available in this editor" {
		t.Errorf("resolve refusal = %q", got)
	}
	if got := uiText("gvim.notice.completion.offer_stale", "completion is stale: the document changed, so nothing was applied"); got != "completion is stale: the document changed, so nothing was applied" {
		t.Errorf("accept notice = %q", got)
	}
	// task-226 safety rollback: the trash keys are retired; the permanent-delete
	// sentences are the live ones with the strict full wording asserted in
	// i18n_task138_test.go, and the copy-metadata warning is retained.
	for _, dead := range []string{"gvim.remote.trash_delete_file", "gvim.remote.trash_delete_items", "gvim.remote.no_trash_suffix"} {
		if got := i18n.T("en", dead, nil); got != dead {
			t.Errorf("retired key %s still in the catalogue as %q", dead, got)
		}
	}
	for _, key := range []string{"gvim.prompt.move_remote", "gvim.prompt.move_entries", "gvim.prompt.move", "gvim.notice.move_cancelled", "gvim.notice.move_drag_remote_only", "gvim.notice.copy_metadata_warning", "gvim.remote.permanent_delete_file", "gvim.remote.permanent_delete_items", "gvim.notice.completion.resolve_unsupported", "gvim.notice.completion.resolve_version_unverifiable", "gvim.notice.completion.resolve_inactive", "gvim.notice.completion.resolve_version_lost", "gvim.notice.completion.resolve_stale", "gvim.notice.completion.resolve_cursors_changed", "gvim.notice.completion.accept_snippet_import_unsupported", "gvim.notice.completion.accept_unsupported", "gvim.notice.completion.offer_stale", "gvim.notice.completion.offer_overlap", "gvim.notice.completion.error"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
