package dialog

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui/style"
)

// TestDialogLabelsUseTheCatalogue checks the extracted dialog labels: English is byte
// identical to the old literals, Chinese uses Fresh's dialog translations for the mapped
// buttons/title, and a gvim-only label falls back to English, never the bare key. Not
// parallel: it changes and restores the process locale.
func TestDialogLabelsUseTheCatalogue(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale("en") })
	ov := style.Overlay{}
	en := NewUnsavedChanges(nil, "a.go", nil, ov)
	if en.title != "Unsaved Changes" {
		t.Fatalf("English title = %q", en.title)
	}
	if got := buttonLabels(en.buttons); got != "Save,Discard,Cancel" {
		t.Fatalf("English buttons = %q", got)
	}
	i18n.SetLocale("zh-CN")
	zh := NewUnsavedChanges(nil, "a.go", nil, ov)
	if zh.title != "未保存的更改" {
		t.Errorf("Chinese title = %q, want Fresh's dialog title", zh.title)
	}
	if got := buttonLabels(zh.buttons); got != "保存,丢弃,取消" {
		t.Errorf("Chinese buttons = %q, want Fresh's dialog buttons", got)
	}
	// Error/OK are gvim-only: the explicit English fallback.
	errDlg := NewErrorDialog(nil, "boom", nil, ov)
	if errDlg.title != "Error" || buttonLabels(errDlg.buttons) != "OK" {
		t.Errorf("Chinese error dialog = %q/%q, want the English fallback", errDlg.title, buttonLabels(errDlg.buttons))
	}
	// The dynamic labels are catalogue text with placeholders; the English rendering is
	// what the dialog always showed.
	if got := dialogTextVars("gvim.dialog.save_changes", "Save changes to %{name}?", map[string]any{"name": "a.go"}); got != "Save changes to a.go?" {
		t.Errorf("save-changes text = %q", got)
	}
	if got := dialogTextVars("gvim.dialog.trash_confirm", "Move \"%{name}\" to Trash?", map[string]any{"name": "a.go"}); got != `Move "a.go" to Trash?` {
		t.Errorf("trash text = %q", got)
	}
}

// buttonLabels joins the labels for an assertion.
func buttonLabels(buttons []button) string {
	out := ""
	for i, b := range buttons {
		if i > 0 {
			out += ","
		}
		out += b.label
	}
	return out
}
