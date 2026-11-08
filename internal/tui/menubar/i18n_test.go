package menubar

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestMenuLabelsFollowTheLocale checks the extracted menu labels: German uses Fresh's
// translations for the bar titles and dropdown rows, while a gvim-rewritten label
// (Show Help, gvim-only key) and an unmapped row fall back to English, never the key.
// Not parallel: it changes and restores the process locale.
func TestMenuLabelsFollowTheLocale(t *testing.T) {
	en := menuBarLabel("File")
	if en != "File" {
		t.Fatalf("English bar label = %q, want File", en)
	}
	if rows := dropdownRows("File", func(string) bool { return true }, nil, nil); len(rows) == 0 || rows[0].Label != "New File" {
		t.Fatalf("English first File row = %+v, want New File", rows)
	}
	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := menuBarLabel("File"); got != "Datei" {
		t.Errorf("German bar label = %q, want Datei", got)
	}
	if got := menuBarLabel("Edit"); got != "Bearbeiten" {
		t.Errorf("German bar label = %q, want Bearbeiten", got)
	}
	rows := dropdownRows("File", func(string) bool { return true }, nil, nil)
	if len(rows) == 0 || rows[0].Label != "Neue Datei" {
		t.Errorf("German first File row = %+v, want Neue Datei", rows[0])
	}
	// Show Help is gvim's rewording of Fresh's manual row: a gvim-only key with no
	// German translation, so the English label is the explicit fallback.
	help := dropdownRows("Help", func(string) bool { return true }, nil, nil)
	var showHelp string
	for _, row := range help {
		if strings.Contains(row.Label, "Help") {
			showHelp = row.Label
		}
	}
	if showHelp != "Show Help" {
		t.Errorf("German extra help row = %q, want the English fallback", showHelp)
	}
}

// TestGeneratedMenuKeysExist checks the CI invariant: every key the menu generator emits
// is in the English catalogue (the English label is also the fallback), so a stale
// generated table or a missing en.json entry fails the build before menus silently fall
// back. Items without a key (the version label) are exempt.
func TestGeneratedMenuKeysExist(t *testing.T) {
	t.Parallel()
	check := func(label, key string) {
		if key == "" {
			return
		}
		if got := i18n.T("en", key, nil); got == key {
			t.Errorf("menu label %q: key %q is not in the English catalogue", label, key)
		}
	}
	for _, menu := range legacyMenus {
		check(menu.label, menu.key)
		var walk func([]legacyItem)
		walk = func(items []legacyItem) {
			for _, it := range items {
				check(it.label, it.key)
				walk(it.children)
			}
		}
		walk(menu.items)
	}
}
