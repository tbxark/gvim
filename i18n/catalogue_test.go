package i18n

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// embeddedLocales returns the locale files in the embedded filesystem, decoded.
func embeddedLocales(t *testing.T) map[string]map[string]string {
	t.Helper()
	entries, err := localeFiles.ReadDir("locales")
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]map[string]string, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		code := strings.TrimSuffix(name, ".json")
		data, err := localeFiles.ReadFile("locales/" + name)
		if err != nil {
			t.Fatal(err)
		}
		var table map[string]string
		if err := json.Unmarshal(data, &table); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out[code] = table
	}
	return out
}

// TestEmbeddedLocaleFiles checks the catalogue layout: exactly the shipped locales,
// English first as the source of truth, and a bounded embedded size.
func TestEmbeddedLocaleFiles(t *testing.T) {
	t.Parallel()
	want := map[string]bool{"en": true}
	for _, lang := range Locales() {
		want[lang.Code] = true
	}
	if got := len(want); got != 15 {
		t.Fatalf("Locales() has %d languages, want the 15 Fresh locales", got)
	}
	files := embeddedLocales(t)
	if len(files) != len(want) {
		t.Fatalf("embedded locales = %d files, want %d", len(files), len(want))
	}
	for code := range files {
		if !want[code] {
			t.Errorf("embedded locale %q is not one of Locales()", code)
		}
	}
	for code := range want {
		if files[code] == nil {
			t.Errorf("locale %q has no embedded file", code)
		}
	}
	// The size stays bounded: the catalogue is the small extracted inventory, not
	// Fresh's full 1.9k-key file. The cap leaves room for future slices but keeps a
	// careless full-import from silently bloating the binary.
	var total int
	for code, table := range files {
		for key, value := range table {
			total += len(key) + len(value)
			if len(value) > maxValue {
				t.Errorf("%s: %s is %d bytes, over maxValue", code, key, len(value))
			}
		}
	}
	if total > 64*1024 {
		t.Errorf("embedded locale text = %d bytes, want under the 64 KiB cap", total)
	}
}

// TestEmbeddedCataloguesValidate runs the CI validator over every embedded file: key
// shape, placeholders, plural categories and the English-key relation.
func TestEmbeddedCataloguesValidate(t *testing.T) {
	t.Parallel()
	entries, err := localeFiles.ReadDir("locales")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		code := strings.TrimSuffix(entry.Name(), ".json")
		data, err := localeFiles.ReadFile("locales/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if errs := ValidateCatalogueJSON(code, data); len(errs) > 0 {
			for _, err := range errs {
				t.Errorf("%s", err)
			}
		}
	}
}

// TestEnglishIsTheSourceOfTruth checks the catalogue boundary: the explicit
// gvim-key -> Fresh-key map covers part of the English inventory, the rest is
// gvim-only (English text, no Fresh translation), and every English value is
// well-formed.
func TestEnglishIsTheSourceOfTruth(t *testing.T) {
	t.Parallel()
	ensureEnglish()
	en := catalogue(DefaultLocale)
	mapping := FreshMap()
	if len(mapping) == 0 {
		t.Fatal("fresh_map.json is empty")
	}
	for gvimKey := range mapping {
		if _, ok := en[gvimKey]; !ok {
			t.Errorf("mapped key %q is not in the English catalogue", gvimKey)
		}
	}
	var gvimOnly []string
	for key := range en {
		if _, ok := mapping[key]; ok {
			continue
		}
		gvimOnly = append(gvimOnly, key)
	}
	if len(gvimOnly)+len(mapping) != len(en) {
		t.Fatalf("mapping (%d) and gvim-only (%d) do not cover the %d English keys", len(mapping), len(gvimOnly), len(en))
	}
	for _, key := range gvimOnly {
		if !strings.HasPrefix(key, "gvim.") {
			t.Errorf("unmapped key %q is not a gvim-only key", key)
		}
	}
	for key, value := range en {
		if value == "" {
			t.Errorf("en: %s: empty value", key)
		}
		if _, err := placeholders(value); err != nil {
			t.Errorf("en: %s: %v", key, err)
		}
	}
}

