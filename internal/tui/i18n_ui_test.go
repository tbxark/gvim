package tui

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestNoticeAndPromptStringsUseTheCatalogue checks display-time behavior with Chinese
// active: the Fresh-mapped explorer notice is translated, a gvim-only notice falls back
// to English, and the prompt label fallback is the English text, never the key. Not
// parallel: locale.
func TestNoticeAndPromptStringsUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	m := newTestModel(t, newFakeEditor("hello"), newFakeTree(3), 80, 20)
	m.explorerOpen = true
	runAction(t, m, "toggle_file_explorer_side")
	if m.notice != "已将文件资源管理器移到右侧" {
		t.Errorf("Chinese explorer notice = %q, want Fresh's translated toggle", m.notice)
	}
	runAction(t, m, "toggle_file_explorer_side")
	if m.notice != "已将文件资源管理器移到左侧" {
		t.Errorf("Chinese explorer notice = %q, want Fresh's translated toggle", m.notice)
	}
	// A gvim-only compare notice stays English (explicit fallback, not the bare key).
	runAction(t, m, "compare_next_difference")
	if m.notice != "Compare: no comparison is open here" {
		t.Errorf("Chinese compare notice = %q, want the English fallback", m.notice)
	}
	// The go-to-line prompt label is gvim-only: English fallback.
	runAction(t, m, "goto_line")
	if !frameHas(m, "Go to line:") {
		t.Errorf("the prompt does not show the English fallback label:\n%s", strings.Join(frameText(m), "\n"))
	}
}
