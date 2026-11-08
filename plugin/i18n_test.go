package plugin_test

import (
	"strings"
	"testing"

	"github.com/tbxark/gvim/plugin"
	"github.com/tbxark/gvim/plugin/plugintest"
)

// i18nJSON is a plugin translation file: English plus one locale that only translates some
// keys, so the fallback chain is observable.
const i18nJSON = `{
  "en": {
    "status.ready": "Ready",
    "status.found": "Found %{count} items",
    "cmd.open": "Open File",
    "cmd.open_desc": "Open a file"
  },
  "bg": {
    "status.ready": "Готово",
    "cmd.open": "Отвори файл"
  }
}`

const i18nPlugin = `
local gvim = require 'gvim'
gvim.command.register{
  name = "report", title = "%cmd.open", description = "%cmd.open_desc",
  run = function()
    gvim.notify(gvim.i18n.t("status.ready"))
    gvim.notify(gvim.i18n.t("status.found", { count = 3 }))
    gvim.notify(gvim.i18n.t("nope.missing"))
    gvim.notify(gvim.i18n.locale())
    gvim.notify(tostring(gvim.i18n.translate == gvim.i18n.t))
  end,
}
`

func i18nHarness(t *testing.T, locale, i18n string) *plugintest.Harness {
	t.Helper()
	files := plugintest.Files{"p.lua": i18nPlugin}
	if i18n != "" {
		files["p.i18n.json"] = i18n
	}
	h := plugintest.New(t, files, plugintest.Options{Locale: locale})
	h.Start()
	if p, ok := h.Plugin("p"); !ok || p.Status != "loaded" {
		t.Fatalf("plugin status = %+v, log %v", p, h.LogText(""))
	}
	return h
}

func TestI18nEnglishFallback(t *testing.T) {
	h := i18nHarness(t, "", i18nJSON)
	cmd := h.Host.Commands["p"]
	if len(cmd) != 1 || cmd[0].Title != "Open File" || cmd[0].Description != "Open a file" {
		t.Fatalf("commands = %+v, want the translated title and description", cmd)
	}
	if err := h.Run("p.report", nil); err != nil {
		t.Fatal(err)
	}
	got := h.NoticeTexts()
	want := []string{"Ready", "Found 3 items", "nope.missing", "en", "true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("notices = %q, want %q", got, want)
	}
}

func TestI18nLocaleAndFallbackChain(t *testing.T) {
	h := i18nHarness(t, "bg", i18nJSON)
	if err := h.Run("p.report", nil); err != nil {
		t.Fatal(err)
	}
	got := h.NoticeTexts()
	// ready is Bulgarian; found has no Bulgarian entry, so English is used; missing
	// everywhere keeps the key; the locale is reported.
	want := []string{"Готово", "Found 3 items", "nope.missing", "bg", "true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("notices = %q, want %q", got, want)
	}
	// The command title falls back to English when the locale has no cmd.open entry.
	cmd := h.Host.Commands["p"]
	if len(cmd) != 1 || cmd[0].Title != "Отвори файл" {
		t.Fatalf("title = %+v, want the Bulgarian title", cmd)
	}
}

func TestI18nWithoutFile(t *testing.T) {
	h := i18nHarness(t, "", "")
	if err := h.Run("p.report", nil); err != nil {
		t.Fatal(err)
	}
	got := h.NoticeTexts()
	// No table: every key comes back as itself, including the %-prefixed command title.
	want := []string{"status.ready", "status.found", "nope.missing", "en", "true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("notices = %q, want %q", got, want)
	}
	cmd := h.Host.Commands["p"]
	if len(cmd) != 1 || cmd[0].Title != "cmd.open" {
		t.Fatalf("title = %+v, want the key", cmd)
	}
}

func TestI18nMalformedFileDoesNotStopThePlugin(t *testing.T) {
	h := i18nHarness(t, "", "{not json")
	if err := h.Run("p.report", nil); err != nil {
		t.Fatal(err)
	}
	if got := h.NoticeTexts(); len(got) == 0 || got[0] != "status.ready" {
		t.Fatalf("notices = %q, want the key fallback", got)
	}
	found := false
	for _, line := range h.LogText("p") {
		if strings.Contains(line, "i18n:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no i18n warning in the log: %q", h.LogText(""))
	}
}

// TestI18nStubsMatchRuntime keeps the locale API in the stubs and the docs.
func TestI18nStubsMatchRuntime(t *testing.T) {
	h := i18nHarness(t, "", i18nJSON)
	inv, ok := plugin.APIInventory(h.RT, "p")
	if !ok {
		t.Fatal("plugin did not load")
	}
	want := map[string]bool{"gvim.i18n.t": false, "gvim.i18n.translate": false, "gvim.i18n.locale": false}
	for _, f := range inv.Funcs {
		if _, ok := want[f]; ok {
			want[f] = true
		}
	}
	for f, found := range want {
		if !found {
			t.Errorf("the live state has no %s", f)
		}
	}
}

func TestSetLocaleSwitchesTheRuntimeLocale(t *testing.T) {
	h := i18nHarness(t, "en", i18nJSON)
	h.RT.SetLocale("bg")
	if err := h.Run("p.report", nil); err != nil {
		t.Fatal(err)
	}
	got := h.NoticeTexts()
	want := []string{"Готово", "Found 3 items", "nope.missing", "bg", "true"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("notices after SetLocale(bg) = %q, want %q", got, want)
	}
	// Back to English: the same keys resolve through the English fallback.
	h.RT.SetLocale("en")
	if err := h.Run("p.report", nil); err != nil {
		t.Fatal(err)
	}
	got = h.NoticeTexts()[len(got):]
	want = []string{"Ready", "Found 3 items", "nope.missing", "en", "true"}
	if got, want := got, want; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("notices after SetLocale(en) = %q, want %q", got, want)
	}
}

const i18nJSONDe = `{
  "en": {"status.ready": "Ready", "cmd.open": "Open File"},
  "de": {"status.ready": "Bereit", "cmd.open": "Datei öffnen"}
}`

const i18nIndependentPlugin = `
local gvim = require 'gvim'
gvim.command.register{
  name = "report",
  run = function()
    gvim.notify(gvim.i18n.t("clipboard.copied"))
  end,
}
`

// TestI18nPluginUsesItsOwnFile checks that a plugin translates from its own
// <name>.i18n.json for the editor locale, independently of the embedded editor
// catalogues: the editor's de clipboard translation is not injected into the plugin,
// so an editor-only key reads as the key until the plugin translates it itself.
func TestI18nPluginUsesItsOwnFile(t *testing.T) {
	files := plugintest.Files{"p.lua": i18nIndependentPlugin, "p.i18n.json": i18nJSONDe}
	h := plugintest.New(t, files, plugintest.Options{Locale: "de"})
	h.Start()
	if p, ok := h.Plugin("p"); !ok || p.Status != "loaded" {
		t.Fatalf("plugin status = %+v, log %v", p, h.LogText(""))
	}
	if err := h.Run("p.report", nil); err != nil {
		t.Fatal(err)
	}
	// The plugin file has no clipboard.copied entry and the editor catalogue is not
	// consulted, so the key itself is returned.
	if got := h.NoticeTexts(); len(got) != 1 || got[0] != "clipboard.copied" {
		t.Fatalf("notices = %q, want the untranslated key", got)
	}
}