// TestLocaleCoverage checks and reports the per-locale coverage from the embedded
// files themselves (not the runtime registry, which Register can replace): every
// mapped key is translated in every shipped locale (100% of the mapped subset), and
// the gvim-only keys are the documented untranslated boundary.
func TestLocaleCoverage(t *testing.T) {
	t.Parallel()
	files := embeddedLocales(t)
	en := files[DefaultLocale]
	mapping := FreshMap()
	for _, lang := range Locales() {
		if lang.Code == DefaultLocale {
			continue
		}
		table := files[lang.Code]
		if table == nil {
			t.Errorf("%s: no embedded file", lang.Code)
			continue
		}
		translated, translatedMapped := 0, 0
		for key := range en {
			if _, ok := table[key]; ok {
				translated++
			}
		}
		for key := range mapping {
			if _, ok := table[key]; ok {
				translatedMapped++
			} else {
				t.Errorf("%s: mapped key %q is missing (run tools/i18n_import.py)", lang.Code, key)
			}
		}
		t.Logf("%s: %d/%d English keys translated (%d/%d mapped; %d gvim-only keys fall back to English)",
			lang.Code, translated, len(en), translatedMapped, len(mapping), len(en)-len(mapping))
	}
}

// TestCoverageFor checks the public coverage report against the embedded German file.
// It is not parallel: it reads the runtime catalogue table, which tests may replace.
func TestCoverageFor(t *testing.T) {
	cov := CoverageFor("de")
	if cov.Locale != "de" || cov.Total == 0 || cov.Translated == 0 || cov.Translated < len(FreshMap()) {
		t.Fatalf("CoverageFor(de) = %+v, want the mapped German keys", cov)
	}
	if len(cov.Missing) != cov.Total-cov.Translated {
		t.Errorf("missing = %d, want %d", len(cov.Missing), cov.Total-cov.Translated)
	}
}

// TestValidateCatalogueRejects checks the validator's error paths: unknown keys,
// placeholder mismatches and malformed placeholders, empty/oversized values and the
// locale plural categories.
func TestValidateCatalogueRejects(t *testing.T) {
	// The plural-category cases add a base to the English table, so this test is not
	// parallel and restores the English catalogue afterwards.
	ensureEnglish()
	en := catalogue(DefaultLocale)
	const pluralBase = "gvim.test.plurality"
	forms := []string{".zero", ".one", ".two", ".few", ".many", ".other"}
	for _, form := range forms {
		en[pluralBase+form] = pluralBase + form
	}
	t.Cleanup(func() {
		for _, form := range forms {
			delete(en, pluralBase+form)
		}
	})
	tests := []struct {
		name   string
		locale string
		table  map[string]string
		want   string
	}{
		{"unknown key", "de", map[string]string{"gvim.not.in.english": "x"}, "not in the English catalogue"},
		{"placeholder mismatch", "de", map[string]string{"clipboard.copied_path": "Pfad: %{datei}"}, "do not match English"},
		{"malformed placeholder", "de", map[string]string{"clipboard.copied_path": "Pfad: %{path"}, "unterminated placeholder"},
		{"bad placeholder name", "de", map[string]string{"gvim.terminal.no_file_at": "x %{a-b}"}, "not allowed"},
		{"empty value", "de", map[string]string{"clipboard.copied": ""}, "empty value"},
		{"oversized value", "de", map[string]string{"clipboard.copied": strings.Repeat("x", maxValue+1)}, "over the"},
		{"plural category the locale does not use", "ja", map[string]string{pluralBase + ".one": "x"}, "plural category"},
		{"foreign plural category", "ru", map[string]string{pluralBase + ".zero": "x"}, "plural category"},
		{"unknown locale", "xx", map[string]string{"clipboard.copied": "Kopiert"}, "not one of the shipped locales"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := ValidateCatalogue(tt.locale, tt.table)
			if len(errs) == 0 {
				t.Fatalf("ValidateCatalogue(%s, %v) accepted the table", tt.locale, tt.table)
			}
			joined := ""
			for _, err := range errs {
				joined += err.Error() + "\n"
			}
			if !strings.Contains(joined, tt.want) {
				t.Fatalf("errors = %v, want one containing %q", errs, tt.want)
			}
		})
	}
	// The valid forms are accepted for the locales that use them.
	for _, tt := range []struct {
		locale string
		table  map[string]string
	}{
		{"en", map[string]string{pluralBase + ".one": "one", pluralBase + ".other": "other"}},
		{"ru", map[string]string{pluralBase + ".few": "few", pluralBase + ".many": "many"}},
		{"ja", map[string]string{pluralBase + ".other": "other"}},
	} {
		if errs := ValidateCatalogue(tt.locale, tt.table); len(errs) > 0 {
			t.Errorf("ValidateCatalogue(%s) rejected a valid table: %v", tt.locale, errs)
		}
	}
}

