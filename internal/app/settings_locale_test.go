package app

import (
	"testing"

	"github.com/tbxark/gvim/config"
	"github.com/tbxark/gvim/editor"
	"github.com/tbxark/gvim/i18n"
	"github.com/tbxark/gvim/internal/tui"
)

// localeItem returns the locale row of a snapshot.
func localeItem(t *testing.T, snap tui.SettingsSnapshot) tui.SettingsItem {
	t.Helper()
	for _, sec := range snap.Sections {
		for _, it := range sec.Items {
			if it.Key == "locale" {
				return it
			}
		}
	}
	t.Fatal("the locale setting is not in the snapshot")
	return tui.SettingsItem{}
}

// TestSettingsLocaleAppliesLive checks the language row's live apply: the resolved locale
// reaches the i18n package and the plugin runtime hook, and "auto" reads the environment.
func TestSettingsLocaleAppliesLive(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	t.Cleanup(func() { i18n.SetLocale("en") })

	var applied []string
	hook := func(locale string) { applied = append(applied, locale) }
	cfg := config.Defaults()
	cfg.Locale = "pt-BR"
	s := newSettingsService("", Options{}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil,
		settingsRuntime{localeApply: hook})
	s.Apply(tui.SettingsChange{})
	if got := i18n.Locale(); got != "pt-BR" {
		t.Fatalf("i18n locale = %q, want pt-BR", got)
	}
	if len(applied) != 1 || applied[0] != "pt-BR" {
		t.Fatalf("plugin runtime locale hooks = %v, want pt-BR", applied)
	}
	if it := localeItem(t, s.Snapshot()); it.Value != "pt-BR" || it.Kind != "enum" {
		t.Fatalf("snapshot locale row = %+v", it)
	}

	// "auto" resolves from the environment at apply time.
	t.Setenv("LANG", "de_DE.UTF-8")
	cfg.Locale = ""
	s = newSettingsService("", Options{}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil, settingsRuntime{})
	s.Apply(tui.SettingsChange{})
	if got := i18n.Locale(); got != "de" {
		t.Fatalf("i18n locale from LANG = %q, want de", got)
	}
	if it := localeItem(t, s.Snapshot()); it.Value != "auto" {
		t.Fatalf("snapshot locale row after auto = %+v", it)
	}
}
