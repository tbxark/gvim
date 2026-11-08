package commands

import (
	"strings"
	"testing"
)

// TestCommandEntriesCarryCatalogueKeys checks the generated/hand-written split behind
// the palette title extraction: generated rows carry Fresh's cmd.* keys, the gvim-only
// terminal rows carry gvim.command.* keys (no Fresh translation), and the renamed rows
// use their gvim-specific key so a Fresh translation cannot name Fresh's file. The
// translation itself is the UI layer's job (internal/tui/command_i18n.go); commands
// stays i18n-free.
func TestCommandEntriesCarryCatalogueKeys(t *testing.T) {
	entries := Commands(nil)
	find := func(action string) Entry {
		t.Helper()
		for _, e := range entries {
			if e.Action == action {
				return e
			}
		}
		t.Fatalf("no command %q", action)
		return Entry{}
	}
	if got := find("close_tab").I18nKey; got != "cmd.close_tab" {
		t.Errorf("close_tab key = %q, want the generated Fresh key", got)
	}
	if got := find("terminal_tab_new").I18nKey; got != "gvim.command.terminal_tab_new" {
		t.Errorf("terminal_tab_new key = %q, want the gvim-only key", got)
	}
	if got := find("init_edit").I18nKey; got != "gvim.command.init_edit" {
		t.Errorf("init_edit key = %q, want the gvim rename key", got)
	}
	for _, e := range entries {
		if e.I18nKey != "" && !strings.HasPrefix(e.I18nKey, "cmd.") && !strings.HasPrefix(e.I18nKey, "gvim.command.") {
			t.Errorf("%s: unexpected i18n key %q", e.Action, e.I18nKey)
		}
	}
}