// TestValidateCatalogueJSONRejectsDuplicateKeys checks the raw-file validation: a
// duplicate key that json.Unmarshal would silently collapse is an error.
func TestValidateCatalogueJSONRejectsDuplicateKeys(t *testing.T) {
	t.Parallel()
	data := []byte(`{"clipboard.copied": "A", "clipboard.copied": "B"}`)
	errs := ValidateCatalogueJSON("de", data)
	if len(errs) == 0 || !strings.Contains(errs[0].Error(), "duplicate key") {
		t.Fatalf("duplicate keys = %v, want a duplicate-key error", errs)
	}
	if errs := ValidateCatalogueJSON("de", []byte(`["not", "an", "object"]`)); len(errs) == 0 {
		t.Fatal("a JSON array was accepted")
	}
	if errs := ValidateCatalogueJSON("de", []byte(`{"clipboard.copied": 3}`)); len(errs) == 0 {
		t.Fatal("a non-string value was accepted")
	}
}

// TestEmbeddedTranslationsAndFallback checks the embedded catalogues through the
// public API: German translates the mapped keys, a gvim-only key and an unknown key
// fall back to English, and a regional tag resolves to its language.
func TestEmbeddedTranslationsAndFallback(t *testing.T) {
	SetLocale("en")
	t.Cleanup(func() { SetLocale("en") })
	if got := T("de", "clipboard.copied", nil); got != "Kopiert" {
		t.Errorf("T(de, clipboard.copied) = %q, want the embedded German", got)
	}
	if got := T("de", "clipboard.copied_path", map[string]any{"path": "/ws/a.go"}); got != "Pfad kopiert: /ws/a.go" {
		t.Errorf("interpolated German = %q", got)
	}
	// gvim-only: no translation, English text.
	if got := T("de", "gvim.terminal.capture_on", nil); !strings.HasPrefix(got, "Keyboard capture ON") {
		t.Errorf("gvim-only German = %q, want the English fallback", got)
	}
	// Unknown locale and unknown key fall back.
	if got := T("xx", "clipboard.copied", nil); got != "Copied" {
		t.Errorf("unknown locale = %q, want English", got)
	}
	if got := T("de", "gvim.never.defined", nil); got != "gvim.never.defined" {
		t.Errorf("unknown key = %q, want the key", got)
	}
	// A regional tag finds the language file through the chain.
	if got := T("de-AT", "clipboard.copied", nil); got != "Kopiert" {
		t.Errorf("T(de-AT) = %q, want the German fallback", got)
	}
	// The active locale drives the empty-locale form.
	SetLocale("de")
	if got := T("", "clipboard.pasted", nil); got != "Eingefügt" {
		t.Errorf("active de pasted = %q", got)
	}
}

// TestPluralCategoriesPerLocale checks the plural-category validation table stays in
// step with the runtime rules.
func TestPluralCategoriesPerLocale(t *testing.T) {
	t.Parallel()
	tests := []struct {
		locale string
		want   []string
	}{
		{"en", []string{"one", "other"}},
		{"de", []string{"one", "other"}},
		{"fr", []string{"one", "other"}},
		{"cs", []string{"few", "one", "other"}},
		{"ru", []string{"few", "many", "one", "other"}},
		{"ja", []string{"other"}},
	}
	for _, tt := range tests {
		allowed := allowedCategories(tt.locale)
		got := make([]string, 0, len(allowed))
		for cat := range allowed {
			got = append(got, cat)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(tt.want, ",") {
			t.Errorf("allowedCategories(%s) = %v, want %v", tt.locale, got, tt.want)
		}
	}
}
