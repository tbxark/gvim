package app

import (
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/i18n"
)

// TestSettingsLabelsUseTheCatalogue checks the extracted settings labels: English
// keeps the schema names the page has always shown, and Chinese uses Fresh's translated
// row labels for the rows mapped to settings.field.* while gvim-only rows and section
// titles fall back to English (never the bare key). Not parallel: it changes and
// restores the process locale.
func TestSettingsLabelsUseTheCatalogue(t *testing.T) {
	isolateConfig(t)
	got, err := buildCaptured(t, Options{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	port := settingsPort(t, got)

	en := port.Snapshot()
	autoClose := findItem(t, en, "editor.auto_close")
	if autoClose.Name != "auto_close" || autoClose.Section != "Editing" {
		t.Fatalf("English auto_close label = %q/%q, want the schema names", autoClose.Name, autoClose.Section)
	}
	if item := findItem(t, en, "theme"); item.Name != "theme" || item.Section != "Interface" {
		t.Fatalf("English theme label = %q/%q, want the schema names", item.Name, item.Section)
	}

	i18n.SetLocale("zh-CN")
	t.Cleanup(func() { i18n.SetLocale("en") })
	zh := port.Snapshot()
	if item := findItem(t, zh, "editor.auto_close"); item.Name != "自动闭合" {
		t.Errorf("Chinese auto_close label = %q, want the Fresh translation", item.Name)
	}
	if item := findItem(t, zh, "editor.whitespace_show"); item.Name != "显示空白字符" {
		t.Errorf("Chinese whitespace_show label = %q, want the Fresh translation", item.Name)
	}
	// A gvim-only row and a section title have no translation: the English fallback,
	// not the key.
	if item := findItem(t, zh, "theme"); item.Name != "theme" || item.Section != "Interface" {
		t.Errorf("Chinese unmapped label = %q/%q, want the English fallback", item.Name, item.Section)
	}
}

// TestEverySettingsLabelHasACatalogueKey checks the CI invariant: every scalar settings
// row and section has an English catalogue entry, so the label extraction cannot silently
// fall back because a new setting was added without its key.
func TestEverySettingsLabelHasACatalogueKey(t *testing.T) {
	t.Parallel()
	for _, setting := range config.Settings() {
		if got := i18n.T("en", settingLabelKey(setting), nil); got == settingLabelKey(setting) {
			t.Errorf("%s: row label key %q is not in the English catalogue", setting.Key(), settingLabelKey(setting))
		}
		if got := i18n.T("en", settingsSectionKey(setting.Section), nil); got == settingsSectionKey(setting.Section) {
			t.Errorf("%s: section label key %q is not in the English catalogue", setting.Section, settingsSectionKey(setting.Section))
		}
	}
}

// TestStructuredSettingsLabelsFallBackToEnglish checks the extracted sub-page labels: a
// gvim-only page/section/note key renders its English text in another locale (never the
// bare key) and keeps the English rendering identical.
func TestStructuredSettingsLabelsFallBackToEnglish(t *testing.T) {
	t.Cleanup(func() { i18n.SetLocale("en") })
	if got := settingsPage("language_servers", "Language Servers"); got != "Language Servers" {
		t.Fatalf("English page label = %q", got)
	}
	if got := settingsLanguageTitle("go"); got != "Languages · go" {
		t.Fatalf("English language title = %q", got)
	}
	i18n.SetLocale("zh-CN")
	if got := settingsPage("language_servers", "Language Servers"); got != "Language Servers" {
		t.Errorf("Chinese page label = %q, want the English fallback", got)
	}
	if got := settingsLanguageTitle("go"); got != "Languages · go" {
		t.Errorf("Chinese language title = %q, want the English fallback", got)
	}
	if got := settingsLanguagePart("Formatter"); got != "Formatter" {
		t.Errorf("Chinese language part = %q, want the English fallback", got)
	}
	if got := settingsNote("does_not_exist", "note"); got != "note" {
		t.Errorf("Chinese unknown note = %q, want the English fallback", got)
	}
}
