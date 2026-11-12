package i18n

import (
	"strings"
	"testing"
)

func TestLocalesOffersEnglishAndSimplifiedChinese(t *testing.T) {
	t.Parallel()
	got := Locales()
	if len(got) != 2 {
		t.Fatalf("Locales() = %d entries, want en and zh-CN", len(got))
	}
	seen := map[string]bool{}
	for _, l := range got {
		if l.Code == "" || l.English == "" || l.Native == "" {
			t.Fatalf("incomplete locale: %+v", l)
		}
		if seen[l.Code] {
			t.Fatalf("duplicate locale %q", l.Code)
		}
		seen[l.Code] = true
	}
	if !seen["zh-CN"] || !seen["en"] {
		t.Fatalf("locales = %v, want en and zh-CN", got)
	}
	if l, ok := Find("en"); !ok || l.English != "English" {
		t.Fatalf("Find(en) = %+v, %v", l, ok)
	}
	if l, ok := Find("zh-CN"); !ok || l.Native != "简体中文" {
		t.Fatalf("Find(zh-CN) = %+v, %v", l, ok)
	}
	if _, ok := Find("de"); ok {
		t.Fatal("Find accepted the removed German locale")
	}
	if _, ok := Find("xx"); ok {
		t.Fatal("Find accepted an unknown locale")
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"C", ""},
		{"POSIX", ""},
		{"en", "en"},
		{"en_US.UTF-8", "en"},
		{"zh_CN.UTF-8", "zh-CN"},
		{"zh_CN", "zh-CN"},
		{"zh_SG.UTF-8", "zh-CN"},
		{"zh-Hans", "zh-CN"},
		{"zh_TW.UTF-8", "zh-TW"},
		{"zh-Hant", "zh-Hant"},
		{"zh-HK", "zh-HK"},
		{"de_DE.UTF-8", "de"},
		{"zh-CN", "zh-CN"},
		{"xx-YY", "xx"},
	}
	for _, tt := range tests {
		if got := Normalize(tt.in); got != tt.want {
			t.Errorf("Normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestResolve(t *testing.T) {
	t.Parallel()
	env := func(values map[string]string) func(string) string {
		return func(name string) string { return values[name] }
	}
	tests := []struct {
		name   string
		config string
		env    map[string]string
		want   string
	}{
		{"no config and no environment", "", nil, "en"},
		{"config locale wins", "zh-CN", map[string]string{"LC_ALL": "fr_FR.UTF-8"}, "zh-CN"},
		{"config auto reads the environment", "auto", map[string]string{"LANG": "zh_CN.UTF-8"}, "zh-CN"},
		{"LC_ALL wins over LANG", "", map[string]string{"LC_ALL": "zh_CN.UTF-8", "LANG": "de_DE.UTF-8"}, "zh-CN"},
		{"LC_MESSAGES comes second", "", map[string]string{"LC_MESSAGES": "en_US.UTF-8", "LANG": "de_DE.UTF-8"}, "en"},
		{"C and POSIX fall back to English", "", map[string]string{"LANG": "C"}, "en"},
		{"an unknown config locale is kept for the fallback chain", "xx", nil, "xx"},
		{"a config tag is normalised", "zh_cn", nil, "zh-CN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Resolve(tt.config, env(tt.env)); got != tt.want {
				t.Fatalf("Resolve(%q) = %q, want %q", tt.config, got, tt.want)
			}
		})
	}
}

func TestTTranslatesAndFallsBack(t *testing.T) {
	SetLocale("en")
	t.Cleanup(func() { SetLocale("en") })
	if got := T("en", "clipboard.copied", nil); got != "Copied" {
		t.Fatalf("T(en, clipboard.copied) = %q", got)
	}
	// An unknown locale falls back to English.
	if got := T("xx", "clipboard.copied", nil); got != "Copied" {
		t.Fatalf("T(xx, clipboard.copied) = %q, want the English text", got)
	}
	// An unknown key reads as the key itself.
	if got := T("en", "gvim.never.defined", nil); got != "gvim.never.defined" {
		t.Fatalf("unknown key = %q", got)
	}
	// Interpolation, and a missing variable stays visible.
	if got := T("en", "clipboard.copied_path", map[string]any{"path": "/ws/a.go"}); got != "Copied path: /ws/a.go" {
		t.Fatalf("interpolated = %q", got)
	}
	if got := T("en", "clipboard.copied_path", nil); got != "Copied path: %{path}" {
		t.Fatalf("missing var = %q, want the placeholder kept", got)
	}
	// The active locale is used for the empty locale; Chinese is now embedded from
	// Fresh's translations, so the extracted key is translated.
	SetLocale("zh-CN")
	if got := T("", "clipboard.copied", nil); got != "已复制" {
		t.Fatalf("T with the active locale = %q, want the embedded Chinese text", got)
	}
	// A gvim-only key has no translation: it falls back to English.
	if got := T("", "gvim.terminal.capture_on", nil); got != "Keyboard capture ON - all keys go to terminal (F9 to toggle)" {
		t.Fatalf("gvim-only key = %q, want the English fallback", got)
	}
}

