package tui

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestTask223PasteStrings checks the extracted batch-paste and remote-finder strings:
// German shows the English fallback (gvim-only keys) and the counts/suffix compose
// byte-identically. Not parallel: locale.
func TestTask223PasteStrings(t *testing.T) {
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := pasteStopNotice("Paste skipped", 3); got != "Paste skipped; 3 item(s) of this paste were already pasted" {
		t.Errorf("paste stop notice = %q", got)
	}
	if got := pasteStopNotice("Paste cancelled", 0); got != "Paste cancelled" {
		t.Errorf("paste cancel notice = %q", got)
	}
	if got := uiTextVars("gvim.prompt.paste_as_on_target", "Paste as on %{target}:", map[string]any{"target": "host"}); got != "Paste as on host:" {
		t.Errorf("paste-as label = %q", got)
	}
	for _, key := range []string{"gvim.notice.remote_git_unavailable", "gvim.notice.remote_git_list_failed", "gvim.notice.paste_stopped", "gvim.notice.paste_skipped", "gvim.notice.paste_cancelled", "gvim.notice.nothing_to_paste", "gvim.notice.paste_cut_cancelled", "gvim.prompt.paste_as", "gvim.prompt.paste_as_on_target", "gvim.prompt.paste_many", "gvim.paste.overwrite", "gvim.paste.overwrite_all", "gvim.paste.skip", "gvim.paste.skip_all", "gvim.paste.cancel", "gvim.notice.remote_destination_exists"} {
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("missing English entry for %s", key)
		}
	}
}
