package app

import (
	"testing"

	"github.com/tbxark/gvim/i18n"
)

// TestSettingsLabelsUseTheCatalogue checks the extracted settings labels: English
// keeps the schema names the page has always shown, and German uses Fresh's translated
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

	i18n.SetLocale("de")
	t.Cleanup(func() { i18n.SetLocale("en") })
	de := port.Snapshot()
	if item := findItem(t, de, "editor.auto_close"); item.Name != "Automatisches Schließen" {
		t.Errorf("German auto_close label = %q, want the Fresh translation", item.Name)
	}
	if item := findItem(t, de, "editor.whitespace_show"); item.Name != "Leerzeichen anzeigen" {
		t.Errorf("German whitespace_show label = %q, want the Fresh translation", item.Name)
	}
	// A gvim-only row and a section title have no translation: the English fallback,
	// not the key.
	if item := findItem(t, de, "theme"); item.Name != "theme" || item.Section != "Interface" {
		t.Errorf("German unmapped label = %q/%q, want the English fallback", item.Name, item.Section)
	}
}