func TestPluralCategories(t *testing.T) {
	t.Parallel()
	tests := []struct {
		locale string
		n      int
		want   string
	}{
		{"en", 0, "other"}, {"en", 1, "one"}, {"en", 2, "other"},
		{"zh-CN", 0, "other"}, {"zh-CN", 1, "other"}, {"zh-CN", 5, "other"},
		{"ja", 1, "other"}, {"ko", 1, "other"}, {"th", 2, "other"}, {"vi", 1, "other"},
		{"cs", 1, "one"}, {"cs", 3, "few"}, {"cs", 5, "other"},
		{"ru", 1, "one"}, {"ru", 21, "one"}, {"ru", 2, "few"}, {"ru", 14, "many"}, {"ru", 5, "many"},
	}
	for _, tt := range tests {
		if got := PluralCategory(tt.locale, tt.n); got != tt.want {
			t.Errorf("PluralCategory(%q, %d) = %q, want %q", tt.locale, tt.n, got, tt.want)
		}
	}
}

func TestTNPluralForms(t *testing.T) {
	Register("ru", map[string]string{
		"test.files.one":  "%{count} файл",
		"test.files.few":  "%{count} файла",
		"test.files.many": "%{count} файлов",
		"test.only.other": "любое %{count}",
	})
	t.Cleanup(func() { Register("ru", nil) })
	if got, want := TN("ru", "test.files", 1, nil), "1 файл"; got != want {
		t.Fatalf("TN(ru, 1) = %q, want %q", got, want)
	}
	if got, want := TN("ru", "test.files", 2, nil), "2 файла"; got != want {
		t.Fatalf("TN(ru, 2) = %q, want %q", got, want)
	}
	if got, want := TN("ru", "test.files", 5, nil), "5 файлов"; got != want {
		t.Fatalf("TN(ru, 5) = %q, want %q", got, want)
	}
	// A missing category falls back to the other form.
	if got, want := TN("ru", "test.only", 1, nil), "любое 1"; got != want {
		t.Fatalf("TN(ru, test.only, 1) = %q, want %q", got, want)
	}
	// A key with no catalogue entry reads as the key.
	if got := TN("ru", "test.never", 3, nil); got != "test.never" {
		t.Fatalf("unknown plural key = %q", got)
	}
	// English's one/other forms come from the embedded catalogue through Register.
	oldEn := catalogue(DefaultLocale)
	Register("en", map[string]string{
		"test.saved.one":   "Saved %{count} file",
		"test.saved.other": "Saved %{count} files",
	})
	t.Cleanup(func() {
		if oldEn != nil {
			Register(DefaultLocale, oldEn)
		}
	})
	if got, want := TN("en", "test.saved", 1, nil), "Saved 1 file"; got != want {
		t.Fatalf("TN(en, 1) = %q, want %q", got, want)
	}
	if got, want := TN("en", "test.saved", 3, nil), "Saved 3 files"; got != want {
		t.Fatalf("TN(en, 3) = %q, want %q", got, want)
	}
	// The count argument wins over a caller's own count.
	if got := TN("en", "test.saved", 2, map[string]any{"count": 99}); got != "Saved 2 files" {
		t.Fatalf("TN did not override the count: %q", got)
	}
}

func TestRegisterAndShorterTagFallback(t *testing.T) {
	Register("xx-YY", map[string]string{"test.hello": "short"})
	t.Cleanup(func() { Register("xx-YY", nil) })
	if got := T("xx-YY", "test.hello", nil); got != "short" {
		t.Fatalf("registered translation = %q", got)
	}
	// "xx-YY-zz" walks down to "xx-YY".
	if got := T("xx-YY-zz", "test.hello", nil); got != "short" {
		t.Fatalf("shorter tag fallback = %q", got)
	}
	// A table is copied: a later mutation does not show.
	src := map[string]string{"test.mutable": "first"}
	Register("xx-YY", src)
	src["test.mutable"] = "second"
	if got := T("xx-YY", "test.mutable", nil); got != "first" {
		t.Fatalf("registered table was not copied: %q", got)
	}
}

func TestSetLocaleNormalises(t *testing.T) {
	t.Cleanup(func() { SetLocale("en") })
	SetLocale("zh_CN.UTF-8")
	if got := Locale(); got != "zh-CN" {
		t.Fatalf("Locale() = %q, want zh-CN", got)
	}
	SetLocale("")
	if got := Locale(); got != "en" {
		t.Fatalf("Locale() after reset = %q, want en", got)
	}
	SetLocale("bogus")
	if got := Locale(); got != "bogus" {
		t.Fatalf("Locale() = %q, want the unknown code kept", got)
	}
}

func TestValueText(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   any
		want string
	}{
		{"text", "text"},
		{[]byte("bytes"), "bytes"},
		{true, "true"},
		{false, "false"},
		{7, "7"},
		{int64(-3), "-3"},
		{1.5, "1.5"},
	}
	for _, tt := range tests {
		if got := valueText(tt.in); got != tt.want {
			t.Errorf("valueText(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if !strings.Contains(T("en", "gvim.terminal.no_file_at", map[string]any{"token": "x/y"}), "x/y") {
		t.Fatal("no_file_at did not interpolate the token")
	}
}
