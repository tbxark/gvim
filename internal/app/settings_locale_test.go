package app

import (
	"path/filepath"
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
	cfg.Locale = "zh-CN"
	s := newSettingsService("", Options{}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil,
		settingsRuntime{localeApply: hook})
	s.Apply(tui.SettingsChange{})
	if got := i18n.Locale(); got != "zh-CN" {
		t.Fatalf("i18n locale = %q, want zh-CN", got)
	}
	if len(applied) != 1 || applied[0] != "zh-CN" {
		t.Fatalf("plugin runtime locale hooks = %v, want zh-CN", applied)
	}
	if it := localeItem(t, s.Snapshot()); it.Value != "zh-CN" || it.Kind != "enum" {
		t.Fatalf("snapshot locale row = %+v", it)
	}

	// "auto" resolves from the environment at apply time.
	t.Setenv("LANG", "zh_CN.UTF-8")
	cfg.Locale = ""
	s = newSettingsService("", Options{}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil, settingsRuntime{})
	s.Apply(tui.SettingsChange{})
	if got := i18n.Locale(); got != "zh-CN" {
		t.Fatalf("i18n locale from LANG = %q, want zh-CN", got)
	}
	if it := localeItem(t, s.Snapshot()); it.Value != "auto" {
		t.Fatalf("snapshot locale row after auto = %+v", it)
	}
}

// TestLocalePickerPersistsThroughSettings checks the Select Locale command's persistence
// path: the choice goes through the Settings port so the config, the i18n package and
// the plugin runtime hook all move together.
func TestLocalePickerPersistsThroughSettings(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	t.Cleanup(func() { i18n.SetLocale("en") })

	var applied []string
	cfg := config.Defaults()
	s := newSettingsService("", Options{Config: filepath.Join(t.TempDir(), "config.json")}, editor.New(editor.Options{}), func() bool { return true }, cfg, nil,
		settingsRuntime{localeApply: func(locale string) { applied = append(applied, locale) }})
	change, err := s.Save([]tui.SettingEdit{{Key: "locale", Value: "zh-CN"}}, tui.SettingsLayerUser)
	if err != nil {
		t.Fatal(err)
	}
	s.Apply(change)
	if got := localeItem(t, change.Snapshot); got.Value != "zh-CN" {
		t.Fatalf("saved locale row = %+v, want zh-CN", got)
	}
	if got := i18n.Locale(); got != "zh-CN" {
		t.Fatalf("i18n locale = %q, want zh-CN", got)
	}
	if len(applied) != 1 || applied[0] != "zh-CN" {
		t.Fatalf("plugin runtime locale hooks = %v, want zh-CN", applied)
	}
}
