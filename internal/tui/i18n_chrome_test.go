package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestChromeStringsUseTheCatalogue checks the extracted chrome strings: German uses
// Fresh's menu translations for the mapped context-menu rows, the gvim-only rows keep
// their English text (never the bare key), and the help context headings fall back to
// English. Not parallel: locale.
func TestChromeStringsUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(2), 80, 20)
	items := m.tabMenuItems(1)
	if len(items) == 0 || items[0].Label != "Close" {
		t.Fatalf("German tab menu first row = %+v, want the English fallback Close", items)
	}
	for _, it := range items {
		if it.Separator {
			continue
		}
		if it.Label == "" || (len(it.Label) > 6 && it.Label[:6] == "gvim.c") {
			t.Errorf("tab menu row %q looks like an unresolved key", it.Label)
		}
	}
	if got := uiText("gvim.context.cut", "Cut"); got != "Ausschneiden" {
		t.Errorf("mapped Cut = %q, want Fresh's translation", got)
	}
	if got := contextTitle("normal"); got != "Editor" {
		t.Errorf("help context title = %q, want the English fallback", got)
	}
}

// TestChromeKeyGuard checks the CI invariant: every catalogue key referenced by the
// extracted chrome helpers exists in the English catalogue (so a missing en entry is a
// test failure, not a silently bare key).
func TestChromeKeyGuard(t *testing.T) {
	t.Parallel()
	for _, key := range []string{
		"gvim.context.close", "gvim.context.close_others", "gvim.context.close_right", "gvim.context.close_left",
		"gvim.context.close_all", "gvim.context.copy_relative_path", "gvim.context.copy_full_path",
		"gvim.context.extract_workspace", "gvim.context.new_terminal", "gvim.context.new_file",
		"gvim.context.new_directory", "gvim.context.cut", "gvim.context.copy", "gvim.context.paste",
		"gvim.context.delete", "gvim.context.duplicate", "gvim.context.rename", "gvim.context.select_all",
		"gvim.browser.column_name", "gvim.browser.column_size", "gvim.browser.column_modified",
		"gvim.browser.title_open_file", "gvim.browser.title_switch_project", "gvim.browser.no_matches",
		"gvim.browser.hint_open", "gvim.browser.hint_choose", "gvim.browser.sort_by", "gvim.browser.show_hidden",
		"gvim.picker.no_items", "gvim.list.hint", "gvim.list.read_only_hint", "gvim.list.empty",
		"gvim.list.filter_label", "gvim.help.no_bindings", "gvim.help.context_editor",
		"gvim.help.context_everywhere", "gvim.help.context_file_explorer", "gvim.help.context_terminal",
		"gvim.key_hints.palette", "gvim.key_hints.palette_shortcut", "gvim.prompt.item",
		"gvim.prompt.language", "gvim.prompt.key", "gvim.prompt.value", "gvim.prompt.value_for",
	} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English catalogue entry for %s", key)
		}
	}
}
