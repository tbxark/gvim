package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestTask138StringsUseTheCatalogue checks the round's extracted strings: German keeps the
// gvim-only remote/palette/prompt texts as English (never the bare key) and Fresh-mapped
// LSP picker strings use Fresh's translations.
func TestTask138StringsUseTheCatalogue(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := newEntryLabel(false, "host"); got != "New file on host:" {
		t.Errorf("remote new-file label = %q, want the English fallback", got)
	}
	if got := newEntryLabel(true, ""); got != "New folder:" {
		t.Errorf("remote new-folder label = %q", got)
	}
	if got := deleteQuestion([]string{"/w/a.go"}, "host"); got != "Permanently delete a.go on host? Remote deletes have no trash." {
		t.Errorf("remote delete question = %q, want the full permanent-delete sentence", got)
	}
	if got := deleteQuestion([]string{"/w/a.go", "/w/b.go"}, "host"); got != "Permanently delete 2 items on host? Remote deletes have no trash." {
		t.Errorf("remote multi delete question = %q, want the full permanent-delete sentence", got)
	}
	if got := uiText("gvim.prompt.name_empty", "name is empty"); got != "name is empty" {
		t.Errorf("validation = %q, want the English fallback", got)
	}
	if got := uiText("gvim.lsp.no_definition", "No definition found"); got != "Keine Definition gefunden" {
		t.Errorf("German LSP picker = %q, want Fresh's translation", got)
	}
}
